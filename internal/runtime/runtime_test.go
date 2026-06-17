package runtime

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ammarbinfaisal/dumpduck/internal/config"
	"github.com/ammarbinfaisal/dumpduck/internal/state"
)

func TestDecideWindowStartNew(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 17, 9, 0, 0, 0, time.UTC)
	decision := decideWindow(now, state.State{}, 24*time.Hour)

	if decision.action != windowActionStartNew {
		t.Fatalf("expected new window, got %v", decision.action)
	}
	if !decision.startTime.Equal(now) {
		t.Fatalf("unexpected start time %s", decision.startTime)
	}
}

func TestDecideWindowResumeWithinDuration(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	now := start.Add(23 * time.Hour)
	decision := decideWindow(now, state.State{CurrentWindowStartTime: &start}, 24*time.Hour)

	if decision.action != windowActionResume {
		t.Fatalf("expected resume window, got %v", decision.action)
	}
	if !decision.startTime.Equal(start) {
		t.Fatalf("unexpected start time %s", decision.startTime)
	}
}

func TestDecideWindowExpiredAtStartup(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	now := start.Add(24 * time.Hour)
	decision := decideWindow(now, state.State{CurrentWindowStartTime: &start}, 24*time.Hour)

	if decision.action != windowActionExpired {
		t.Fatalf("expected expired window, got %v", decision.action)
	}
	if !decision.endTime.Equal(start.Add(24 * time.Hour)) {
		t.Fatalf("unexpected end time %s", decision.endTime)
	}
}

func TestSelectUploadCandidates(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	rotateInterval := 5 * time.Minute

	oldPCAP := writeFileWithModTime(t, dir, "old.pcap", now.Add(-10*time.Minute))
	recentPCAP := writeFileWithModTime(t, dir, "recent.pcap", now.Add(-2*time.Minute))
	uploadedPCAP := writeFileWithModTime(t, dir, "uploaded.pcap", now.Add(-20*time.Minute))
	writeFileWithModTime(t, dir, "notes.txt", now.Add(-30*time.Minute))

	st := &state.State{
		UploadedFiles: []state.UploadedFileRecord{
			{
				Path:       uploadedPCAP,
				RemotePath: "remote:captures/uploaded.pcap",
				UploadedAt: now.Add(-1 * time.Minute),
			},
		},
	}

	got, err := selectUploadCandidates(dir, st, now, rotateInterval, true)
	if err != nil {
		t.Fatalf("select upload candidates: %v", err)
	}

	want := []string{oldPCAP}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidate mismatch:\nwant: %#v\ngot: %#v\nrecent: %s", want, got, recentPCAP)
	}
}

func TestSelectUploadCandidatesIncludesRecentWhenSkipDisabled(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	rotateInterval := 5 * time.Minute

	oldPCAP := writeFileWithModTime(t, dir, "old.pcap", now.Add(-10*time.Minute))
	recentPCAP := writeFileWithModTime(t, dir, "recent.pcap", now.Add(-2*time.Minute))

	got, err := selectUploadCandidates(dir, &state.State{}, now, rotateInterval, false)
	if err != nil {
		t.Fatalf("select upload candidates: %v", err)
	}

	want := []string{oldPCAP, recentPCAP}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidate mismatch:\nwant: %#v\ngot: %#v", want, got)
	}
}

func TestBuildTCPDumpArgs(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Capture.Interface = "en0"
	cfg.Capture.Snaplen = 128
	cfg.Capture.OutputDir = "/tmp/dumps"
	cfg.Capture.RotateInterval = "50ms"
	cfg.Capture.BPFFilter = "tcp port 443"

	got, err := buildTCPDumpArgs(cfg)
	if err != nil {
		t.Fatalf("build tcpdump args: %v", err)
	}

	want := []string{
		"-i", "en0",
		"-s", "128",
		"-G", "1",
		"-w", filepath.Join("/tmp/dumps", captureFilePattern),
		"tcp", "port", "443",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tcpdump args mismatch:\nwant: %#v\ngot: %#v", want, got)
	}
}

func TestBuildRcloneCopyArgsIncludesConfigPathWhenSet(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Upload.RcloneConfigPath = "/Users/test/.config/rclone/rclone.conf"

	got := buildRcloneCopyArgs(cfg, "/tmp/dump.pcap", "remote:captures")
	want := []string{"--config", "/Users/test/.config/rclone/rclone.conf", "copy", "/tmp/dump.pcap", "remote:captures"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rclone args mismatch:\nwant: %#v\ngot: %#v", want, got)
	}
}

func TestBuildRcloneCopyArgsUsesDefaultConfigWhenUnset(t *testing.T) {
	t.Parallel()

	cfg := config.Default()

	got := buildRcloneCopyArgs(cfg, "/tmp/dump.pcap", "remote:captures")
	want := []string{"copy", "/tmp/dump.pcap", "remote:captures"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rclone args mismatch:\nwant: %#v\ngot: %#v", want, got)
	}
}

func TestUploadFileMetadata(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "capture.pcap")
	content := "packet-data"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write capture: %v", err)
	}

	got, err := uploadFileMetadata(path)
	if err != nil {
		t.Fatalf("upload file metadata: %v", err)
	}
	if got.sizeBytes != int64(len(content)) {
		t.Fatalf("unexpected size: got %d want %d", got.sizeBytes, len(content))
	}
	if got.sha1 != testSHA1(content) {
		t.Fatalf("unexpected sha1: got %s want %s", got.sha1, testSHA1(content))
	}
}

func TestUploadFileMetadataMissingFileReturnsError(t *testing.T) {
	t.Parallel()

	_, err := uploadFileMetadata(filepath.Join(t.TempDir(), "missing.pcap"))
	if err == nil {
		t.Fatal("expected missing file metadata error")
	}
	if !strings.Contains(err.Error(), "stat upload candidate") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunExpiredWindowUploadsAndExitsWithoutTCPDump(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.Capture.TotalDuration = "1h"

	start := time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC)
	if err := state.Save(cfg.State.Path, state.State{CurrentWindowStartTime: &start}); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	oldFile := writeFileWithModTime(t, cfg.Capture.OutputDir, "expired-window.pcap", start.Add(30*time.Minute))
	recentFile := writeFileWithModTime(t, cfg.Capture.OutputDir, "expired-recent.pcap", start.Add(65*time.Minute))
	rcloneLog := filepath.Join(dir, "rclone.log")
	tcpdumpLog := filepath.Join(dir, "tcpdump.log")
	cfg.Binaries.RclonePath = writeRcloneScript(t, dir, rcloneLog)
	cfg.Binaries.TCPDumpPath = writeTCPDumpScript(t, dir, tcpdumpLog, 0)

	svc := New(cfg, ioDiscard{})
	svc.now = func() time.Time { return start.Add(2 * time.Hour) }

	if err := svc.Run(context.Background()); err != nil {
		t.Fatalf("run service: %v", err)
	}

	logData, err := os.ReadFile(rcloneLog)
	if err != nil {
		t.Fatalf("read rclone log: %v", err)
	}
	if !strings.Contains(string(logData), oldFile) {
		t.Fatalf("expected upload log to contain %s, got %s", oldFile, string(logData))
	}
	if !strings.Contains(string(logData), recentFile) {
		t.Fatalf("expected upload log to contain recent file %s, got %s", recentFile, string(logData))
	}

	if _, err := os.Stat(tcpdumpLog); !os.IsNotExist(err) {
		t.Fatalf("tcpdump should not have been started, stat err=%v", err)
	}

	st, err := state.Load(cfg.State.Path)
	if err != nil {
		t.Fatalf("reload state: %v", err)
	}
	if st.CurrentWindowStartTime != nil {
		t.Fatalf("expected current window to be cleared, got %v", st.CurrentWindowStartTime)
	}
	if len(st.UploadedFiles) != 2 {
		t.Fatalf("expected two uploaded file records, got %#v", st.UploadedFiles)
	}
	assertUploadedMetadata(t, st, oldFile, "pcap")
	assertUploadedMetadata(t, st, recentFile, "pcap")
}

func TestRunActiveWindowUploadsAndClearsExpiredWindow(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.Capture.RotateInterval = "50ms"
	cfg.Capture.TotalDuration = "180ms"
	cfg.Upload.Frequency = "30ms"

	oldFile := writeFileWithModTime(t, cfg.Capture.OutputDir, "cycle-1.pcap", time.Now().Add(-1*time.Second))
	recentFile := writeFileWithModTime(t, cfg.Capture.OutputDir, "cycle-recent.pcap", time.Now())
	rcloneLog := filepath.Join(dir, "rclone.log")
	tcpdumpLog := filepath.Join(dir, "tcpdump.log")
	cfg.Binaries.RclonePath = writeRcloneScript(t, dir, rcloneLog)
	cfg.Binaries.TCPDumpPath = writeTCPDumpScript(t, dir, tcpdumpLog, 130)

	svc := New(cfg, ioDiscard{})
	if err := svc.Run(context.Background()); err != nil {
		t.Fatalf("run service: %v", err)
	}

	rcloneData, err := os.ReadFile(rcloneLog)
	if err != nil {
		t.Fatalf("read rclone log: %v", err)
	}
	if !strings.Contains(string(rcloneData), oldFile) {
		t.Fatalf("expected upload log to contain %s, got %s", oldFile, string(rcloneData))
	}
	if !strings.Contains(string(rcloneData), recentFile) {
		t.Fatalf("expected final upload log to contain recent file %s, got %s", recentFile, string(rcloneData))
	}

	tcpdumpData, err := os.ReadFile(tcpdumpLog)
	if err != nil {
		t.Fatalf("read tcpdump log: %v", err)
	}
	if !strings.Contains(string(tcpdumpData), "-G 1") {
		t.Fatalf("expected tcpdump log to record rotate args, got %s", string(tcpdumpData))
	}

	st, err := state.Load(cfg.State.Path)
	if err != nil {
		t.Fatalf("reload state: %v", err)
	}
	if st.CurrentWindowStartTime != nil {
		t.Fatalf("expected window start cleared after expiry, got %v", st.CurrentWindowStartTime)
	}
	if st.LastSuccessfulUploadTime == nil {
		t.Fatal("expected last successful upload time to be recorded")
	}
	if len(st.UploadedFiles) != 2 {
		t.Fatalf("expected uploaded file records, got %#v", st.UploadedFiles)
	}
}

func TestRunActiveWindowReturnsErrorWhenTCPDumpExitsEarly(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.Capture.RotateInterval = "1s"
	cfg.Capture.TotalDuration = "5s"
	cfg.Upload.Frequency = "1s"

	rcloneLog := filepath.Join(dir, "rclone.log")
	tcpdumpLog := filepath.Join(dir, "tcpdump.log")
	cfg.Binaries.RclonePath = writeRcloneScript(t, dir, rcloneLog)
	cfg.Binaries.TCPDumpPath = writeTCPDumpCrashScript(t, dir, tcpdumpLog, 2)

	svc := New(cfg, ioDiscard{})
	err := svc.Run(context.Background())
	if err == nil {
		t.Fatal("expected tcpdump early exit to return an error")
	}
	if !strings.Contains(err.Error(), "tcpdump exited unexpectedly") {
		t.Fatalf("expected unexpected tcpdump error, got %v", err)
	}

	st, loadErr := state.Load(cfg.State.Path)
	if loadErr != nil {
		t.Fatalf("reload state: %v", loadErr)
	}
	if st.CurrentWindowStartTime == nil {
		t.Fatal("expected window state to remain set after unexpected tcpdump exit")
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}

func testConfig(dir string) config.Config {
	cfg := config.Default()
	cfg.Capture.OutputDir = filepath.Join(dir, "dumps")
	cfg.State.Path = filepath.Join(dir, "state.json")
	cfg.Logging.Path = filepath.Join(dir, "dumpduck.log")
	cfg.Upload.RcloneRemote = "remote"
	cfg.Upload.RclonePath = "captures"
	return cfg
}

func writeFileWithModTime(t *testing.T, dir, name string, modTime time.Time) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte("pcap"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
	return path
}

func writeRcloneScript(t *testing.T, dir, logPath string) string {
	t.Helper()

	script := filepath.Join(dir, "rclone.sh")
	content := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> " + shellQuote(logPath) + "\n" +
		"exit 0\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write rclone script: %v", err)
	}
	return script
}

func writeTCPDumpScript(t *testing.T, dir, logPath string, interruptExitCode int) string {
	t.Helper()

	script := filepath.Join(dir, "tcpdump.sh")
	content := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" > " + shellQuote(logPath) + "\n" +
		"trap 'exit " + itoa(interruptExitCode) + "' INT TERM\n" +
		"while :; do\n" +
		"  sleep 0.02\n" +
		"done\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write tcpdump script: %v", err)
	}
	return script
}

func writeTCPDumpCrashScript(t *testing.T, dir, logPath string, exitCode int) string {
	t.Helper()

	script := filepath.Join(dir, "tcpdump-crash.sh")
	content := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" > " + shellQuote(logPath) + "\n" +
		"exit " + itoa(exitCode) + "\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write tcpdump crash script: %v", err)
	}
	return script
}

func itoa(value int) string {
	return strconv.Itoa(value)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func assertUploadedMetadata(t *testing.T, st state.State, localPath, content string) {
	t.Helper()

	for _, record := range st.UploadedFiles {
		if record.Path != filepath.Clean(localPath) {
			continue
		}
		if record.SizeBytes != int64(len(content)) {
			t.Fatalf("unexpected size for %s: got %d want %d", localPath, record.SizeBytes, len(content))
		}
		if record.SHA1 != testSHA1(content) {
			t.Fatalf("unexpected sha1 for %s: got %s want %s", localPath, record.SHA1, testSHA1(content))
		}
		return
	}

	t.Fatalf("uploaded record not found for %s in %#v", localPath, st.UploadedFiles)
}

func testSHA1(value string) string {
	sum := sha1.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

package config

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	cfg := Default()

	if cfg.Capture.Interface != "" {
		t.Fatalf("expected empty interface, got %q", cfg.Capture.Interface)
	}
	if cfg.Capture.BPFFilter != "" {
		t.Fatalf("expected empty bpf filter, got %q", cfg.Capture.BPFFilter)
	}
	if cfg.Capture.Snaplen != 0 {
		t.Fatalf("expected snaplen 0, got %d", cfg.Capture.Snaplen)
	}
	if cfg.Capture.OutputDir != "/var/lib/dumpduck/dumps" {
		t.Fatalf("unexpected output dir %q", cfg.Capture.OutputDir)
	}
	if cfg.Capture.RotateInterval != "5m" {
		t.Fatalf("unexpected rotate interval %q", cfg.Capture.RotateInterval)
	}
	if cfg.Capture.TotalDuration != "24h" {
		t.Fatalf("unexpected total duration %q", cfg.Capture.TotalDuration)
	}
	if cfg.Upload.Frequency != "15m" {
		t.Fatalf("unexpected upload frequency %q", cfg.Upload.Frequency)
	}
	if cfg.Upload.RcloneRemote != "gdrive" {
		t.Fatalf("unexpected rclone remote %q", cfg.Upload.RcloneRemote)
	}
	if cfg.Upload.RclonePath != "dumpduck" {
		t.Fatalf("unexpected rclone path %q", cfg.Upload.RclonePath)
	}
	if cfg.Upload.RcloneConfigPath != "" {
		t.Fatalf("unexpected rclone config path %q", cfg.Upload.RcloneConfigPath)
	}
	if cfg.Upload.DeleteAfterSuccess {
		t.Fatal("delete after success should default to false")
	}
	if cfg.State.Path != "/var/lib/dumpduck/state.json" {
		t.Fatalf("unexpected state path %q", cfg.State.Path)
	}
	if cfg.Logging.Path != "/var/log/dumpduck/dumpduck.log" {
		t.Fatalf("unexpected logging path %q", cfg.Logging.Path)
	}
	if cfg.Binaries.TCPDumpPath != "/usr/sbin/tcpdump" {
		t.Fatalf("unexpected tcpdump path %q", cfg.Binaries.TCPDumpPath)
	}
	if cfg.Binaries.RclonePath != "/opt/homebrew/bin/rclone" {
		t.Fatalf("unexpected rclone binary path %q", cfg.Binaries.RclonePath)
	}
}

func TestYAMLRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	want := Default()

	if err := Save(path, want); err != nil {
		t.Fatalf("save config: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("config mismatch after round trip:\nwant: %#v\ngot: %#v", want, got)
	}
}

func TestSetSupportedFields(t *testing.T) {
	t.Parallel()

	cfg := Default()
	updates := map[string]string{
		"upload.frequency":          "30m",
		"upload.rclone_config_path": filepath.Join(t.TempDir(), "rclone.conf"),
		"upload.rclone_remote":      "backblaze",
		"upload.rclone_path":        "captures/team-a",
		"capture.interface":         "en0",
		"capture.bpf_filter":        "tcp port 443",
		"capture.output_dir":        "/tmp/dumps",
		"state.path":                "/tmp/state.json",
		"logging.path":              "/tmp/dumpduck.log",
	}

	for key, value := range updates {
		if err := cfg.Set(key, value); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}

	if cfg.Upload.Frequency != "30m" ||
		cfg.Upload.RcloneConfigPath == "" ||
		cfg.Upload.RcloneRemote != "backblaze" ||
		cfg.Upload.RclonePath != "captures/team-a" ||
		cfg.Capture.Interface != "en0" ||
		cfg.Capture.BPFFilter != "tcp port 443" ||
		cfg.Capture.OutputDir != "/tmp/dumps" ||
		cfg.State.Path != "/tmp/state.json" ||
		cfg.Logging.Path != "/tmp/dumpduck.log" {
		t.Fatalf("supported fields were not updated correctly: %#v", cfg)
	}
}

func TestSetRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	cfg := Default()

	if err := cfg.Set("upload.frequency", "not-a-duration"); err == nil || !strings.Contains(err.Error(), "valid duration") {
		t.Fatalf("expected invalid duration error, got %v", err)
	}

	if err := cfg.Set("upload.unknown", "value"); err == nil || !strings.Contains(err.Error(), "unsupported config key") {
		t.Fatalf("expected unsupported key error, got %v", err)
	}

	if err := cfg.Set("upload.rclone_config_path", "relative/rclone.conf"); err == nil || !strings.Contains(err.Error(), "absolute path") {
		t.Fatalf("expected absolute path error, got %v", err)
	}
}

func TestInitFileRefusesOverwrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := InitFile(path, false); err != nil {
		t.Fatalf("first init failed: %v", err)
	}
	if err := InitFile(path, false); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("expected overwrite refusal, got %v", err)
	}
	if err := InitFile(path, true); err != nil {
		t.Fatalf("forced init failed: %v", err)
	}
}

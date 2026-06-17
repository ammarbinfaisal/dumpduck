package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/ammarbinfaisal/dumpduck/internal/config"
	"github.com/ammarbinfaisal/dumpduck/internal/state"
)

const captureFilePattern = "dumpduck-%Y%m%d-%H%M%S.pcap"

type Service struct {
	cfg    config.Config
	stdout io.Writer
	now    func() time.Time
}

type windowAction int

const (
	windowActionStartNew windowAction = iota
	windowActionResume
	windowActionExpired
)

type windowDecision struct {
	action    windowAction
	startTime time.Time
	endTime   time.Time
}

func New(cfg config.Config, stdout io.Writer) *Service {
	return &Service{
		cfg:    cfg,
		stdout: stdout,
		now:    time.Now,
	}
}

func (s *Service) Run(ctx context.Context) error {
	rotateInterval, err := time.ParseDuration(s.cfg.Capture.RotateInterval)
	if err != nil {
		return fmt.Errorf("parse capture.rotate_interval: %w", err)
	}
	totalDuration, err := time.ParseDuration(s.cfg.Capture.TotalDuration)
	if err != nil {
		return fmt.Errorf("parse capture.total_duration: %w", err)
	}
	uploadFrequency, err := time.ParseDuration(s.cfg.Upload.Frequency)
	if err != nil {
		return fmt.Errorf("parse upload.frequency: %w", err)
	}

	st, err := state.Load(s.cfg.State.Path)
	if err != nil {
		return err
	}

	decision := decideWindow(s.now(), st, totalDuration)
	switch decision.action {
	case windowActionStartNew:
		startTime := decision.startTime.UTC()
		st.CurrentWindowStartTime = &startTime
		if err := state.Save(s.cfg.State.Path, st); err != nil {
			return err
		}
		fmt.Fprintf(s.stdout, "Starting DumpDuck capture window at %s\n", startTime.Format(time.RFC3339))
	case windowActionResume:
		fmt.Fprintf(s.stdout, "Resuming DumpDuck capture window started at %s\n", decision.startTime.UTC().Format(time.RFC3339))
	case windowActionExpired:
		fmt.Fprintf(s.stdout, "Existing DumpDuck capture window expired at %s; uploading completed dumps and exiting.\n", decision.endTime.UTC().Format(time.RFC3339))
		if err := s.uploadPending(&st, rotateInterval, false); err != nil {
			return err
		}
		st.CurrentWindowStartTime = nil
		if err := state.Save(s.cfg.State.Path, st); err != nil {
			return err
		}
		return nil
	default:
		return fmt.Errorf("unknown window action %d", decision.action)
	}

	if err := os.MkdirAll(s.cfg.Capture.OutputDir, 0o755); err != nil {
		return fmt.Errorf("create capture output directory %q: %w", s.cfg.Capture.OutputDir, err)
	}

	return s.runActiveWindow(ctx, &st, decision, rotateInterval, totalDuration, uploadFrequency)
}

func (s *Service) runActiveWindow(ctx context.Context, st *state.State, decision windowDecision, rotateInterval, totalDuration, uploadFrequency time.Duration) error {
	windowEnd := decision.startTime.Add(totalDuration)
	remaining := windowEnd.Sub(s.now())
	if remaining < 0 {
		remaining = 0
	}

	cmd, stderr, err := s.startTCPDump()
	if err != nil {
		return err
	}

	cmdDone := make(chan error, 1)
	go func() {
		cmdDone <- cmd.Wait()
	}()

	expiryTimer := time.NewTimer(remaining)
	defer expiryTimer.Stop()

	uploadTicker := time.NewTicker(uploadFrequency)
	defer uploadTicker.Stop()

	var shutdownRequested bool
	var finalWindowExpired bool

	for {
		select {
		case <-ctx.Done():
			if !shutdownRequested {
				shutdownRequested = true
				fmt.Fprintln(s.stdout, "Shutdown signal received; stopping tcpdump and uploading completed dumps.")
				if err := stopProcess(cmd); err != nil {
					return fmt.Errorf("stop tcpdump: %w", err)
				}
			}
		case <-expiryTimer.C:
			if !shutdownRequested {
				shutdownRequested = true
				finalWindowExpired = true
				fmt.Fprintf(s.stdout, "DumpDuck capture window reached %s; stopping tcpdump.\n", windowEnd.UTC().Format(time.RFC3339))
				if err := stopProcess(cmd); err != nil {
					return fmt.Errorf("stop tcpdump at window expiry: %w", err)
				}
			}
		case <-uploadTicker.C:
			if shutdownRequested {
				continue
			}
			if err := s.uploadPending(st, rotateInterval, true); err != nil {
				fmt.Fprintf(s.stdout, "Upload pass failed; will retry on the next cycle: %v\n", err)
			}
		case err := <-cmdDone:
			finalUploadErr := s.uploadPending(st, rotateInterval, false)

			if finalWindowExpired && finalUploadErr == nil {
				st.CurrentWindowStartTime = nil
				if saveErr := state.Save(s.cfg.State.Path, *st); saveErr != nil {
					return saveErr
				}
			}

			if err != nil {
				if shutdownRequested && isIntentionalStopError(err) {
					if finalUploadErr != nil {
						return finalUploadErr
					}
					return nil
				}

				tcpdumpErr := formatProcessExitError("tcpdump", err, stderr.String())
				if shutdownRequested {
					if finalUploadErr != nil {
						return finalUploadErr
					}
					return tcpdumpErr
				}
				if finalUploadErr != nil {
					return fmt.Errorf("%v; final upload pass also failed: %w", tcpdumpErr, finalUploadErr)
				}
				return tcpdumpErr
			}

			if finalUploadErr != nil {
				return finalUploadErr
			}

			return nil
		}
	}
}

func (s *Service) startTCPDump() (*exec.Cmd, *bytes.Buffer, error) {
	args, err := buildTCPDumpArgs(s.cfg)
	if err != nil {
		return nil, nil, err
	}

	cmd := exec.Command(s.cfg.Binaries.TCPDumpPath, args...)
	cmd.Stdout = io.Discard

	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("start tcpdump command %q: %w", s.cfg.Binaries.TCPDumpPath, err)
	}

	return cmd, stderr, nil
}

func (s *Service) uploadPending(st *state.State, rotateInterval time.Duration, skipRecent bool) error {
	candidates, err := selectUploadCandidates(s.cfg.Capture.OutputDir, st, s.now(), rotateInterval, skipRecent)
	if err != nil {
		return err
	}

	remoteRoot := remoteRoot(s.cfg.Upload.RcloneRemote, s.cfg.Upload.RclonePath)
	for _, localPath := range candidates {
		output, err := exec.Command(s.cfg.Binaries.RclonePath, "copy", localPath, remoteRoot).CombinedOutput()
		if err != nil {
			message := strings.TrimSpace(string(output))
			if message == "" {
				return fmt.Errorf("upload %q with rclone: %w", localPath, err)
			}
			return fmt.Errorf("upload %q with rclone: %w: %s", localPath, err, message)
		}

		uploadedAt := s.now().UTC()
		st.RecordUploadedFile(state.UploadedFileRecord{
			Path:       localPath,
			RemotePath: remoteFilePath(remoteRoot, localPath),
			UploadedAt: uploadedAt,
		})
		st.LastSuccessfulUploadTime = &uploadedAt
		if err := state.Save(s.cfg.State.Path, *st); err != nil {
			return err
		}

		if s.cfg.Upload.DeleteAfterSuccess {
			if err := os.Remove(localPath); err != nil {
				return fmt.Errorf("delete uploaded file %q: %w", localPath, err)
			}
		}
	}

	return nil
}

func decideWindow(now time.Time, st state.State, totalDuration time.Duration) windowDecision {
	now = now.UTC()
	if st.CurrentWindowStartTime == nil {
		return windowDecision{
			action:    windowActionStartNew,
			startTime: now,
			endTime:   now.Add(totalDuration),
		}
	}

	start := st.CurrentWindowStartTime.UTC()
	end := start.Add(totalDuration)
	if !now.Before(end) {
		return windowDecision{
			action:    windowActionExpired,
			startTime: start,
			endTime:   end,
		}
	}

	return windowDecision{
		action:    windowActionResume,
		startTime: start,
		endTime:   end,
	}
}

func buildTCPDumpArgs(cfg config.Config) ([]string, error) {
	rotateInterval, err := time.ParseDuration(cfg.Capture.RotateInterval)
	if err != nil {
		return nil, fmt.Errorf("parse capture.rotate_interval: %w", err)
	}

	args := make([]string, 0, 8)
	if strings.TrimSpace(cfg.Capture.Interface) != "" {
		args = append(args, "-i", cfg.Capture.Interface)
	}
	if cfg.Capture.Snaplen > 0 {
		args = append(args, "-s", fmt.Sprintf("%d", cfg.Capture.Snaplen))
	}

	args = append(args, "-G", fmt.Sprintf("%d", rotateIntervalSeconds(rotateInterval)))
	args = append(args, "-w", filepath.Join(cfg.Capture.OutputDir, captureFilePattern))
	args = append(args, splitBPFArgs(cfg.Capture.BPFFilter)...)

	return args, nil
}

func splitBPFArgs(filter string) []string {
	return strings.Fields(filter)
}

func rotateIntervalSeconds(interval time.Duration) int {
	return max(1, int(math.Ceil(interval.Seconds())))
}

func selectUploadCandidates(outputDir string, st *state.State, now time.Time, rotateInterval time.Duration, skipRecent bool) ([]string, error) {
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read capture output directory %q: %w", outputDir, err)
	}

	uploaded := st.UploadedFileIndex()
	cutoff := now.Add(-rotateInterval)
	candidates := make([]string, 0, len(entries))

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".pcap" {
			continue
		}

		fullPath := filepath.Clean(filepath.Join(outputDir, entry.Name()))
		if _, ok := uploaded[fullPath]; ok {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("stat capture file %q: %w", fullPath, err)
		}
		if skipRecent && info.ModTime().After(cutoff) {
			continue
		}

		candidates = append(candidates, fullPath)
	}

	slices.Sort(candidates)
	return candidates, nil
}

func remoteRoot(remote, remotePath string) string {
	return fmt.Sprintf("%s:%s", remote, remotePath)
}

func remoteFilePath(remoteDestination, localPath string) string {
	return fmt.Sprintf("%s/%s", strings.TrimRight(remoteDestination, "/"), path.Base(localPath))
}

func stopProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil && !isProcessDoneError(err) {
		return err
	}
	return nil
}

func isProcessDoneError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "process already finished")
}

func isIntentionalStopError(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}

	waitStatus, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok {
		return false
	}

	if waitStatus.Signaled() {
		signal := waitStatus.Signal()
		return signal == syscall.SIGINT || signal == syscall.SIGTERM
	}

	if waitStatus.Exited() {
		exitStatus := waitStatus.ExitStatus()
		return exitStatus == 128+int(syscall.SIGINT) || exitStatus == 128+int(syscall.SIGTERM)
	}

	return false
}

func formatProcessExitError(name string, err error, stderr string) error {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return fmt.Errorf("%s exited unexpectedly: %w", name, err)
	}
	return fmt.Errorf("%s exited unexpectedly: %w: %s", name, err, stderr)
}

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ammarbinfaisal/dumpduck/internal/config"
)

func TestRunInstallDryRunPrintsPlist(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	binaryPath := filepath.Join(dir, "dumpduck")
	if err := os.WriteFile(binaryPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	var stdout bytes.Buffer
	if err := runInstall([]string{
		"--dry-run",
		"--config", configPath,
		"--binary", binaryPath,
	}, &stdout); err != nil {
		t.Fatalf("run install dry-run: %v", err)
	}

	output := stdout.String()
	for _, want := range []string{
		"<string>com.dumpduck.service</string>",
		"<string>" + binaryPath + "</string>",
		"<string>run</string>",
		"<string>--config</string>",
		"<string>" + configPath + "</string>",
		"<true/>",
		"<string>/var/log/dumpduck/dumpduck.log</string>",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("dry-run output missing %q:\n%s", want, output)
		}
	}

	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not create config, stat err = %v", err)
	}
}

func TestRunInstallWritesPlistAndCreatesConfigWhenMissing(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	plistPath := filepath.Join(dir, "LaunchDaemons", "com.dumpduck.service.plist")
	binaryPath := filepath.Join(dir, "dumpduck")
	if err := os.WriteFile(binaryPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	originalMkdirAll := mkdirAll
	t.Cleanup(func() {
		mkdirAll = originalMkdirAll
	})
	mkdirAll = func(path string, perm os.FileMode) error {
		switch path {
		case config.Default().Capture.OutputDir, filepath.Dir(config.Default().State.Path), filepath.Dir(config.Default().Logging.Path):
			return nil
		default:
			return originalMkdirAll(path, perm)
		}
	}

	var stdout bytes.Buffer
	if err := runInstall([]string{
		"--config", configPath,
		"--plist", plistPath,
		"--binary", binaryPath,
		"--skip-load",
	}, &stdout); err != nil {
		t.Fatalf("run install: %v", err)
	}

	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("config not created: %v", err)
	}
	if _, err := os.Stat(plistPath); err != nil {
		t.Fatalf("plist not created: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg != config.Default() {
		t.Fatalf("unexpected created config: %#v", cfg)
	}

	plistData, err := os.ReadFile(plistPath)
	if err != nil {
		t.Fatalf("read plist: %v", err)
	}
	if !strings.Contains(string(plistData), "<string>"+configPath+"</string>") {
		t.Fatalf("plist missing config path:\n%s", plistData)
	}
}

func TestResolveBinaryPathRejectsImplicitGoRunExecutable(t *testing.T) {
	originalExecutablePath := executablePath
	t.Cleanup(func() {
		executablePath = originalExecutablePath
	})

	executablePath = func() (string, error) {
		return "/var/folders/test/go-build123456789/b001/exe/dumpduck", nil
	}

	_, err := resolveBinaryPath("")
	if err == nil {
		t.Fatal("expected go run executable path to be rejected")
	}
	if !strings.Contains(err.Error(), "temporary `go run` build artifact") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "--binary /path/to/dumpduck") {
		t.Fatalf("expected actionable guidance, got: %v", err)
	}
}

func TestResolveBinaryPathAcceptsExplicitPathEvenIfItLooksTemporary(t *testing.T) {
	dir := t.TempDir()
	explicitPath := filepath.Join(dir, "go-build123456789", "exe", "dumpduck")

	got, err := resolveBinaryPath(explicitPath)
	if err != nil {
		t.Fatalf("resolve explicit binary path: %v", err)
	}
	if got != explicitPath {
		t.Fatalf("unexpected resolved path: got %q want %q", got, explicitPath)
	}
}

func TestRunUninstallDryRunPrintsActions(t *testing.T) {
	var stdout bytes.Buffer
	plistPath := filepath.Join(t.TempDir(), "com.dumpduck.service.plist")

	if err := runUninstall([]string{"--dry-run", "--plist", plistPath}, &stdout); err != nil {
		t.Fatalf("run uninstall dry-run: %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "Would unload LaunchDaemon: system/com.dumpduck.service") {
		t.Fatalf("dry-run output missing unload action:\n%s", output)
	}
	if !strings.Contains(output, "Would remove LaunchDaemon plist: "+plistPath) {
		t.Fatalf("dry-run output missing plist action:\n%s", output)
	}
}

func TestRunUninstallRemovesPlistWhenSkipUnload(t *testing.T) {
	dir := t.TempDir()
	plistPath := filepath.Join(dir, "com.dumpduck.service.plist")
	if err := os.WriteFile(plistPath, []byte("plist"), 0o644); err != nil {
		t.Fatalf("write plist: %v", err)
	}

	var stdout bytes.Buffer
	if err := runUninstall([]string{"--plist", plistPath, "--skip-unload"}, &stdout); err != nil {
		t.Fatalf("run uninstall: %v", err)
	}

	if _, err := os.Stat(plistPath); !os.IsNotExist(err) {
		t.Fatalf("expected plist removed, stat err = %v", err)
	}
}

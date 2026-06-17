package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/pflag"

	"github.com/ammarbinfaisal/dumpduck/internal/config"
	"github.com/ammarbinfaisal/dumpduck/internal/launchd"
	runtimepkg "github.com/ammarbinfaisal/dumpduck/internal/runtime"
	"github.com/ammarbinfaisal/dumpduck/internal/state"
)

var mkdirAll = os.MkdirAll
var executablePath = os.Executable

func Execute(args []string, stdout, stderr io.Writer) int {
	if err := run(args, stdout); err != nil {
		fmt.Fprintf(stderr, "DumpDuck error: %v\n", err)
		return 1
	}
	return 0
}

func run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		printRootUsage(stdout)
		return nil
	}

	switch args[0] {
	case "config":
		return runConfig(args[1:], stdout)
	case "status":
		return runStatus(args[1:], stdout)
	case "run":
		return runDaemon(args[1:], stdout)
	case "install":
		return runInstall(args[1:], stdout)
	case "uninstall":
		return runUninstall(args[1:], stdout)
	case "help", "--help", "-h":
		printRootUsage(stdout)
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runConfig(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		printConfigUsage(stdout)
		return nil
	}

	switch args[0] {
	case "init":
		return runConfigInit(args[1:], stdout)
	case "set":
		return runConfigSet(args[1:], stdout)
	case "help", "--help", "-h":
		printConfigUsage(stdout)
		return nil
	default:
		return fmt.Errorf("unknown config command %q", args[0])
	}
}

func runConfigInit(args []string, stdout io.Writer) error {
	flags := pflag.NewFlagSet("config init", pflag.ContinueOnError)
	flags.SetOutput(io.Discard)

	path := flags.String("path", config.DefaultConfigPath, "config file path")
	force := flags.Bool("force", false, "overwrite an existing config file")

	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("config init does not accept positional arguments")
	}

	if err := config.InitFile(*path, *force); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "DumpDuck configuration initialized at %s\n", *path)
	return nil
}

func runConfigSet(args []string, stdout io.Writer) error {
	flags := pflag.NewFlagSet("config set", pflag.ContinueOnError)
	flags.SetOutput(io.Discard)

	path := flags.String("path", config.DefaultConfigPath, "config file path")

	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return errors.New("usage: dumpduck config set <key> <value> --path <path>")
	}

	cfg, err := config.Load(*path)
	if err != nil {
		return err
	}

	key := flags.Arg(0)
	value := flags.Arg(1)
	if err := cfg.Set(key, value); err != nil {
		return err
	}
	if err := config.Save(*path, cfg); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "DumpDuck configuration updated: %s=%s\n", key, value)
	return nil
}

func runStatus(args []string, stdout io.Writer) error {
	flags := pflag.NewFlagSet("status", pflag.ContinueOnError)
	flags.SetOutput(io.Discard)

	path := flags.String("config", config.DefaultConfigPath, "config file path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("status does not accept positional arguments")
	}

	cfg, err := config.Load(*path)
	if err != nil {
		return err
	}
	st, err := state.Load(cfg.State.Path)
	if err != nil {
		return err
	}

	fmt.Fprintf(stdout, "Config path: %s\n", *path)
	fmt.Fprintf(stdout, "Dump dir: %s\n", cfg.Capture.OutputDir)
	fmt.Fprintf(stdout, "Upload frequency: %s\n", cfg.Upload.Frequency)
	fmt.Fprintf(stdout, "Upload destination: %s:%s\n", cfg.Upload.RcloneRemote, cfg.Upload.RclonePath)
	fmt.Fprintf(stdout, "Rclone config: %s\n", formatString(cfg.Upload.RcloneConfigPath, "rclone default"))
	fmt.Fprintf(stdout, "State path: %s\n", cfg.State.Path)
	fmt.Fprintf(stdout, "Last upload: %s\n", formatTime(st.LastSuccessfulUploadTime, "never"))
	fmt.Fprintf(stdout, "Current window start: %s\n", formatTime(st.CurrentWindowStartTime, "none"))
	fmt.Fprintf(stdout, "LaunchDaemon label: %s\n", config.DefaultLaunchDaemonLabel)
	fmt.Fprintf(stdout, "LaunchDaemon plist: %s (%s)\n", config.DefaultLaunchDaemonPlistPath, plistPresence(config.DefaultLaunchDaemonPlistPath))

	return nil
}

func runDaemon(args []string, stdout io.Writer) error {
	flags := pflag.NewFlagSet("run", pflag.ContinueOnError)
	flags.SetOutput(io.Discard)

	path := flags.String("config", config.DefaultConfigPath, "config file path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("run does not accept positional arguments")
	}

	cfg, err := config.Load(*path)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return runtimepkg.New(cfg, stdout).Run(ctx)
}

func runInstall(args []string, stdout io.Writer) error {
	flags := pflag.NewFlagSet("install", pflag.ContinueOnError)
	flags.SetOutput(io.Discard)

	configPath := flags.String("config", config.DefaultConfigPath, "config file path")
	binaryPath := flags.String("binary", "", "absolute dumpduck binary path")
	plistPath := flags.String("plist", config.DefaultLaunchDaemonPlistPath, "LaunchDaemon plist path")
	dryRun := flags.Bool("dry-run", false, "print the LaunchDaemon plist and exit")
	skipLoad := flags.Bool("skip-load", false, "write the plist but skip launchctl bootstrap")

	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("install does not accept positional arguments")
	}

	resolvedBinaryPath, err := resolveBinaryPath(*binaryPath)
	if err != nil {
		return err
	}

	cfg, createdConfig, err := loadInstallConfig(*configPath, !*dryRun)
	if err != nil {
		return err
	}

	service := launchd.Service{
		Label:             config.DefaultLaunchDaemonLabel,
		ProgramArguments:  []string{resolvedBinaryPath, "run", "--config", *configPath},
		RunAtLoad:         true,
		KeepAlive:         true,
		StandardOutPath:   cfg.Logging.Path,
		StandardErrorPath: cfg.Logging.Path,
	}
	plistData, err := service.PlistBytes()
	if err != nil {
		return err
	}

	if *dryRun {
		_, err := stdout.Write(plistData)
		return err
	}

	if err := ensureInstallDirectories(*configPath, *plistPath, cfg); err != nil {
		return err
	}

	if err := launchd.WritePlist(*plistPath, plistData); err != nil {
		return withPrivilegeHint(err)
	}

	if createdConfig {
		fmt.Fprintf(stdout, "DumpDuck configuration initialized at %s\n", *configPath)
	}
	fmt.Fprintf(stdout, "DumpDuck LaunchDaemon plist written to %s\n", *plistPath)

	if *skipLoad {
		fmt.Fprintln(stdout, "LaunchDaemon load skipped.")
		return nil
	}

	if err := launchd.Bootstrap(*plistPath); err != nil {
		return withRootHint(fmt.Errorf("%w; if the service is already loaded, run `sudo launchctl bootout system/%s` first or re-run with --skip-load", err, config.DefaultLaunchDaemonLabel))
	}

	fmt.Fprintf(stdout, "DumpDuck LaunchDaemon loaded as %s\n", config.DefaultLaunchDaemonLabel)
	return nil
}

func runUninstall(args []string, stdout io.Writer) error {
	flags := pflag.NewFlagSet("uninstall", pflag.ContinueOnError)
	flags.SetOutput(io.Discard)

	plistPath := flags.String("plist", config.DefaultLaunchDaemonPlistPath, "LaunchDaemon plist path")
	dryRun := flags.Bool("dry-run", false, "print uninstall actions and exit")
	skipUnload := flags.Bool("skip-unload", false, "remove the plist but skip launchctl bootout")

	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("uninstall does not accept positional arguments")
	}

	if *dryRun {
		if !*skipUnload {
			fmt.Fprintf(stdout, "Would unload LaunchDaemon: system/%s\n", config.DefaultLaunchDaemonLabel)
		}
		fmt.Fprintf(stdout, "Would remove LaunchDaemon plist: %s\n", *plistPath)
		return nil
	}

	if !*skipUnload {
		if err := launchd.Bootout(config.DefaultLaunchDaemonLabel); err != nil {
			return withRootHint(err)
		}
		fmt.Fprintf(stdout, "DumpDuck LaunchDaemon unloaded: %s\n", config.DefaultLaunchDaemonLabel)
	}

	if err := os.Remove(*plistPath); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return withPrivilegeHint(fmt.Errorf("remove LaunchDaemon plist %q: %w", *plistPath, err))
		}
		fmt.Fprintf(stdout, "LaunchDaemon plist already absent: %s\n", *plistPath)
		return nil
	}

	fmt.Fprintf(stdout, "DumpDuck LaunchDaemon plist removed: %s\n", *plistPath)
	return nil
}

func loadInstallConfig(path string, allowCreate bool) (config.Config, bool, error) {
	if _, err := os.Stat(path); err == nil {
		cfg, loadErr := config.Load(path)
		return cfg, false, loadErr
	} else if !errors.Is(err, os.ErrNotExist) {
		return config.Config{}, false, fmt.Errorf("check config %q: %w", path, err)
	}

	if !allowCreate {
		return config.Default(), false, nil
	}

	if err := config.InitFile(path, false); err != nil {
		return config.Config{}, false, err
	}
	cfg, err := config.Load(path)
	return cfg, true, err
}

func ensureInstallDirectories(configPath, plistPath string, cfg config.Config) error {
	for _, dir := range []string{
		filepath.Dir(configPath),
		cfg.Capture.OutputDir,
		filepath.Dir(cfg.State.Path),
		filepath.Dir(cfg.Logging.Path),
		filepath.Dir(plistPath),
	} {
		if err := mkdirAll(dir, 0o755); err != nil {
			return withPrivilegeHint(fmt.Errorf("create directory %q: %w", dir, err))
		}
	}
	return nil
}

func resolveBinaryPath(flagValue string) (string, error) {
	path := strings.TrimSpace(flagValue)
	explicit := path != ""
	if !explicit {
		exe, err := executablePath()
		if err != nil {
			return "", fmt.Errorf("discover dumpduck binary path: %w; pass --binary explicitly", err)
		}
		path = exe
	}

	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve absolute binary path for %q: %w", path, err)
	}
	if !explicit && isGoRunExecutablePath(absolutePath) {
		return "", fmt.Errorf("discovered dumpduck binary path %q looks like a temporary `go run` build artifact; build or install DumpDuck first and pass --binary /path/to/dumpduck", absolutePath)
	}
	return absolutePath, nil
}

func isGoRunExecutablePath(path string) bool {
	cleanPath := filepath.Clean(path)
	segments := strings.Split(cleanPath, string(filepath.Separator))
	for i, segment := range segments {
		if strings.HasPrefix(segment, "go-build") {
			for _, later := range segments[i+1:] {
				if later == "exe" {
					return true
				}
			}
			if i > 0 && strings.HasPrefix(segments[i-1], "b") {
				return true
			}
		}
	}
	return false
}

func withPrivilegeHint(err error) error {
	message := err.Error()
	if strings.Contains(strings.ToLower(message), "permission denied") || strings.Contains(strings.ToLower(message), "operation not permitted") {
		return withRootHint(err)
	}
	return err
}

func withRootHint(err error) error {
	return fmt.Errorf("%w; real DumpDuck LaunchDaemon installs usually require sudo/root", err)
}

func plistPresence(path string) string {
	if _, err := os.Stat(path); err == nil {
		return "present"
	} else if errors.Is(err, os.ErrNotExist) {
		return "absent"
	}
	return "unreadable"
}

func formatTime(ts *time.Time, fallback string) string {
	if ts == nil {
		return fallback
	}
	return ts.Format(time.RFC3339)
}

func formatString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func printRootUsage(w io.Writer) {
	fmt.Fprintln(w, "DumpDuck commands:")
	fmt.Fprintln(w, "  config init --path <path> [--force]")
	fmt.Fprintln(w, "  config set <key> <value> --path <path>")
	fmt.Fprintln(w, "  status --config <path>")
	fmt.Fprintln(w, "  run --config <path>")
	fmt.Fprintln(w, "  install [--config <path>] [--binary <path>] [--plist <path>] [--dry-run] [--skip-load]")
	fmt.Fprintln(w, "  uninstall [--plist <path>] [--dry-run] [--skip-unload]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Install note: build a stable binary before install, for example:")
	fmt.Fprintln(w, "  go build -o ./bin/dumpduck ./cmd/dumpduck")
}

func printConfigUsage(w io.Writer) {
	fmt.Fprintln(w, "DumpDuck config commands:")
	fmt.Fprintln(w, "  config init --path <path> [--force]")
	fmt.Fprintf(w, "  config set <key> <value> --path <path>\n    supported keys: %s\n", strings.Join(config.SupportedSetKeys(), ", "))
}

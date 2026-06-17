package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/pflag"

	"github.com/ammarbinfaisal/dumpduck/internal/config"
	runtimepkg "github.com/ammarbinfaisal/dumpduck/internal/runtime"
	"github.com/ammarbinfaisal/dumpduck/internal/state"
)

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
	fmt.Fprintf(stdout, "Rclone destination: %s:%s\n", cfg.Upload.RcloneRemote, cfg.Upload.RclonePath)
	fmt.Fprintf(stdout, "State path: %s\n", cfg.State.Path)
	fmt.Fprintf(stdout, "Last upload: %s\n", formatTime(st.LastSuccessfulUploadTime, "never"))
	fmt.Fprintf(stdout, "Current window start: %s\n", formatTime(st.CurrentWindowStartTime, "none"))

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
	if len(args) != 0 {
		return errors.New("install does not accept arguments in phase 1")
	}
	fmt.Fprintln(stdout, "DumpDuck install is not implemented in phase 1 yet.")
	return nil
}

func runUninstall(args []string, stdout io.Writer) error {
	if len(args) != 0 {
		return errors.New("uninstall does not accept arguments in phase 1")
	}
	fmt.Fprintln(stdout, "DumpDuck uninstall is not implemented in phase 1 yet.")
	return nil
}

func formatTime(ts *time.Time, fallback string) string {
	if ts == nil {
		return fallback
	}
	return ts.Format(time.RFC3339)
}

func printRootUsage(w io.Writer) {
	fmt.Fprintln(w, "DumpDuck commands:")
	fmt.Fprintln(w, "  config init --path <path> [--force]")
	fmt.Fprintln(w, "  config set <key> <value> --path <path>")
	fmt.Fprintln(w, "  status --config <path>")
	fmt.Fprintln(w, "  run --config <path>")
	fmt.Fprintln(w, "  install")
	fmt.Fprintln(w, "  uninstall")
}

func printConfigUsage(w io.Writer) {
	fmt.Fprintln(w, "DumpDuck config commands:")
	fmt.Fprintln(w, "  config init --path <path> [--force]")
	fmt.Fprintf(w, "  config set <key> <value> --path <path>\n    supported keys: %s\n", strings.Join(config.SupportedSetKeys(), ", "))
}

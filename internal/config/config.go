package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const DefaultConfigPath = "/etc/dumpduck/config.yaml"

var supportedSetKeys = []string{
	"capture.bpf_filter",
	"capture.interface",
	"capture.output_dir",
	"logging.path",
	"state.path",
	"upload.frequency",
	"upload.rclone_path",
	"upload.rclone_remote",
}

type Config struct {
	Capture  CaptureConfig `yaml:"capture"`
	Upload   UploadConfig  `yaml:"upload"`
	State    StateConfig   `yaml:"state"`
	Logging  LoggingConfig `yaml:"logging"`
	Binaries BinaryConfig  `yaml:"binaries"`
}

type CaptureConfig struct {
	Interface      string `yaml:"interface"`
	BPFFilter      string `yaml:"bpf_filter"`
	Snaplen        int    `yaml:"snaplen"`
	OutputDir      string `yaml:"output_dir"`
	RotateInterval string `yaml:"rotate_interval"`
	TotalDuration  string `yaml:"total_duration"`
}

type UploadConfig struct {
	Frequency          string `yaml:"frequency"`
	RcloneRemote       string `yaml:"rclone_remote"`
	RclonePath         string `yaml:"rclone_path"`
	DeleteAfterSuccess bool   `yaml:"delete_after_success"`
}

type StateConfig struct {
	Path string `yaml:"path"`
}

type LoggingConfig struct {
	Path string `yaml:"path"`
}

type BinaryConfig struct {
	TCPDumpPath string `yaml:"tcpdump_path"`
	RclonePath  string `yaml:"rclone_path"`
}

func Default() Config {
	return Config{
		Capture: CaptureConfig{
			Interface:      "",
			BPFFilter:      "",
			Snaplen:        0,
			OutputDir:      "/var/lib/dumpduck/dumps",
			RotateInterval: "5m",
			TotalDuration:  "24h",
		},
		Upload: UploadConfig{
			Frequency:          "15m",
			RcloneRemote:       "gdrive",
			RclonePath:         "dumpduck",
			DeleteAfterSuccess: false,
		},
		State: StateConfig{
			Path: "/var/lib/dumpduck/state.json",
		},
		Logging: LoggingConfig{
			Path: "/var/log/dumpduck/dumpduck.log",
		},
		Binaries: BinaryConfig{
			TCPDumpPath: "/usr/sbin/tcpdump",
			RclonePath:  "/opt/homebrew/bin/rclone",
		},
	}
}

func SupportedSetKeys() []string {
	return slices.Clone(supportedSetKeys)
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}

	cfg := Default()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func InitFile(path string, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("config file %q already exists; pass --force to overwrite", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("check config %q: %w", path, err)
		}
	}

	return Save(path, Default())
}

func Save(path string, cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory for %q: %w", path, err)
	}

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write config %q: %w", path, err)
	}
	return nil
}

func (c *Config) Set(key, value string) error {
	switch key {
	case "upload.frequency":
		if err := validateDurationString("upload.frequency", value); err != nil {
			return err
		}
		c.Upload.Frequency = value
	case "upload.rclone_remote":
		if err := validateNonEmpty("upload.rclone_remote", value); err != nil {
			return err
		}
		c.Upload.RcloneRemote = value
	case "upload.rclone_path":
		if err := validateNonEmpty("upload.rclone_path", value); err != nil {
			return err
		}
		c.Upload.RclonePath = value
	case "capture.interface":
		c.Capture.Interface = value
	case "capture.bpf_filter":
		c.Capture.BPFFilter = value
	case "capture.output_dir":
		if err := validateNonEmpty("capture.output_dir", value); err != nil {
			return err
		}
		c.Capture.OutputDir = value
	case "state.path":
		if err := validateNonEmpty("state.path", value); err != nil {
			return err
		}
		c.State.Path = value
	case "logging.path":
		if err := validateNonEmpty("logging.path", value); err != nil {
			return err
		}
		c.Logging.Path = value
	default:
		return fmt.Errorf("unsupported config key %q (supported: %s)", key, strings.Join(supportedSetKeys, ", "))
	}

	return c.Validate()
}

func (c Config) Validate() error {
	if err := validateNonEmpty("capture.output_dir", c.Capture.OutputDir); err != nil {
		return err
	}
	if err := validateDurationString("capture.rotate_interval", c.Capture.RotateInterval); err != nil {
		return err
	}
	if err := validateDurationString("capture.total_duration", c.Capture.TotalDuration); err != nil {
		return err
	}
	if err := validateDurationString("upload.frequency", c.Upload.Frequency); err != nil {
		return err
	}
	if err := validateNonEmpty("upload.rclone_remote", c.Upload.RcloneRemote); err != nil {
		return err
	}
	if err := validateNonEmpty("upload.rclone_path", c.Upload.RclonePath); err != nil {
		return err
	}
	if err := validateNonEmpty("state.path", c.State.Path); err != nil {
		return err
	}
	if err := validateNonEmpty("logging.path", c.Logging.Path); err != nil {
		return err
	}
	if err := validateNonEmpty("binaries.tcpdump_path", c.Binaries.TCPDumpPath); err != nil {
		return err
	}
	if err := validateNonEmpty("binaries.rclone_path", c.Binaries.RclonePath); err != nil {
		return err
	}
	return nil
}

func validateDurationString(field, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s must not be empty", field)
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return fmt.Errorf("invalid value for %s: %q is not a valid duration: %w", field, value, err)
	}
	if duration <= 0 {
		return fmt.Errorf("invalid value for %s: %q must be greater than zero", field, value)
	}
	return nil
}

func validateNonEmpty(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s must not be empty", field)
	}
	return nil
}

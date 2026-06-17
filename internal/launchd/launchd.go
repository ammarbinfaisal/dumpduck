package launchd

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

type Service struct {
	Label             string
	ProgramArguments  []string
	RunAtLoad         bool
	KeepAlive         bool
	StandardOutPath   string
	StandardErrorPath string
}

var plistTemplate = template.Must(template.New("launchd-plist").Funcs(template.FuncMap{
	"esc": escapeXML,
	"boolTag": func(value bool) string {
		if value {
			return "true"
		}
		return "false"
	},
}).Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>{{esc .Label}}</string>
  <key>ProgramArguments</key>
  <array>
{{- range .ProgramArguments }}
    <string>{{esc .}}</string>
{{- end }}
  </array>
  <key>RunAtLoad</key>
  <{{boolTag .RunAtLoad}}/>
  <key>KeepAlive</key>
  <{{boolTag .KeepAlive}}/>
  <key>StandardOutPath</key>
  <string>{{esc .StandardOutPath}}</string>
  <key>StandardErrorPath</key>
  <string>{{esc .StandardErrorPath}}</string>
</dict>
</plist>
`))

func (s Service) PlistBytes() ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := plistTemplate.Execute(&buf, s); err != nil {
		return nil, fmt.Errorf("render launchd plist: %w", err)
	}
	return buf.Bytes(), nil
}

func (s Service) Validate() error {
	if strings.TrimSpace(s.Label) == "" {
		return fmt.Errorf("launchd label must not be empty")
	}
	if len(s.ProgramArguments) == 0 {
		return fmt.Errorf("launchd program arguments must not be empty")
	}
	for i, arg := range s.ProgramArguments {
		if strings.TrimSpace(arg) == "" {
			return fmt.Errorf("launchd program argument %d must not be empty", i)
		}
	}
	if strings.TrimSpace(s.StandardOutPath) == "" {
		return fmt.Errorf("launchd stdout path must not be empty")
	}
	if strings.TrimSpace(s.StandardErrorPath) == "" {
		return fmt.Errorf("launchd stderr path must not be empty")
	}
	return nil
}

func WritePlist(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create LaunchDaemon directory for %q: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write LaunchDaemon plist %q: %w", path, err)
	}
	return nil
}

func Bootstrap(plistPath string) error {
	output, err := exec.Command("launchctl", "bootstrap", "system", plistPath).CombinedOutput()
	if err == nil {
		return nil
	}

	message := strings.TrimSpace(string(output))
	if message == "" {
		return fmt.Errorf("launchctl bootstrap system %s: %w", plistPath, err)
	}
	return fmt.Errorf("launchctl bootstrap system %s: %w: %s", plistPath, err, message)
}

func Bootout(label string) error {
	target := "system/" + label
	output, err := exec.Command("launchctl", "bootout", target).CombinedOutput()
	if err == nil {
		return nil
	}

	message := strings.TrimSpace(string(output))
	if isNotLoadedMessage(message) {
		return nil
	}
	if message == "" {
		return fmt.Errorf("launchctl bootout %s: %w", target, err)
	}
	return fmt.Errorf("launchctl bootout %s: %w: %s", target, err, message)
}

func escapeXML(value string) string {
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(value)); err != nil {
		return value
	}
	return buf.String()
}

func isNotLoadedMessage(message string) bool {
	message = strings.ToLower(message)
	return strings.Contains(message, "no such process") ||
		strings.Contains(message, "could not find service") ||
		strings.Contains(message, "service not found") ||
		strings.Contains(message, "not loaded")
}

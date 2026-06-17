package launchd

import (
	"strings"
	"testing"
)

func TestPlistBytesIncludesRequiredFields(t *testing.T) {
	t.Parallel()

	service := Service{
		Label:             "com.dumpduck.service",
		ProgramArguments:  []string{"/usr/local/bin/dumpduck", "run", "--config", "/etc/dumpduck/config.yaml"},
		RunAtLoad:         true,
		KeepAlive:         true,
		StandardOutPath:   "/var/log/dumpduck/dumpduck.log",
		StandardErrorPath: "/var/log/dumpduck/dumpduck.log",
	}

	data, err := service.PlistBytes()
	if err != nil {
		t.Fatalf("plist bytes: %v", err)
	}

	content := string(data)
	for _, want := range []string{
		"<key>Label</key>",
		"<string>com.dumpduck.service</string>",
		"<key>ProgramArguments</key>",
		"<string>/usr/local/bin/dumpduck</string>",
		"<string>run</string>",
		"<string>--config</string>",
		"<string>/etc/dumpduck/config.yaml</string>",
		"<key>RunAtLoad</key>",
		"<true/>",
		"<key>KeepAlive</key>",
		"<key>StandardOutPath</key>",
		"<string>/var/log/dumpduck/dumpduck.log</string>",
		"<key>StandardErrorPath</key>",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("plist missing %q:\n%s", want, content)
		}
	}
}

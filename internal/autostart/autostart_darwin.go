//go:build darwin

package autostart

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
)

// Label is the LaunchAgent label.
const Label = "de.tlanfer." + Name

func plistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", Label+".plist"), nil
}

func enabled() (bool, error) {
	p, err := plistPath()
	if err != nil {
		return false, err
	}
	_, err = os.Stat(p)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func plist(exe string) []byte {
	return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>ProcessType</key>
	<string>Interactive</string>
</dict>
</plist>
`, xmlEscape(Label), xmlEscape(exe), BackgroundFlag))
}

// enable writes the LaunchAgent; it takes effect at the next login and does
// not start a second instance now.
func enable(exe string) error {
	if !filepath.IsAbs(exe) {
		return fmt.Errorf("autostart: executable path must be absolute: %q", exe)
	}
	p, err := plistPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, plist(exe), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func disable() error {
	p, err := plistPath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

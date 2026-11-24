// Package autostart manages "start on login" for the current user.
//
// Windows: HKCU\Software\Microsoft\Windows\CurrentVersion\Run value "linkctl".
// macOS:   ~/Library/LaunchAgents/de.tlanfer.linkctl.plist (RunAtLoad).
package autostart

// Name is the registry value / LaunchAgent label suffix.
const Name = "linkctl"

// BackgroundFlag is passed when started on login: run without opening the
// browser.
const BackgroundFlag = "--background"

// Enabled reports whether start-on-login is configured for exe.
func Enabled() (bool, error) { return enabled() }

// Enable registers exe (absolute path) to start on login.
func Enable(exe string) error { return enable(exe) }

// Disable removes the start-on-login registration.
func Disable() error { return disable() }

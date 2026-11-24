// Package platform wires the OS-specific camera backend and helpers.
package platform

import "github.com/tlanfer/linkctl/internal/camera"

// Backend returns the camera backend for this OS. logf receives diagnostics
// (may be nil).
func Backend(logf func(format string, args ...any)) camera.Backend { return backend(logf) }

// OpenBrowser opens url in the user's default browser.
func OpenBrowser(url string) error { return openBrowser(url) }

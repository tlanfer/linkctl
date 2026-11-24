// Package tray shows the system tray (Windows) or menu bar (macOS) icon.
package tray

// Menu describes the tray icon and its actions. All callbacks are invoked on
// an arbitrary goroutine and must not block for long.
type Menu struct {
	Tooltip string
	// IconPNG is a square PNG (at least 32x32). Platforms convert as needed.
	IconPNG []byte
	// TemplatePNG is a monochrome PNG used as macOS template image (optional).
	TemplatePNG []byte

	OnOpen func() // "Open control panel"

	// StartOnLogin reports the current state for the checkbox.
	StartOnLogin    func() bool
	SetStartOnLogin func(enabled bool) error

	OnQuit func() // "Quit"; Run returns after OnQuit.
}

// Run shows the tray icon and blocks until Quit is called or the user selects
// "Quit". It must be called from the main goroutine (macOS requires the main
// thread; cmd/linkctl locks it in init). onReady runs once the icon is shown.
func Run(m Menu, onReady func()) { run(m, onReady) }

// Quit removes the icon and makes Run return.
func Quit() { quit() }

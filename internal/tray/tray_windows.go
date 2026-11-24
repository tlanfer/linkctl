//go:build windows

package tray

import (
	"fyne.io/systray"

	"github.com/tlanfer/linkctl/internal/icon"
)

func run(m Menu, onReady func()) {
	systray.Run(func() {
		systray.SetIcon(icon.ICO(m.IconPNG))
		systray.SetTooltip(m.Tooltip)
		// Left click opens the panel; right click shows the menu.
		systray.SetOnTapped(func() { go m.OnOpen() })
		open := systray.AddMenuItem("Open control panel", "")
		login := systray.AddMenuItemCheckbox("Start on login", "", m.StartOnLogin())
		systray.AddSeparator()
		quitItem := systray.AddMenuItem("Quit linkctl", "")
		go func() {
			for {
				select {
				case <-open.ClickedCh:
					go m.OnOpen()
				case <-login.ClickedCh:
					want := !login.Checked()
					if err := m.SetStartOnLogin(want); err == nil {
						if want {
							login.Check()
						} else {
							login.Uncheck()
						}
					}
				case <-quitItem.ClickedCh:
					m.OnQuit()
					systray.Quit()
					return
				}
			}
		}()
		if onReady != nil {
			onReady()
		}
	}, nil)
}

func quit() { systray.Quit() }

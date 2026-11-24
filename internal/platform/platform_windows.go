//go:build windows

package platform

import (
	"golang.org/x/sys/windows"

	"github.com/tlanfer/linkctl/internal/camera"
	"github.com/tlanfer/linkctl/internal/camera/dshow"
)

func backend(logf func(string, ...any)) camera.Backend {
	b := dshow.New()
	b.Log = logf
	return b
}

func openBrowser(url string) error {
	verb, _ := windows.UTF16PtrFromString("open")
	u, err := windows.UTF16PtrFromString(url)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, u, nil, nil, windows.SW_SHOWNORMAL)
}

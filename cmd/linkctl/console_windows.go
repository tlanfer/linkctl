//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// attachConsole connects stdout/stderr to the parent console. The release
// binary is built with -H windowsgui (no console window when started from
// the tray/autostart), so `linkctl diag` would otherwise print nothing.
func attachConsole() {
	const attachParentProcess = ^uint32(0)
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")
	if r, _, _ := proc.Call(uintptr(attachParentProcess)); r == 0 {
		return // no parent console (started from Explorer) or already attached
	}
	if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout = f
		os.Stderr = f
	}
}

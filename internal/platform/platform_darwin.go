//go:build darwin

package platform

import (
	"fmt"
	"io"
	"os/exec"

	"github.com/tlanfer/linkctl/internal/camera"
	"github.com/tlanfer/linkctl/internal/camera/iokit"
)

func backend(logf func(string, ...any)) camera.Backend {
	b := iokit.New()
	b.Log = logf
	return b
}

func openBrowser(url string) error { return exec.Command("open", url).Start() }

func diagnose(w io.Writer) error {
	err := iokit.Diagnose(w)
	fmt.Fprintln(w, "-- video in use (CoreMediaIO) --")
	if lerr := loadCMIO(); lerr != nil {
		fmt.Fprintf(w, "FAIL %v\n", lerr)
		return err
	}
	id, name, ferr := findInsta360()
	switch {
	case ferr != nil:
		fmt.Fprintf(w, "FAIL enumerate: %v\n", ferr)
	case id == 0:
		fmt.Fprintln(w, "warn no Insta360 CoreMediaIO device found (in-use detection reports false)")
	default:
		fmt.Fprintf(w, "ok   CMIO device id=%d name=%q\n", id, name)
	}
	used, uerr := inUse()
	if uerr != nil {
		fmt.Fprintf(w, "FAIL in use: %v\n", uerr)
	} else {
		fmt.Fprintf(w, "ok   in use by a video app: %v\n", used)
	}
	return err
}

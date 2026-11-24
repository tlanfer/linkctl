//go:build windows

package platform

import (
	"fmt"
	"io"

	"github.com/tlanfer/linkctl/internal/camera"
	"github.com/tlanfer/linkctl/internal/camera/dshow"
)

func diagnose(w io.Writer) error {
	b := dshow.New()
	b.Log = func(f string, a ...any) { fmt.Fprintf(w, "  "+f+"\n", a...) }
	return camera.Diagnose(w, b)
}

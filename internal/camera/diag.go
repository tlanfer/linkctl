package camera

import (
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

// Diagnose runs a verbose, non-destructive check of the first camera found
// by b: identity, every control's range and value, and a write/restore test.
func Diagnose(w io.Writer, b Backend) error {
	fmt.Fprintln(w, "== enumerate")
	ds, err := b.List()
	if err != nil {
		return fmt.Errorf("enumerate: %w", err)
	}
	for _, d := range ds {
		fmt.Fprintf(w, "  %s (vid %04x pid %04x) %s\n", d.Name, d.VendorID, d.ProductID, d.Path)
	}
	if len(ds) == 0 {
		return fmt.Errorf("no Insta360 camera found")
	}
	fmt.Fprintln(w, "== open")
	drv, err := b.Open(ds[0])
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer drv.Close()
	if raw, err := drv.XUGet(XUFeatureUnit, XUSelDeviceInfo); err != nil {
		fmt.Fprintf(w, "  device info: ERROR %v\n", err)
	} else {
		s, f := ParseDeviceInfo(raw)
		fmt.Fprintf(w, "  serial %q firmware %q\n", s, f)
		for _, l := range strings.Split(strings.TrimRight(hex.Dump(raw), "\n"), "\n") {
			fmt.Fprintf(w, "    %s\n", l)
		}
	}
	cam := New(drv, ds[0])
	fmt.Fprintln(w, "== controls")
	for _, s := range cam.Specs() {
		v, err := cam.Get(s.Key)
		status := fmt.Sprint(v)
		if err != nil {
			status = "ERROR " + err.Error()
		}
		fmt.Fprintf(w, "  %-26s %-6s [%d..%d step %d def %d] = %s\n", s.Key, s.Kind, s.Min, s.Max, s.Step, s.Default, status)
	}
	fmt.Fprintln(w, "== write test (restores original values)")
	for _, key := range []string{Brightness, Pan, HDR} {
		s, _ := cam.Spec(key)
		orig, err := cam.Get(key)
		if err != nil {
			fmt.Fprintf(w, "  %s: read failed: %v\n", key, err)
			continue
		}
		want := s.Clamp(orig + 5)
		if s.Kind == KindBool {
			want = 1 - orig
		}
		if want == orig {
			want = s.Clamp(orig - 5)
		}
		got, err := cam.Set(key, want)
		if err != nil {
			fmt.Fprintf(w, "  %s: write %d FAILED: %v\n", key, want, err)
			continue
		}
		res := "OK"
		if got != want {
			res = fmt.Sprintf("MISMATCH (read back %d)", got)
		}
		fmt.Fprintf(w, "  %s: %d -> %d %s\n", key, orig, want, res)
		if _, err := cam.Set(key, orig); err != nil {
			fmt.Fprintf(w, "  %s: restore FAILED: %v\n", key, err)
		}
	}
	return nil
}

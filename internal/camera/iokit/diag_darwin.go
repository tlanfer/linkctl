//go:build darwin

package iokit

import (
	"encoding/hex"
	"fmt"
	"io"
	"runtime"

	"github.com/tlanfer/linkctl/internal/camera"
)

// Diagnose exercises every step of the IOKit path and prints the results.
// It is intended to be pasted back verbatim when something fails on a Mac.
func Diagnose(w io.Writer) error {
	p := func(format string, args ...any) { fmt.Fprintf(w, format+"\n", args...) }
	p("== linkctl macOS camera diagnostics ==")
	p("go %s %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH)

	if err := load(); err != nil {
		p("FAIL load frameworks: %v", err)
		return err
	}
	p("ok   IOKit and CoreFoundation loaded")

	logf := func(format string, args ...any) { p("     "+format, args...) }
	svcs, err := findServices(logf)
	if err != nil {
		p("FAIL enumerate: %v", err)
		return err
	}
	p("ok   %d Insta360 USB device(s) found", len(svcs))
	for _, s := range svcs {
		ioObjectRelease(s.entry)
	}
	if len(svcs) == 0 {
		return fmt.Errorf("no Insta360 camera found")
	}
	s := svcs[0]
	p("     using id=0x%x pid=0x%04x name=%q", s.id, s.productID, s.name)

	dev, err := openDeviceInterface(s.id, logf)
	if err != nil {
		p("FAIL open device interface: %v", err)
		return err
	}
	p("ok   device interface open (device NOT opened exclusively)")
	drv := &driver{dev: dev, xuLen: map[[2]uint8]int{}, logf: logf}
	defer drv.Close()

	if err := drv.loadTopology(); err != nil {
		p("FAIL topology: %v", err)
		return err
	}
	p("ok   VC interface=%d camera terminal=%d processing unit=%d", drv.top.VCInterface, drv.top.CameraTerminalID, drv.top.ProcessingUnitID)
	for _, xu := range drv.top.ExtensionUnits {
		p("     extension unit id=%d guid=%s", xu.ID, xu.GUID)
	}
	p("     XU mapping: feature unit -> %d, privacy unit -> %d", drv.xuUnit[camera.XUFeatureUnit], drv.xuUnit[camera.XUPrivacyUnit])

	p("-- standard controls --")
	failures := 0
	for _, key := range camera.StandardKeys {
		r, rerr := drv.StdRange(key)
		v, gerr := drv.StdGet(key)
		switch {
		case gerr != nil:
			failures++
			p("FAIL %-26s get: %v", key, gerr)
		case rerr != nil && rerr != camera.ErrUnsupported:
			p("warn %-26s value=%d range error: %v", key, v, rerr)
		case rerr == camera.ErrUnsupported:
			p("ok   %-26s value=%d", key, v)
		default:
			p("ok   %-26s value=%d range=[%d..%d] step=%d default=%d", key, v, r.Min, r.Max, r.Step, r.Default)
		}
	}

	p("-- extension unit --")
	info, err := drv.XUGet(camera.XUFeatureUnit, camera.XUSelDeviceInfo)
	if err != nil {
		failures++
		p("FAIL device info: %v", err)
	} else {
		serial, fw := camera.ParseDeviceInfo(info)
		p("ok   device info (%d bytes) serial=%q firmware=%q", len(info), serial, fw)
		fmt.Fprint(w, indent(hex.Dump(info)))
	}
	for _, sel := range []uint8{camera.XUSelFeatureMask, camera.XUSelExposureComp} {
		raw, err := drv.XUGet(camera.XUFeatureUnit, sel)
		if err != nil {
			failures++
			p("FAIL XU selector 0x%02x: %v", sel, err)
			continue
		}
		p("ok   XU selector 0x%02x = %s", sel, hex.EncodeToString(raw))
	}

	p("-- write test (values are restored) --")
	for _, t := range []struct {
		key   string
		delta int
	}{{camera.Brightness, 1}, {camera.Pan, 5}} {
		orig, err := drv.StdGet(t.key)
		if err != nil {
			failures++
			p("FAIL %s read: %v", t.key, err)
			continue
		}
		want := orig + t.delta
		if err := drv.StdSet(t.key, want); err != nil {
			failures++
			p("FAIL %s set %d: %v", t.key, want, err)
			continue
		}
		got, err := drv.StdGet(t.key)
		if err != nil || got != want {
			failures++
			p("FAIL %s wrote %d, read back %d (err %v)", t.key, want, got, err)
		} else {
			p("ok   %s %d -> %d", t.key, orig, got)
		}
		if err := drv.StdSet(t.key, orig); err != nil {
			failures++
			p("FAIL %s restore %d: %v", t.key, orig, err)
		}
	}

	if failures > 0 {
		p("== %d failure(s) ==", failures)
		return fmt.Errorf("%d diagnostic step(s) failed", failures)
	}
	p("== all checks passed ==")
	return nil
}

func indent(s string) string {
	out := make([]byte, 0, len(s)+len(s)/16)
	start := true
	for i := 0; i < len(s); i++ {
		if start {
			out = append(out, "     "...)
			start = false
		}
		out = append(out, s[i])
		if s[i] == '\n' {
			start = true
		}
	}
	return string(out)
}

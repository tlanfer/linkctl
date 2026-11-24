//go:build windows && hardware

package dshow

import (
	"testing"

	"github.com/tlanfer/linkctl/internal/camera"
)

// TestHardwareRoundTrip writes each control to a different value and back.
// Run with: go test -tags hardware ./internal/camera/dshow/
func TestHardwareRoundTrip(t *testing.T) {
	b := New()
	b.Log = t.Logf
	ds, err := b.List()
	if err != nil || len(ds) == 0 {
		t.Fatalf("no camera: %v", err)
	}
	cam, err := camera.Open(b, ds[0])
	if err != nil {
		t.Fatal(err)
	}
	defer cam.Close()
	t.Logf("info: %+v", cam.Info())
	before, errs := cam.ReadAll()
	t.Logf("values: %v errs: %v", before, errs)
	if len(errs) > 0 {
		t.Fatalf("read errors: %v", errs)
	}
	for _, s := range cam.Specs() {
		if s.Key == camera.Pan || s.Key == camera.Tilt || s.Key == camera.Zoom || s.AutoKey != "" {
			continue // motion and auto-driven controls tested separately
		}
		orig := before[s.Key]
		want := s.Clamp(orig + s.Step)
		if want == orig {
			want = s.Clamp(orig - s.Step)
		}
		got, err := cam.Set(s.Key, want)
		if err != nil {
			t.Errorf("%s: set %d: %v", s.Key, want, err)
			continue
		}
		if got != want {
			t.Errorf("%s: wrote %d read back %d", s.Key, want, got)
		}
		if _, err := cam.Set(s.Key, orig); err != nil {
			t.Errorf("%s: restore: %v", s.Key, err)
		}
	}
}

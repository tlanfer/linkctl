package camera

import "testing"

func TestParseDeviceInfo(t *testing.T) {
	raw := make([]byte, 234)
	copy(raw, "17B0000000000000000001")
	copy(raw[0x20:], "\x0100000000-0000-0000-0000-000000000000")
	copy(raw[0x61:], "v3.3.0.8_build3")
	copy(raw[0xA9:], "\x06Insta Puc2_flow")
	serial, fw := ParseDeviceInfo(raw)
	if serial != "17B0000000000000000001" || fw != "v3.3.0.8_build3" {
		t.Fatalf("serial=%q fw=%q", serial, fw)
	}
}

func TestExposureCompRoundTrip(t *testing.T) {
	for v := 0; v <= 100; v++ {
		if got := DecodeExposureComp(EncodeExposureComp(v)); got != v {
			t.Fatalf("%d -> %d", v, got)
		}
	}
	// Observed on hardware: raw 30 (0x1e) decodes to 55.
	if got := DecodeExposureComp([]byte{0x1e, 0}); got != 55 {
		t.Fatalf("raw 30 -> %d", got)
	}
	// Negative values floor like Python (link-studio): -1 -> 49.
	if got := DecodeExposureComp([]byte{0xff, 0xff}); got != 49 {
		t.Fatalf("raw -1 -> %d", got)
	}
}

func TestCameraVendorControls(t *testing.T) {
	drv := NewFakeDriver("17B0TEST000000000000")
	cam := New(drv, Descriptor{Name: "Insta360 Link 2", ProductID: 0x4C04})
	if cam.Info().Serial != "17B0TEST000000000000" || cam.Info().Model != "Insta360 Link 2" {
		t.Fatalf("info = %+v", cam.Info())
	}
	if _, err := cam.Set(HDR, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := cam.Set(Mirror, 1); err != nil {
		t.Fatal(err)
	}
	// Other feature bits (0x02C0 on hardware) must be preserved.
	if m := drv.FeatureMask(); m != 0x02C0|1<<FeatureBitHDR|1<<FeatureBitMirror {
		t.Fatalf("mask = %#x", m)
	}
	if v, _ := cam.Set(ExposureComp, 80); v != 80 {
		t.Fatalf("exposure = %d", v)
	}
	if _, err := cam.Set(Brightness, 101); err == nil {
		t.Fatal("out of range accepted")
	}
	vals, errs := cam.ReadAll()
	if len(errs) != 0 || vals[HDR] != 1 || vals[Mirror] != 1 || vals[ExposureComp] != 80 {
		t.Fatalf("vals=%v errs=%v", vals, errs)
	}
}

func TestAIFeatures(t *testing.T) {
	modeSwitchDelay = 0
	drv := NewFakeDriver("17B0TEST000000000000")
	cam := New(drv, Descriptor{Name: "Insta360 Link 2", ProductID: 0x4C04})

	// Tracking: reset to 0 first, then mode 1 with flag 0.
	if v, err := cam.Set(AIMode, 1); err != nil || v != 1 {
		t.Fatalf("set tracking: %d %v", v, err)
	}
	if w := drv.VideoModeWrites(); len(w) != 2 || w[0] != [2]byte{0, 0} || w[1] != [2]byte{1, 0} {
		t.Fatalf("mode writes = %v", w)
	}
	// DeskView carries flag 0x10 (as the official app sends it).
	_, _ = cam.Set(AIMode, AIModeDeskView)
	if w := drv.VideoModeWrites(); w[len(w)-1] != [2]byte{6, 0x10} {
		t.Fatalf("deskview write = %v", w[len(w)-1])
	}
	// Normal is written directly, without the reset handshake.
	n := len(drv.VideoModeWrites())
	_, _ = cam.Set(AIMode, 0)
	if w := drv.VideoModeWrites(); len(w) != n+1 || w[n] != [2]byte{0, 0} {
		t.Fatalf("normal writes = %v", w[n:])
	}
	if _, err := cam.Set(AIMode, 3); err == nil {
		t.Fatal("removed mode accepted")
	}

	// Composition: 0 off, 1..3 = framing written to 0x13 plus the enable bit.
	if v, _ := cam.Get(Composition); v != 0 {
		t.Fatalf("composition default = %d, want off", v)
	}
	if v, err := cam.Set(Composition, 1); err != nil || v != 1 {
		t.Fatalf("set head: %d %v", v, err)
	}
	if raw, _ := drv.XUGet(XUFeatureUnit, XUSelFraming); raw[0] != 1 || drv.FeatureMask()&1 == 0 {
		t.Fatalf("head: framing %d mask %#x", raw[0], drv.FeatureMask())
	}
	if v, _ := cam.Set(Composition, 3); v != 3 {
		t.Fatalf("full body read back %d", v)
	}
	if v, _ := cam.Set(Composition, 0); v != 0 || drv.FeatureMask()&1 != 0 {
		t.Fatalf("off: %d mask %#x", v, drv.FeatureMask())
	}
	// Tracking speed is inverted in firmware: Fast (2) -> 0, Slow (0) -> 2.
	_, _ = cam.Set(TrackingSpeed, 2)
	if raw, _ := drv.XUGet(XUFeatureUnit, XUSelTrackSpeed); raw[0] != 0 {
		t.Fatalf("tracking speed fast written as %d", raw[0])
	}
	if v, _ := cam.Get(TrackingSpeed); v != 2 {
		t.Fatalf("tracking speed read back %d", v)
	}
	_, _ = cam.Set(Composition, 2)
	vals, errs := cam.ReadAll()
	if len(errs) != 0 || vals[Composition] != 2 || vals[TrackingSpeed] != 2 {
		t.Fatalf("vals=%v errs=%v", vals, errs)
	}
}

func TestMigrateValues(t *testing.T) {
	cases := []struct {
		in   map[string]int
		want int
	}{
		{map[string]int{"smart_composition": 0, "composition_framing": 2}, 0},
		{map[string]int{"smart_composition": 1, "composition_framing": 0}, 1},
		{map[string]int{"smart_composition": 1, "composition_framing": 2}, 3},
		{map[string]int{"smart_composition": 1}, 2},
	}
	for _, c := range cases {
		if !MigrateValues(c.in) || c.in[Composition] != c.want || len(c.in) != 1 {
			t.Errorf("got %v, want composition %d", c.in, c.want)
		}
	}
	if MigrateValues(map[string]int{Pan: 1}) {
		t.Error("migrated values without legacy keys")
	}
}

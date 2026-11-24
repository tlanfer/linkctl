package service

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tlanfer/linkctl/internal/camera"
	"github.com/tlanfer/linkctl/internal/config"
)

// inUse is the simulated "a video app uses the camera" flag.
var inUse atomic.Bool

func newTestService(t *testing.T) (*Service, *camera.FakeBackend, *config.Store) {
	t.Helper()
	inUse.Store(false)
	fb := camera.NewFakeBackend()
	store := config.NewMemory(config.Defaults())
	_, _ = store.Update(func(c *config.Config) error { c.PollIntervalMS = 200; return nil })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := New(fb, store, log, Options{DiscoverInterval: 20 * time.Millisecond, MotionTick: 10 * time.Millisecond, StreamSettle: 10 * time.Millisecond, ModeSettle: 10 * time.Millisecond,
		InUse: func() (bool, error) { return inUse.Load(), nil }})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go s.Run(ctx)
	waitFor(t, "connect", func() bool { return s.Snapshot().Connected })
	return s, fb, store
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestConnectSnapshot(t *testing.T) {
	s, _, _ := newTestService(t)
	snap := s.Snapshot()
	if snap.Device == nil || snap.Device.Serial != "17B0FAKE0000000000001" {
		t.Fatalf("device = %+v", snap.Device)
	}
	if snap.DeviceID != "17b0fake0000000000001" {
		t.Errorf("device id = %q", snap.DeviceID)
	}
	if got := snap.Values[camera.Brightness]; got != 50 {
		t.Errorf("brightness = %d", got)
	}
	if got := snap.Values[camera.ExposureComp]; got != 50 {
		t.Errorf("exposure = %d", got)
	}
	if len(snap.Controls) != len(camera.DefaultSpecs()) {
		t.Errorf("controls = %d", len(snap.Controls))
	}
}

func TestSetControlsClampsAndNotifies(t *testing.T) {
	s, _, _ := newTestService(t)
	ch, cancel := s.Subscribe()
	defer cancel()
	if err := s.SetControls(map[string]int{camera.Brightness: 500, camera.HDR: 1}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("no notification")
	}
	v := s.Snapshot().Values
	if v[camera.Brightness] != 100 || v[camera.HDR] != 1 {
		t.Fatalf("values = %v", v)
	}
	if err := s.SetControls(map[string]int{"bogus": 1}); err == nil {
		t.Fatal("expected error for unknown key")
	}
}

func TestAutoOrdering(t *testing.T) {
	s, fb, _ := newTestService(t)
	// Turning auto off and setting the temperature in one call must write the
	// auto flag first, otherwise the temperature write would be overridden.
	if err := s.SetControls(map[string]int{camera.WhiteBalanceTemp: 4000, camera.WhiteBalanceAuto: 0}); err != nil {
		t.Fatal(err)
	}
	w := fb.Last().Writes
	if len(w) < 2 || w[0] != camera.WhiteBalanceAuto || w[1] != camera.WhiteBalanceTemp {
		t.Fatalf("write order = %v", w)
	}
}

func TestExternalChangeDetectedByPoll(t *testing.T) {
	s, fb, _ := newTestService(t)
	fb.Last().Poke(camera.Contrast, 77)
	waitFor(t, "poll", func() bool { return s.Snapshot().Values[camera.Contrast] == 77 })
}

func TestHotplug(t *testing.T) {
	s, fb, _ := newTestService(t)
	fb.Plug(false)
	s.RequestPoll()
	waitFor(t, "disconnect", func() bool { return !s.Snapshot().Connected })
	if err := s.SetControls(map[string]int{camera.Brightness: 1}); err != ErrNoCamera {
		t.Fatalf("err = %v", err)
	}
	fb.Plug(true)
	waitFor(t, "reconnect", func() bool { return s.Snapshot().Connected })
}

func TestNudgeAndCenter(t *testing.T) {
	s, _, _ := newTestService(t)
	if err := s.Nudge(map[string]int{camera.Pan: -10, camera.Zoom: 50}); err != nil {
		t.Fatal(err)
	}
	v := s.Snapshot().Values
	if v[camera.Pan] != -10 || v[camera.Zoom] != 150 {
		t.Fatalf("values = %v", v)
	}
	if err := s.Nudge(map[string]int{camera.Pan: -1000}); err != nil {
		t.Fatal(err)
	}
	if got := s.Snapshot().Values[camera.Pan]; got != -145 {
		t.Fatalf("pan clamped = %d", got)
	}
	if err := s.Nudge(map[string]int{camera.Brightness: 1}); err == nil {
		t.Fatal("expected error")
	}
	if err := s.Center(); err != nil {
		t.Fatal(err)
	}
	v = s.Snapshot().Values
	if v[camera.Pan] != 0 || v[camera.Tilt] != 0 || v[camera.Zoom] != 100 {
		t.Fatalf("center = %v", v)
	}
}

func TestMoveStopsAfterTTL(t *testing.T) {
	s, _, _ := newTestService(t)
	if err := s.Move(map[string]float64{camera.Pan: 1}, 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "motion", func() bool { return s.Snapshot().Values[camera.Pan] > 0 })
	waitFor(t, "auto stop", func() bool { return !s.Moving() })
	stopped := s.Snapshot().Values[camera.Pan]
	time.Sleep(60 * time.Millisecond)
	if got := s.Snapshot().Values[camera.Pan]; got != stopped {
		t.Fatalf("pan kept moving: %d -> %d", stopped, got)
	}
	if stopped > 15 {
		t.Fatalf("moved too far for 100ms at 60°/s: %d", stopped)
	}
}

func TestMoveUsesSpeedControl(t *testing.T) {
	s, fb, _ := newTestService(t)
	drv := fb.Last()
	if err := s.Move(map[string]float64{camera.Pan: 1, camera.Tilt: 0.5}, 150*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "driving", func() bool { p, tl := drv.Driving(); return p > 0 && tl > 0 })
	waitFor(t, "live readout", func() bool { return s.Snapshot().Values[camera.Pan] > 0 })
	// TTL expires -> eased stop -> stop command on the camera.
	waitFor(t, "stopped", func() bool { p, tl := drv.Driving(); return p == 0 && tl == 0 && !s.Moving() })
	log := drv.DriveLog
	if len(log) < 3 {
		t.Fatalf("expected a speed ramp, got %v", log)
	}
	if last := log[len(log)-1]; last != [4]byte{0, 1, 0, 1} {
		t.Fatalf("last command %v is not a stop", last)
	}
	// Speeds ramp up rather than jumping to the maximum.
	if first := log[0]; first[1] >= maxDriveSpeed {
		t.Fatalf("first speed %d is not eased", first[1])
	}
	// Tilt up must be sent as firmware dir 0xff.
	for _, c := range log {
		if c[3] > 1 && c[2] != 0xff {
			t.Fatalf("tilt up sent with dir %#x", c[2])
		}
	}
	for _, w := range drv.Writes {
		if w == camera.Pan || w == camera.Tilt {
			t.Fatalf("absolute pan/tilt write %q while speed control is available", w)
		}
	}
}

func TestCenterStopsDriving(t *testing.T) {
	s, fb, _ := newTestService(t)
	drv := fb.Last()
	_ = s.Move(map[string]float64{camera.Pan: 1}, 2*time.Second)
	waitFor(t, "driving", func() bool { p, _ := drv.Driving(); return p > 0 })
	if err := s.Center(); err != nil {
		t.Fatal(err)
	}
	if p, _ := drv.Driving(); p != 0 {
		t.Fatal("gimbal still turning after Center")
	}
}

func TestMoveFallbackWithoutSpeedControl(t *testing.T) {
	fb := camera.NewFakeBackend()
	fb.NoDrive = true
	store := config.NewMemory(config.Defaults())
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := New(fb, store, log, Options{DiscoverInterval: 10 * time.Millisecond, MotionTick: 10 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)
	waitFor(t, "connect", func() bool { return s.Snapshot().Connected })
	_ = s.Move(map[string]float64{camera.Pan: 1}, 100*time.Millisecond)
	waitFor(t, "stepped motion", func() bool { return s.Snapshot().Values[camera.Pan] > 0 })
	waitFor(t, "stop", func() bool { return !s.Moving() })
	if len(fb.Last().DriveLog) != 0 {
		t.Fatal("speed commands sent to a camera without speed control")
	}
}

func TestExpoCurve(t *testing.T) {
	if expo(0) != 0 || expo(1) != 1 || expo(-1) != -1 {
		t.Fatal("expo must keep 0 and full deflection")
	}
	if e := expo(0.5); e > 0.3 || e < 0.2 {
		t.Fatalf("expo(0.5) = %.3f, want fine control near the center", e)
	}
	if expo(-0.5) != -expo(0.5) {
		t.Fatal("expo must be symmetric")
	}
	// small deflections still move (minimum camera speed step)
	if driveSpeed(expo(0.05)) != 1 {
		t.Fatalf("tiny deflection speed = %d", driveSpeed(expo(0.05)))
	}
}

func TestDeskViewTiltsDown(t *testing.T) {
	s, _, _ := newTestService(t)
	if err := s.SetControls(map[string]int{camera.AIMode: camera.AIModeDeskView}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "tilt down", func() bool { return s.Snapshot().Values[camera.Tilt] == camera.DeskViewTilt })
}

func TestPresetWithAIMode(t *testing.T) {
	s, fb, _ := newTestService(t)
	// A fixed view in normal mode ...
	_ = s.SetControls(map[string]int{camera.Pan: 30, camera.Tilt: -5, camera.Zoom: 150})
	fixed, err := s.SavePreset("Fixed", []string{"position", "ai"})
	if err != nil {
		t.Fatal(err)
	}
	if fixed.Values[camera.AIMode] != 0 {
		t.Fatalf("ai mode not captured: %v", fixed.Values)
	}
	// ... and a tracking preset.
	_ = s.SetControls(map[string]int{camera.AIMode: camera.AIModeTracking, camera.TrackingSpeed: 2})
	track, err := s.SavePreset("Track", []string{"ai", "position"})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.ApplyPreset(fixed.ID); err != nil {
		t.Fatal(err)
	}
	v := s.Snapshot().Values
	if v[camera.AIMode] != 0 || v[camera.Pan] != 30 || v[camera.Zoom] != 150 {
		t.Fatalf("fixed preset: %v", v)
	}

	// Applying the tracking preset switches the mode and leaves position alone.
	_ = s.SetControls(map[string]int{camera.Pan: -20})
	fb.Last().Writes = nil
	if err := s.ApplyPreset(track.ID); err != nil {
		t.Fatal(err)
	}
	v = s.Snapshot().Values
	if v[camera.AIMode] != camera.AIModeTracking || v[camera.TrackingSpeed] != 2 {
		t.Fatalf("tracking preset: %v", v)
	}
	for _, w := range fb.Last().Writes {
		if w == camera.Pan || w == camera.Tilt || w == camera.Zoom {
			t.Fatalf("tracking preset wrote position %q", w)
		}
	}
	if s.Snapshot().ActivePreset != track.ID {
		t.Fatal("tracking preset not active")
	}
	// The DeskView auto-tilt must not override a preset's own position.
	_ = s.SetControls(map[string]int{camera.AIMode: camera.AIModeDeskView})
	waitFor(t, "auto tilt", func() bool { return s.Snapshot().Values[camera.Tilt] == camera.DeskViewTilt })
	_ = s.SetControls(map[string]int{camera.Tilt: -30})
	desk, _ := s.SavePreset("Desk", []string{"ai", "position"})
	_ = s.ApplyPreset(fixed.ID)
	_ = s.ApplyPreset(desk.ID)
	time.Sleep(50 * time.Millisecond) // longer than ModeSettle in tests
	if got := s.Snapshot().Values[camera.Tilt]; got != -30 {
		t.Fatalf("desk preset tilt = %d, want its own -30", got)
	}
}

func TestLegacyPresetMigration(t *testing.T) {
	cfg := config.Defaults()
	cfg.Presets = []config.Preset{{ID: "x", Name: "Old", Groups: []string{"ai"},
		Values: map[string]int{"smart_composition": 1, "composition_framing": 2, "ai_mode": 1}}}
	store := config.NewMemory(cfg)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	_ = New(camera.NewFakeBackend(), store, log, Options{})
	got := store.Get().Presets[0].Values
	if got[camera.Composition] != 3 || len(got) != 2 {
		t.Fatalf("migrated values = %v", got)
	}
}

func TestEaseRampsSmoothly(t *testing.T) {
	v := 0.0
	for i := 0; i < 5; i++ { // 100ms of 20ms ticks: well below full speed
		v = ease(v, 1, 20*time.Millisecond)
	}
	if v < 0.4 || v > 0.6 {
		t.Fatalf("after 100ms: %.2f, want ~0.5 (tau 150ms)", v)
	}
	for i := 0; i < 15; i++ { // 300ms: decelerates faster than it accelerated
		v = ease(v, 0, 20*time.Millisecond)
	}
	if v > restVelocity {
		t.Fatalf("not at rest after 300ms: %.3f", v)
	}
}

func TestMoveExplicitStop(t *testing.T) {
	s, _, _ := newTestService(t)
	_ = s.Move(map[string]float64{camera.Tilt: -1}, 2*time.Second)
	waitFor(t, "motion", func() bool { return s.Snapshot().Values[camera.Tilt] < 0 })
	_ = s.Move(map[string]float64{camera.Tilt: 0}, 0)
	// Stopping eases out over ~tauDown instead of halting abruptly.
	waitFor(t, "eased stop", func() bool { return !s.Moving() })
	stopped := s.Snapshot().Values[camera.Tilt]
	time.Sleep(50 * time.Millisecond)
	if got := s.Snapshot().Values[camera.Tilt]; got != stopped {
		t.Fatalf("tilt kept moving after stop: %d -> %d", stopped, got)
	}
	if err := s.Move(map[string]float64{camera.Brightness: 1}, 0); err == nil {
		t.Fatal("expected error")
	}
}

func TestPresets(t *testing.T) {
	s, _, store := newTestService(t)
	_ = s.SetControls(map[string]int{camera.Pan: 30, camera.Tilt: -10, camera.Zoom: 200, camera.Brightness: 70})
	desk, err := s.SavePreset("Desk", []string{"position"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := desk.Values[camera.Brightness]; ok {
		t.Fatal("position preset captured picture values")
	}
	if desk.Values[camera.Pan] != 30 || desk.Values[camera.Zoom] != 200 {
		t.Fatalf("desk = %v", desk.Values)
	}
	if _, err := s.SavePreset("desk", []string{"position"}); err == nil {
		t.Fatal("duplicate name accepted")
	}
	if _, err := s.SavePreset("x", nil); err == nil {
		t.Fatal("empty groups accepted")
	}
	if s.Snapshot().ActivePreset != desk.ID {
		t.Fatal("saved preset not active")
	}

	_ = s.Center()
	if s.Snapshot().ActivePreset != "" {
		t.Fatal("active preset not cleared after moving")
	}
	_ = s.SetControls(map[string]int{camera.Brightness: 20})
	if err := s.ApplyPreset("DESK"); err != nil {
		t.Fatal(err)
	}
	v := s.Snapshot().Values
	if v[camera.Pan] != 30 || v[camera.Tilt] != -10 || v[camera.Zoom] != 200 || v[camera.Brightness] != 20 {
		t.Fatalf("after apply = %v", v)
	}
	if s.Snapshot().ActivePreset != desk.ID {
		t.Fatal("applied preset not active")
	}

	name := "Desk wide"
	if err := s.UpdatePreset(desk.ID, PresetUpdate{Name: &name}); err != nil {
		t.Fatal(err)
	}
	both, err := s.SavePreset("Both", []string{"picture", "position"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReorderPresets([]string{both.ID, desk.ID}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReorderPresets([]string{both.ID, both.ID}); err == nil {
		t.Fatal("bad order accepted")
	}
	if err := s.SetDefaultPreset(desk.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeletePreset(desk.ID); err != nil {
		t.Fatal(err)
	}
	c := store.Get()
	if len(c.Presets) != 1 || c.Presets[0].Name != "Both" || c.DefaultPreset != "" {
		t.Fatalf("config = %+v", c)
	}
	if err := s.ApplyPreset("nope"); err != ErrPresetNotFound {
		t.Fatalf("err = %v", err)
	}
}

func TestPresetKeepsAutoWhiteBalance(t *testing.T) {
	s, fb, _ := newTestService(t)
	p, err := s.SavePreset("Auto", []string{"picture"})
	if err != nil {
		t.Fatal(err)
	}
	fb.Last().Writes = nil
	if err := s.ApplyPreset(p.ID); err != nil {
		t.Fatal(err)
	}
	for _, w := range fb.Last().Writes {
		if w == camera.WhiteBalanceTemp || w == camera.Focus {
			t.Fatalf("manual write %q would disable auto mode", w)
		}
	}
}

func TestDefaultPresetAppliedWhenStreamStarts(t *testing.T) {
	s, _, _ := newTestService(t)
	_ = s.SetControls(map[string]int{camera.Pan: 40})
	p, _ := s.SavePreset("Start", []string{"position"})
	_ = s.SetDefaultPreset(p.ID)
	_ = s.SetControls(map[string]int{camera.Pan: 0}) // camera parked while idle
	time.Sleep(50 * time.Millisecond)
	if s.Snapshot().Values[camera.Pan] != 0 {
		t.Fatal("default preset applied without a stream")
	}
	inUse.Store(true)
	s.RequestPoll()
	waitFor(t, "streaming", func() bool { return s.Snapshot().Streaming })
	waitFor(t, "default applied", func() bool { return s.Snapshot().Values[camera.Pan] == 40 })
	inUse.Store(false)
	s.RequestPoll()
	waitFor(t, "stream stopped", func() bool { return !s.Snapshot().Streaming })
}

func TestDeviceIDFallback(t *testing.T) {
	if got := deviceID(nil, "My Desk Cam!"); got != "my_desk_cam" {
		t.Fatalf("got %q", got)
	}
	if got := deviceID(&camera.Info{}, ""); got != "link" {
		t.Fatalf("got %q", got)
	}
}

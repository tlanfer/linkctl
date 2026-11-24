package service

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/tlanfer/linkctl/internal/camera"
)

// Continuous motion.
//
// Pan/tilt use the camera's own speed control (XU 9/0x16, as the official
// Insta360 app does): we only send a speed and the gimbal moves smoothly by
// itself. Zoom has no speed control, so it is stepped with absolute writes;
// pan/tilt fall back to that on cameras without speed control.

// Absolute stepping rates at velocity 1.0, in units per second.
var motionRates = map[string]float64{
	camera.Pan:  60, // degrees/s (fallback only)
	camera.Tilt: 45, // degrees/s (fallback only)
	camera.Zoom: 150,
}

// Speed control: |velocity| 1.0 maps to maxDriveSpeed (~2 deg/s per step,
// so ~47 deg/s at full deflection).
const (
	maxDriveSpeed  = 24
	driveMinPeriod = 40 * time.Millisecond  // rate limit for speed updates
	liveReadPeriod = 150 * time.Millisecond // position readout while driving
)

// Velocity easing: commanded velocities are smoothed exponentially so that
// starts and stops are soft (like the official app, which ramps speed 4 -> 8).
// Stopping is quicker than starting to keep overshoot after release small.
const (
	tauUp        = 120 * time.Millisecond
	tauDown      = 60 * time.Millisecond
	restVelocity = 0.02 // below this smoothed |velocity| a stopping axis rests
)

// Expo (as on RC transmitters): out = e*x^3 + (1-e)*x. Full deflection stays
// full speed while the center gets much finer control. With e = 0.75 and
// maxDriveSpeed 24 (~2 deg/s per step):
//
//	deflection 0.1 -> ~1 deg/s, 0.25 -> ~3, 0.5 -> ~12, 0.75 -> ~26, 1 -> ~47
//
// Applied to all axes (also zoom) after easing, for UI and MQTT alike.
const expoFactor = 0.75

func expo(x float64) float64 { return expoFactor*x*x*x + (1-expoFactor)*x }

// DefaultMoveTTL is how long a Move command keeps the camera moving unless
// it is repeated.
const DefaultMoveTTL = 500 * time.Millisecond

type motionState struct {
	target   map[string]float64 // commanded velocity per axis (-1..1)
	cur      map[string]float64 // smoothed velocity
	pos      map[string]float64 // fractional position (absolute stepping)
	deadline time.Time
	wake     chan struct{}

	sent    [2]int // last Drive(pan, tilt) speeds sent
	sentAt  time.Time
	readAt  time.Time
	driving bool // a non-zero Drive is in effect on the camera
}

func (m *motionState) active() bool { return m.cur != nil }

// Move starts or updates continuous motion. vel maps pan/tilt/zoom to a
// velocity in [-1, 1]. Motion eases out after ttl unless Move is called
// again; all-zero velocities ease out immediately.
func (s *Service) Move(vel map[string]float64, ttl time.Duration) error {
	if s.camera() == nil {
		return ErrNoCamera
	}
	if ttl <= 0 || ttl > 5*time.Second {
		ttl = DefaultMoveTTL
	}
	clean := map[string]float64{}
	for k, v := range vel {
		if _, ok := motionRates[k]; !ok {
			return fmt.Errorf("move supports pan, tilt and zoom, not %q", k)
		}
		if math.IsNaN(v) {
			continue
		}
		v = math.Max(-1, math.Min(1, v))
		if v != 0 {
			clean[k] = v
		}
	}
	s.mu.Lock()
	m := &s.motion
	if len(clean) == 0 {
		m.target = map[string]float64{} // ease out
		s.mu.Unlock()
		return nil
	}
	if !m.active() {
		m.cur = map[string]float64{}
		m.pos = map[string]float64{}
	}
	for k := range clean {
		if _, ok := m.pos[k]; !ok {
			m.pos[k] = float64(s.values[k])
		}
		if _, ok := m.cur[k]; !ok {
			m.cur[k] = 0
		}
	}
	m.target = clean
	m.deadline = time.Now().Add(ttl)
	wake := m.wake
	s.mu.Unlock()
	select {
	case wake <- struct{}{}:
	default:
	}
	return nil
}

// stopMotion halts immediately (before absolute moves such as presets, and
// on shutdown). A running speed command is stopped on the camera.
func (s *Service) stopMotion() {
	s.mu.Lock()
	m := &s.motion
	wasDriving := m.driving
	m.target, m.cur, m.pos = nil, nil, nil
	m.driving, m.sent = false, [2]int{}
	cam := s.cam
	s.mu.Unlock()
	if wasDriving && cam != nil {
		s.op.Lock()
		if err := cam.Drive(0, 0); err != nil {
			s.log.Warn("stopping gimbal failed", "err", err)
		}
		s.op.Unlock()
	}
}

// Moving reports whether continuous motion (including easing out) is active.
func (s *Service) Moving() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.motion.active()
}

func (s *Service) motionLoop(ctx context.Context) {
	wake := make(chan struct{}, 1)
	s.mu.Lock()
	s.motion.wake = wake
	s.mu.Unlock()
	tick := time.NewTicker(s.opts.MotionTick)
	defer tick.Stop()
	last := time.Now()
	for {
		select {
		case <-ctx.Done():
			s.stopMotion()
			return
		case <-wake:
			last = time.Now()
			continue
		case now := <-tick.C:
			dt := now.Sub(last)
			last = now
			s.motionStep(now, dt)
		}
	}
}

func ease(cur, target float64, dt time.Duration) float64 {
	tau := tauUp
	if math.Abs(target) < math.Abs(cur) || target*cur < 0 {
		tau = tauDown
	}
	a := 1 - math.Exp(-dt.Seconds()/tau.Seconds())
	return cur + (target-cur)*a
}

// driveSpeed maps a smoothed velocity to a signed camera speed step.
func driveSpeed(v float64) int {
	if v == 0 {
		return 0
	}
	sp := int(math.Round(math.Abs(v) * maxDriveSpeed))
	sp = max(sp, 1)
	if v < 0 {
		return -sp
	}
	return sp
}

func (s *Service) motionStep(now time.Time, dt time.Duration) {
	if dt > 100*time.Millisecond {
		dt = 100 * time.Millisecond // after a stall, do not jump
	}
	cam := s.camera()
	s.mu.Lock()
	m := &s.motion
	if !m.active() || cam == nil {
		m.target, m.cur, m.pos = nil, nil, nil
		m.driving = false
		s.mu.Unlock()
		return
	}
	if now.After(m.deadline) {
		m.target = map[string]float64{}
	}
	useDrive := cam.CanDrive()
	want := map[string]int{} // absolute writes
	moving := false
	for k := range m.cur {
		v := ease(m.cur[k], m.target[k], dt)
		if m.target[k] == 0 && math.Abs(v) < restVelocity {
			v = 0
		}
		m.cur[k] = v
		if v != 0 {
			moving = true
		}
		if v == 0 || (useDrive && (k == camera.Pan || k == camera.Tilt)) {
			continue
		}
		spec, _ := cam.Spec(k)
		p := m.pos[k] + expo(v)*motionRates[k]*dt.Seconds()
		p = math.Max(float64(spec.Min), math.Min(float64(spec.Max), p))
		m.pos[k] = p
		if r := int(math.Round(p)); r != s.values[k] {
			want[k] = r
		}
	}

	// Speed control for pan/tilt: send when the quantized speed changes,
	// rate-limited (stops are sent immediately).
	var drive *[2]int
	if useDrive {
		next := [2]int{driveSpeed(expo(m.cur[camera.Pan])), driveSpeed(expo(m.cur[camera.Tilt]))}
		stopping := next == [2]int{}
		if next != m.sent && (stopping || now.Sub(m.sentAt) >= driveMinPeriod) {
			drive = &next
			m.sent, m.sentAt = next, now
			m.driving = !stopping
		}
	}
	readLive := useDrive && m.driving && now.Sub(m.readAt) >= liveReadPeriod
	if readLive {
		m.readAt = now
	}

	finished := !moving && len(m.target) == 0
	if finished {
		m.target, m.cur, m.pos = nil, nil, nil
		if m.driving || m.sent != [2]int{} {
			stop := [2]int{}
			drive = &stop
		}
		m.driving, m.sent = false, [2]int{}
	}
	s.mu.Unlock()

	written := map[string]int{}
	s.op.Lock()
	if drive != nil {
		if err := cam.Drive(drive[0], drive[1]); err != nil {
			s.log.Warn("gimbal speed command failed", "err", err)
		}
	}
	// Absolute writes without read-back: each USB write takes ~18 ms.
	for k, v := range want {
		if err := cam.Write(k, v); err != nil {
			s.log.Debug("motion step failed", "key", k, "err", err)
			continue
		}
		written[k] = v
	}
	if readLive || (finished && useDrive) {
		if p, t, err := cam.PanTiltPrecise(); err == nil {
			written[camera.Pan] = int(math.Round(p))
			written[camera.Tilt] = int(math.Round(t))
		}
	}
	s.op.Unlock()
	if len(written) > 0 {
		s.merge(written, nil)
	}
	if finished {
		s.notify()
		if useDrive {
			// The gimbal decelerates for a moment after the stop command.
			time.AfterFunc(400*time.Millisecond, s.RequestPoll)
		}
	}
}

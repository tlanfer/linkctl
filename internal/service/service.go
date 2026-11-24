// Package service owns the camera connection: hot-plug detection, polling,
// change notification, PTZ motion and presets. The web UI and MQTT bridge
// are thin adapters on top of it.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/tlanfer/linkctl/internal/camera"
	"github.com/tlanfer/linkctl/internal/config"
)

// ErrNoCamera is returned when an operation needs a connected camera.
var ErrNoCamera = errors.New("no camera connected")

// ControlState is a control's metadata plus its current value.
type ControlState struct {
	camera.ControlSpec
	Value *int   `json:"value"`
	Error string `json:"error,omitempty"`
}

// PresetInfo is the public view of a preset.
type PresetInfo struct {
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	Groups []string       `json:"groups"`
	Values map[string]int `json:"values"`
}

// Snapshot is the full observable state.
type Snapshot struct {
	Connected     bool           `json:"connected"`
	Streaming     bool           `json:"streaming"`
	Device        *camera.Info   `json:"device,omitempty"`
	DeviceID      string         `json:"device_id"`
	Host          string         `json:"host"`
	Controls      []ControlState `json:"controls"`
	Values        map[string]int `json:"values"`
	Presets       []PresetInfo   `json:"presets"`
	DefaultPreset string         `json:"default_preset,omitempty"`
	ActivePreset  string         `json:"active_preset,omitempty"`
}

// Options tune timing (tests shorten them).
type Options struct {
	DiscoverInterval time.Duration // default 2s
	MotionTick       time.Duration // default 20ms
	// StreamSettle is how long to wait after a stream starts before applying
	// the default preset (the camera first moves to its wake pose).
	StreamSettle time.Duration // default 1.5s
	// ModeSettle is how long to wait after switching to DeskView before
	// tilting down (the firmware applies modes asynchronously).
	ModeSettle time.Duration // default 1.5s
	// InUse reports whether a video app currently uses the camera. The Link 2
	// parks its gimbal while idle, so this drives the UI hint, the HA "In use"
	// sensor and the default preset. Nil means unknown (always false).
	InUse func() (bool, error)
}

// Service is safe for concurrent use.
type Service struct {
	backend camera.Backend
	store   *config.Store
	log     *slog.Logger
	opts    Options
	host    string

	op sync.Mutex // serializes camera I/O

	mu        sync.Mutex // guards fields below
	cam       *camera.Camera
	streaming bool
	values    map[string]int
	errs      map[string]string
	active    string
	subs      map[chan struct{}]struct{}

	motion motionState
	pollCh chan struct{} // request an immediate poll
}

// New creates a service. Call Run to start it.
func New(b camera.Backend, store *config.Store, log *slog.Logger, opts Options) *Service {
	if opts.DiscoverInterval == 0 {
		opts.DiscoverInterval = 2 * time.Second
	}
	if opts.ModeSettle == 0 {
		opts.ModeSettle = 1500 * time.Millisecond
	}
	if opts.StreamSettle == 0 {
		opts.StreamSettle = 1500 * time.Millisecond
	}
	if opts.MotionTick == 0 {
		opts.MotionTick = 20 * time.Millisecond
	}
	if log == nil {
		log = slog.Default()
	}
	host, _ := os.Hostname()
	migratePresets(store, log)
	return &Service{
		backend: b, store: store, log: log, opts: opts, host: host,
		subs:   map[chan struct{}]struct{}{},
		pollCh: make(chan struct{}, 1),
	}
}

// Subscribe returns a channel that receives a signal after every state change
// (coalesced; read Snapshot for the data).
func (s *Service) Subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}
}

func (s *Service) notifyLocked() {
	for ch := range s.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (s *Service) notify() {
	s.mu.Lock()
	s.notifyLocked()
	s.mu.Unlock()
}

// Run discovers the camera and polls it until ctx is cancelled.
func (s *Service) Run(ctx context.Context) {
	go s.motionLoop(ctx)
	for {
		if s.camera() == nil {
			s.tryConnect()
		}
		wait := s.opts.DiscoverInterval
		if s.camera() != nil {
			if !s.Moving() { // a full poll would stall motion writes
				s.poll()
			}
			wait = time.Duration(s.store.Get().PollIntervalMS) * time.Millisecond
		}
		select {
		case <-ctx.Done():
			s.stopMotion() // never leave the gimbal turning
			s.disconnect("shutdown")
			return
		case <-time.After(wait):
		case <-s.pollCh:
		}
	}
}

// RequestPoll triggers an immediate poll / discovery.
func (s *Service) RequestPoll() {
	select {
	case s.pollCh <- struct{}{}:
	default:
	}
}

func (s *Service) camera() *camera.Camera {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cam
}

func (s *Service) tryConnect() {
	ds, err := s.backend.List()
	if err != nil {
		s.log.Warn("camera enumeration failed", "err", err)
		return
	}
	if len(ds) == 0 {
		return
	}
	d := ds[0]
	s.op.Lock()
	cam, err := camera.Open(s.backend, d)
	if err != nil {
		s.op.Unlock()
		s.log.Warn("camera open failed", "device", d.Name, "err", err)
		return
	}
	values, errs := cam.ReadAll()
	s.op.Unlock()
	if len(errs) == len(cam.Specs()) {
		s.log.Warn("camera opened but no control readable", "device", d.Name)
		cam.Close()
		return
	}
	info := cam.Info()
	s.log.Info("camera connected", "model", info.Model, "serial", info.Serial, "firmware", info.Firmware)
	s.mu.Lock()
	s.cam = cam
	s.values = values
	s.errs = errStrings(errs)
	s.active = ""
	s.notifyLocked()
	s.notifyLocked()
	s.mu.Unlock()
}

func errStrings(errs map[string]error) map[string]string {
	out := make(map[string]string, len(errs))
	for k, e := range errs {
		out[k] = e.Error()
	}
	return out
}

func (s *Service) disconnect(reason string) {
	s.mu.Lock()
	cam := s.cam
	s.cam = nil
	s.streaming = false
	s.values = nil
	s.errs = nil
	s.active = ""
	s.notifyLocked()
	s.mu.Unlock()
	if cam != nil {
		s.op.Lock()
		cam.Close()
		s.op.Unlock()
		s.log.Info("camera disconnected", "reason", reason)
	}
}

func (s *Service) poll() {
	cam := s.camera()
	if cam == nil {
		return
	}
	s.op.Lock()
	values, errs := cam.ReadAll()
	s.op.Unlock()
	if len(errs) == len(cam.Specs()) {
		s.disconnect(fmt.Sprintf("all reads failed: %v", firstErr(errs)))
		return
	}
	s.merge(values, errs)
	if s.opts.InUse != nil {
		if on, err := s.opts.InUse(); err == nil {
			s.setStreaming(cam, on)
		} else {
			s.log.Debug("in-use detection failed", "err", err)
		}
	}
}

// setStreaming records the stream state. When a stream starts, the default
// preset is applied once the camera has settled into its wake pose.
func (s *Service) setStreaming(cam *camera.Camera, on bool) {
	s.mu.Lock()
	if s.cam != cam || s.streaming == on {
		s.mu.Unlock()
		return
	}
	s.streaming = on
	s.notifyLocked()
	s.mu.Unlock()
	s.log.Info("stream state changed", "streaming", on)
	if !on {
		return
	}
	id := s.store.Get().DefaultPreset
	if id == "" {
		return
	}
	go func() {
		time.Sleep(s.opts.StreamSettle)
		s.mu.Lock()
		still := s.cam == cam && s.streaming
		s.mu.Unlock()
		if !still {
			return
		}
		if err := s.ApplyPreset(id); err != nil {
			s.log.Warn("default preset failed", "err", err)
		}
	}()
}

func firstErr(errs map[string]error) error {
	for _, k := range slices.Sorted(maps.Keys(errs)) {
		return errs[k]
	}
	return nil
}

// merge stores read values and notifies subscribers if anything changed.
func (s *Service) merge(values map[string]int, errs map[string]error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cam == nil {
		return
	}
	changed := false
	if s.values == nil {
		s.values = map[string]int{}
	}
	for k, v := range values {
		if old, ok := s.values[k]; !ok || old != v {
			s.values[k] = v
			changed = true
		}
	}
	if errs != nil {
		es := errStrings(errs)
		if !maps.Equal(es, s.errs) {
			s.errs = es
			changed = true
		}
	} else if len(s.errs) > 0 && len(values) == len(s.cam.Specs()) {
		s.errs = nil
		changed = true
	}
	if s.active != "" && !s.presetMatchesLocked(s.active) {
		s.active = ""
		changed = true
	}
	if changed {
		s.notifyLocked()
	}
}

// Snapshot returns the current state.
func (s *Service) Snapshot() Snapshot {
	cfg := s.store.Get()
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := Snapshot{
		Connected:     s.cam != nil,
		Streaming:     s.streaming,
		Host:          s.host,
		Values:        map[string]int{},
		Presets:       presetInfos(cfg.Presets),
		DefaultPreset: cfg.DefaultPreset,
		ActivePreset:  s.active,
	}
	specs := camera.DefaultSpecs()
	if s.cam != nil {
		info := s.cam.Info()
		snap.Device = &info
		specs = s.cam.Specs()
	}
	snap.DeviceID = deviceID(snap.Device, cfg.MQTT.DeviceName)
	for _, sp := range specs {
		cs := ControlState{ControlSpec: sp}
		if v, ok := s.values[sp.Key]; ok {
			cs.Value = &v
			snap.Values[sp.Key] = v
		}
		cs.Error = s.errs[sp.Key]
		snap.Controls = append(snap.Controls, cs)
	}
	return snap
}

var reSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slug converts a name to a topic-safe id.
func Slug(name string) string {
	return strings.Trim(reSlug.ReplaceAllString(strings.ToLower(name), "_"), "_")
}

func deviceID(info *camera.Info, deviceName string) string {
	if info != nil && info.Serial != "" {
		return Slug(info.Serial)
	}
	if id := Slug(deviceName); id != "" {
		return id
	}
	return "link"
}

// SetControls writes several controls. Values are clamped to their range.
// Controls with an auto partner are written after the auto flags so that
// e.g. {"white_balance_auto":0,"white_balance_temperature":4000} works.
func (s *Service) SetControls(values map[string]int) error {
	return s.setControls(values, true)
}

// setControls writes values; deskTilt enables the automatic tilt-down after
// switching to DeskView (presets with their own position disable it).
func (s *Service) setControls(values map[string]int, deskTilt bool) error {
	cam := s.camera()
	if cam == nil {
		return ErrNoCamera
	}
	keys := orderKeys(cam, values)
	var errs []error
	read := map[string]int{}
	s.op.Lock()
	for _, k := range keys {
		spec, ok := cam.Spec(k)
		if !ok {
			errs = append(errs, fmt.Errorf("unknown control %q", k))
			continue
		}
		got, err := cam.Set(k, spec.Clamp(values[k]))
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", k, err))
			continue
		}
		read[k] = got
		if spec.AutoKey != "" {
			if v, err := cam.Get(spec.AutoKey); err == nil {
				read[spec.AutoKey] = v
			}
		}
	}
	s.op.Unlock()
	s.merge(read, nil)
	if v, ok := read[camera.AIMode]; ok && v == camera.AIModeDeskView && deskTilt {
		// DeskView needs the gimbal pointing at the desk; the firmware does
		// not tilt down by itself.
		go func() {
			time.Sleep(s.opts.ModeSettle)
			if s.camera() != cam {
				return
			}
			if err := s.SetControls(map[string]int{camera.Tilt: camera.DeskViewTilt}); err != nil {
				s.log.Warn("DeskView tilt failed", "err", err)
			}
		}()
	}
	return errors.Join(errs...)
}

// orderKeys returns keys in spec order with auto flags before their partners.
func orderKeys(cam *camera.Camera, values map[string]int) []string {
	var autos, rest []string
	for _, sp := range cam.Specs() {
		if _, ok := values[sp.Key]; !ok {
			continue
		}
		if isAutoKey(cam, sp.Key) {
			autos = append(autos, sp.Key)
		} else {
			rest = append(rest, sp.Key)
		}
	}
	for k := range values { // unknown keys last, reported as errors
		if _, ok := cam.Spec(k); !ok {
			rest = append(rest, k)
		}
	}
	return append(autos, rest...)
}

func isAutoKey(cam *camera.Camera, key string) bool {
	for _, sp := range cam.Specs() {
		if sp.AutoKey == key {
			return true
		}
	}
	return false
}

// ResetControls restores defaults for the given keys (all when empty).
func (s *Service) ResetControls(keys []string) error {
	cam := s.camera()
	if cam == nil {
		return ErrNoCamera
	}
	want := map[string]int{}
	for _, sp := range cam.Specs() {
		if sp.ReadOnly {
			continue
		}
		if len(keys) == 0 || slices.Contains(keys, sp.Key) {
			want[sp.Key] = sp.Default
		}
	}
	// Do not force manual values for controls whose auto default is on.
	for _, sp := range cam.Specs() {
		if sp.AutoKey != "" && want[sp.AutoKey] == 1 {
			delete(want, sp.Key)
		}
	}
	return s.SetControls(want)
}

// Center moves pan/tilt to 0 and zoom to its minimum.
func (s *Service) Center() error {
	s.stopMotion()
	cam := s.camera()
	if cam == nil {
		return ErrNoCamera
	}
	z, _ := cam.Spec(camera.Zoom)
	return s.SetControls(map[string]int{camera.Pan: 0, camera.Tilt: 0, camera.Zoom: z.Min})
}

// Nudge changes position controls relatively (pan/tilt degrees, zoom percent).
func (s *Service) Nudge(delta map[string]int) error {
	cam := s.camera()
	if cam == nil {
		return ErrNoCamera
	}
	want := map[string]int{}
	s.mu.Lock()
	for k, d := range delta {
		if k != camera.Pan && k != camera.Tilt && k != camera.Zoom {
			s.mu.Unlock()
			return fmt.Errorf("nudge supports pan, tilt and zoom, not %q", k)
		}
		want[k] = s.values[k] + d
	}
	s.mu.Unlock()
	return s.SetControls(want)
}

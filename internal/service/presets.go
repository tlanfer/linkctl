package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/tlanfer/linkctl/internal/camera"
	"github.com/tlanfer/linkctl/internal/config"
)

// MaxPresets limits the number of stored presets.
const MaxPresets = 32

// ErrPresetNotFound is returned for unknown preset ids or names.
var ErrPresetNotFound = errors.New("preset not found")

var validGroups = []string{string(camera.GroupPicture), string(camera.GroupPosition), string(camera.GroupAI)}

func presetInfos(ps []config.Preset) []PresetInfo {
	out := make([]PresetInfo, 0, len(ps))
	for _, p := range ps {
		out = append(out, PresetInfo{ID: p.ID, Name: p.Name, Groups: p.Groups, Values: p.Values})
	}
	return out
}

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("preset name cannot be empty")
	}
	if len([]rune(name)) > 60 {
		name = string([]rune(name)[:60])
	}
	if strings.EqualFold(name, NoPresetOption) {
		return "", fmt.Errorf("%q is reserved", NoPresetOption)
	}
	return name, nil
}

// NoPresetOption is the placeholder used by Home Assistant's preset select.
const NoPresetOption = "None"

func cleanGroups(groups []string) ([]string, error) {
	var out []string
	for _, g := range validGroups {
		if slices.Contains(groups, g) {
			out = append(out, g)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("select at least one group (picture, position, ai)")
	}
	return out, nil
}

// capture returns the current values of all controls in groups.
func (s *Service) capture(groups []string) (map[string]int, error) {
	cam := s.camera()
	if cam == nil {
		return nil, ErrNoCamera
	}
	s.poll()
	s.mu.Lock()
	defer s.mu.Unlock()
	vals := map[string]int{}
	for _, sp := range cam.Specs() {
		if !slices.Contains(groups, string(sp.Group)) {
			continue
		}
		v, ok := s.values[sp.Key]
		if !ok {
			continue
		}
		vals[sp.Key] = v
	}
	return vals, nil
}

func findPreset(ps []config.Preset, ref string) int {
	for i, p := range ps {
		if p.ID == ref {
			return i
		}
	}
	for i, p := range ps {
		if strings.EqualFold(p.Name, ref) {
			return i
		}
	}
	return -1
}

func nameTaken(ps []config.Preset, name, exceptID string) bool {
	for _, p := range ps {
		if p.ID != exceptID && strings.EqualFold(p.Name, name) {
			return true
		}
	}
	return false
}

// SavePreset captures the current values of groups under name.
func (s *Service) SavePreset(name string, groups []string) (PresetInfo, error) {
	name, err := cleanName(name)
	if err != nil {
		return PresetInfo{}, err
	}
	groups, err = cleanGroups(groups)
	if err != nil {
		return PresetInfo{}, err
	}
	vals, err := s.capture(groups)
	if err != nil {
		return PresetInfo{}, err
	}
	p := config.Preset{ID: newID(), Name: name, Groups: groups, Values: vals}
	_, err = s.store.Update(func(c *config.Config) error {
		if len(c.Presets) >= MaxPresets {
			return fmt.Errorf("at most %d presets are supported", MaxPresets)
		}
		if nameTaken(c.Presets, name, "") {
			return fmt.Errorf("a preset named %q already exists", name)
		}
		c.Presets = append(c.Presets, p)
		return nil
	})
	if err != nil {
		return PresetInfo{}, err
	}
	s.mu.Lock()
	s.active = p.ID
	s.notifyLocked()
	s.mu.Unlock()
	return PresetInfo{ID: p.ID, Name: p.Name, Groups: p.Groups, Values: p.Values}, nil
}

// PresetUpdate changes an existing preset. Nil fields are left unchanged.
// Recapture stores the camera's current values for the (new) groups.
type PresetUpdate struct {
	Name      *string  `json:"name"`
	Groups    []string `json:"groups"`
	Recapture bool     `json:"recapture"`
}

// UpdatePreset renames, regroups or recaptures a preset.
func (s *Service) UpdatePreset(id string, u PresetUpdate) error {
	cur := s.store.Get()
	i := findPreset(cur.Presets, id)
	if i < 0 {
		return ErrPresetNotFound
	}
	p := cur.Presets[i]
	if u.Name != nil {
		n, err := cleanName(*u.Name)
		if err != nil {
			return err
		}
		p.Name = n
	}
	if u.Groups != nil {
		g, err := cleanGroups(u.Groups)
		if err != nil {
			return err
		}
		p.Groups = g
		u.Recapture = true
	}
	if u.Recapture {
		vals, err := s.capture(p.Groups)
		if err != nil {
			return err
		}
		p.Values = vals
	}
	_, err := s.store.Update(func(c *config.Config) error {
		j := findPreset(c.Presets, p.ID)
		if j < 0 {
			return ErrPresetNotFound
		}
		if nameTaken(c.Presets, p.Name, p.ID) {
			return fmt.Errorf("a preset named %q already exists", p.Name)
		}
		c.Presets[j] = p
		return nil
	})
	if err == nil {
		s.notify()
	}
	return err
}

// DeletePreset removes a preset.
func (s *Service) DeletePreset(id string) error {
	_, err := s.store.Update(func(c *config.Config) error {
		i := findPreset(c.Presets, id)
		if i < 0 {
			return ErrPresetNotFound
		}
		if c.DefaultPreset == c.Presets[i].ID {
			c.DefaultPreset = ""
		}
		c.Presets = slices.Delete(c.Presets, i, i+1)
		return nil
	})
	if err == nil {
		s.mu.Lock()
		if s.active == id {
			s.active = ""
		}
		s.notifyLocked()
		s.mu.Unlock()
	}
	return err
}

// ReorderPresets sets the preset order; ids must be a permutation.
func (s *Service) ReorderPresets(ids []string) error {
	_, err := s.store.Update(func(c *config.Config) error {
		if len(ids) != len(c.Presets) {
			return errors.New("order must list every preset exactly once")
		}
		out := make([]config.Preset, 0, len(ids))
		for _, id := range ids {
			i := findPreset(c.Presets, id)
			if i < 0 || slices.ContainsFunc(out, func(p config.Preset) bool { return p.ID == c.Presets[i].ID }) {
				return errors.New("order must list every preset exactly once")
			}
			out = append(out, c.Presets[i])
		}
		c.Presets = out
		return nil
	})
	if err == nil {
		s.notify()
	}
	return err
}

// SetDefaultPreset marks the preset applied on connect ("" clears it).
func (s *Service) SetDefaultPreset(id string) error {
	_, err := s.store.Update(func(c *config.Config) error {
		if id == "" {
			c.DefaultPreset = ""
			return nil
		}
		i := findPreset(c.Presets, id)
		if i < 0 {
			return ErrPresetNotFound
		}
		c.DefaultPreset = c.Presets[i].ID
		return nil
	})
	if err == nil {
		s.notify()
	}
	return err
}

var positionKeys = []string{camera.Pan, camera.Tilt, camera.Zoom}

// ApplyPreset applies a preset by id or (case-insensitive) name.
func (s *Service) ApplyPreset(ref string) error {
	cfg := s.store.Get()
	i := findPreset(cfg.Presets, ref)
	if i < 0 {
		return ErrPresetNotFound
	}
	p := cfg.Presets[i]
	cam := s.camera()
	if cam == nil {
		return ErrNoCamera
	}
	s.stopMotion()
	want := make(map[string]int, len(p.Values))
	for k, v := range p.Values {
		want[k] = v
	}
	// A control driven by an enabled auto flag keeps running automatically.
	for _, sp := range cam.Specs() {
		if sp.AutoKey != "" && want[sp.AutoKey] == 1 {
			delete(want, sp.Key)
		}
	}
	// AI settings go first: a mode switch moves the gimbal by itself, so
	// position is applied only after the firmware has settled. Tracking moves
	// the camera on its own, so a tracking preset skips position.
	ai := map[string]int{}
	for _, sp := range cam.Specs() {
		if v, ok := want[sp.Key]; ok && sp.Group == camera.GroupAI {
			ai[sp.Key] = v
			delete(want, sp.Key)
		}
	}
	mode, hasMode := ai[camera.AIMode]
	if hasMode && mode == camera.AIModeTracking {
		for _, k := range positionKeys {
			delete(want, k)
		}
	}
	hasPos := false
	for _, k := range positionKeys {
		if _, ok := want[k]; ok {
			hasPos = true
		}
	}
	var errs []error
	if len(ai) > 0 {
		s.mu.Lock()
		modeChanged := hasMode && s.values[camera.AIMode] != mode
		s.mu.Unlock()
		errs = append(errs, s.setControls(ai, !hasPos))
		if modeChanged && hasPos {
			time.Sleep(s.opts.ModeSettle)
		}
	}
	if len(want) > 0 {
		errs = append(errs, s.setControls(want, false))
	}
	err := errors.Join(errs...)
	s.mu.Lock()
	s.active = p.ID
	if err != nil {
		s.active = ""
	}
	s.notifyLocked()
	s.mu.Unlock()
	return err
}

// presetMatchesLocked reports whether the camera still matches the preset.
func (s *Service) presetMatchesLocked(id string) bool {
	cfg := s.store.Get()
	i := findPreset(cfg.Presets, id)
	if i < 0 || s.cam == nil {
		return false
	}
	p := cfg.Presets[i]
	for _, sp := range s.cam.Specs() {
		want, ok := p.Values[sp.Key]
		if !ok {
			continue
		}
		if sp.AutoKey != "" && p.Values[sp.AutoKey] == 1 {
			continue
		}
		if mode, ok := p.Values[camera.AIMode]; ok && mode == camera.AIModeTracking && sp.Group == camera.GroupPosition {
			continue // tracking moves the camera; position is not part of the match
		}
		got := s.values[sp.Key]
		tol := 0
		if sp.Key == camera.Pan || sp.Key == camera.Tilt {
			tol = 1 // the gimbal may settle one degree off
		}
		if got < want-tol || got > want+tol {
			return false
		}
	}
	return true
}

// migratePresets rewrites preset values stored by older versions (see
// camera.MigrateValues) once, at startup.
func migratePresets(store *config.Store, log *slog.Logger) {
	changed := false
	for _, p := range store.Get().Presets {
		v := maps.Clone(p.Values)
		if camera.MigrateValues(v) {
			changed = true
		}
	}
	if !changed {
		return
	}
	_, err := store.Update(func(c *config.Config) error {
		for i := range c.Presets {
			camera.MigrateValues(c.Presets[i].Values)
		}
		return nil
	})
	if err != nil {
		log.Warn("preset migration failed", "err", err)
		return
	}
	log.Info("migrated presets to the merged composition setting")
}

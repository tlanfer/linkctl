// Package config persists linkctl settings and presets as JSON in the user's
// config directory.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// TLSMode selects MQTT transport security.
type TLSMode string

const (
	TLSOff      TLSMode = "off"
	TLSOn       TLSMode = "on"
	TLSInsecure TLSMode = "insecure" // TLS without certificate verification
)

// MQTT holds broker settings. Password is only kept here when the OS keychain
// is unavailable (PasswordInFile reports that).
type MQTT struct {
	Enabled         bool    `json:"enabled"`
	Host            string  `json:"host"`
	Port            int     `json:"port"`
	TLS             TLSMode `json:"tls"`
	Username        string  `json:"username"`
	Password        string  `json:"password,omitempty"`
	DeviceName      string  `json:"device_name"`
	BaseTopic       string  `json:"base_topic"`
	DiscoveryPrefix string  `json:"discovery_prefix"`
}

// Preset is a named snapshot of control values.
type Preset struct {
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	Groups []string       `json:"groups"`
	Values map[string]int `json:"values"`
}

// Config is the persisted state.
type Config struct {
	Version        int      `json:"version"`
	HTTPPort       int      `json:"http_port"`
	PollIntervalMS int      `json:"poll_interval_ms"`
	MQTT           MQTT     `json:"mqtt"`
	Presets        []Preset `json:"presets"`
	DefaultPreset  string   `json:"default_preset,omitempty"` // preset id
}

// Defaults returns a fresh configuration.
func Defaults() Config {
	return Config{
		Version:        1,
		HTTPPort:       7360,
		PollIntervalMS: 1000,
		MQTT: MQTT{
			Port:            1883,
			TLS:             TLSOff,
			DeviceName:      "Insta360 Link 2",
			BaseTopic:       "insta360link",
			DiscoveryPrefix: "homeassistant",
		},
		Presets: []Preset{},
	}
}

// Normalize fills missing values with defaults.
func (c *Config) Normalize() {
	d := Defaults()
	if c.Version == 0 {
		c.Version = d.Version
	}
	if c.HTTPPort <= 0 || c.HTTPPort > 65535 {
		c.HTTPPort = d.HTTPPort
	}
	if c.PollIntervalMS < 200 {
		c.PollIntervalMS = d.PollIntervalMS
	}
	if c.MQTT.Port <= 0 || c.MQTT.Port > 65535 {
		c.MQTT.Port = d.MQTT.Port
	}
	switch c.MQTT.TLS {
	case TLSOff, TLSOn, TLSInsecure:
	default:
		c.MQTT.TLS = TLSOff
	}
	if c.MQTT.DeviceName == "" {
		c.MQTT.DeviceName = d.MQTT.DeviceName
	}
	if c.MQTT.BaseTopic == "" {
		c.MQTT.BaseTopic = d.MQTT.BaseTopic
	}
	if c.MQTT.DiscoveryPrefix == "" {
		c.MQTT.DiscoveryPrefix = d.MQTT.DiscoveryPrefix
	}
	if c.Presets == nil {
		c.Presets = []Preset{}
	}
}

// Clone returns a deep copy.
func (c Config) Clone() Config {
	out := c
	out.Presets = make([]Preset, len(c.Presets))
	for i, p := range c.Presets {
		p.Groups = append([]string(nil), p.Groups...)
		vals := make(map[string]int, len(p.Values))
		for k, v := range p.Values {
			vals[k] = v
		}
		p.Values = vals
		out.Presets[i] = p
	}
	return out
}

// Store loads and saves the config file. It is safe for concurrent use.
type Store struct {
	mu   sync.Mutex
	path string
	cfg  Config
}

// DefaultPath is <UserConfigDir>/linkctl/config.json.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "linkctl", "config.json"), nil
}

// Open loads path, creating defaults if it does not exist.
func Open(path string) (*Store, error) {
	s := &Store{path: path, cfg: Defaults()}
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		var c Config
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		s.cfg = c
	}
	s.cfg.Normalize()
	return s, nil
}

// Path returns the file location.
func (s *Store) Path() string { return s.path }

// Get returns a copy of the current config.
func (s *Store) Get() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.Clone()
}

// Update applies fn to a copy and persists the result atomically. If fn
// returns an error nothing is saved.
func (s *Store) Update(fn func(*Config) error) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.cfg.Clone()
	if err := fn(&next); err != nil {
		return s.cfg.Clone(), err
	}
	next.Normalize()
	if err := s.write(next); err != nil {
		return s.cfg.Clone(), err
	}
	s.cfg = next
	return next.Clone(), nil
}

func (s *Store) write(c Config) error {
	if s.path == "" {
		return nil // in-memory store (tests)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), "config-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		return err
	}
	_ = tmp.Chmod(0o600) // best-effort; not supported on Windows
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}

// NewMemory returns a store that never touches disk (for tests).
func NewMemory(c Config) *Store {
	c.Normalize()
	return &Store{cfg: c}
}

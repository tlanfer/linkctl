package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreRoundTripAndNormalize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if c := s.Get(); c.HTTPPort != 7360 || c.MQTT.BaseTopic != "insta360link" {
		t.Fatalf("defaults = %+v", c)
	}
	_, err = s.Update(func(c *Config) error {
		c.MQTT.Host = "broker"
		c.PollIntervalMS = 5 // invalid, normalized
		c.Presets = append(c.Presets, Preset{ID: "a", Name: "Desk", Groups: []string{"position"}, Values: map[string]int{"pan": 3}})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	c := s2.Get()
	if c.MQTT.Host != "broker" || c.PollIntervalMS != 1000 || c.Presets[0].Values["pan"] != 3 {
		t.Fatalf("reloaded = %+v", c)
	}
	// Get returns a deep copy.
	c.Presets[0].Values["pan"] = 99
	if s2.Get().Presets[0].Values["pan"] != 3 {
		t.Fatal("Get leaked internal state")
	}
}

func TestOpenRejectsCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	_ = os.WriteFile(path, []byte("{nope"), 0o600)
	if _, err := Open(path); err == nil {
		t.Fatal("corrupt config accepted")
	}
}

func TestPasswordFallback(t *testing.T) {
	s := NewMemory(Defaults())
	_, _ = s.Update(func(c *Config) error { c.MQTT.Username = "u"; return nil })
	sec := &MemorySecrets{}
	inFile, err := StorePassword(s, sec, "u", "pw")
	if err != nil || inFile || LoadPassword(s.Get(), sec) != "pw" || s.Get().MQTT.Password != "" {
		t.Fatalf("keychain path: inFile=%v err=%v", inFile, err)
	}
	sec.Fail = true
	inFile, _ = StorePassword(s, sec, "u", "pw2")
	if !inFile || LoadPassword(s.Get(), sec) != "pw2" {
		t.Fatal("fallback not used")
	}
}

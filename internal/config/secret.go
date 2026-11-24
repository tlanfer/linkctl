package config

import (
	"errors"

	"github.com/zalando/go-keyring"
)

const keyringService = "linkctl"

// Secrets stores the MQTT password in the OS keychain, falling back to the
// config file when no keychain is available.
type Secrets interface {
	Get(user string) (string, error)
	Set(user, password string) error
	Delete(user string) error
}

// Keyring uses the OS keychain via go-keyring.
type Keyring struct{}

func (Keyring) Get(user string) (string, error) {
	v, err := keyring.Get(keyringService, "mqtt:"+user)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", nil
	}
	return v, err
}

func (Keyring) Set(user, password string) error {
	return keyring.Set(keyringService, "mqtt:"+user, password)
}

func (Keyring) Delete(user string) error {
	err := keyring.Delete(keyringService, "mqtt:"+user)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

// MemorySecrets is an in-memory Secrets for tests. Fail forces errors to
// exercise the config-file fallback.
type MemorySecrets struct {
	Fail bool
	m    map[string]string
}

var errNoKeychain = errors.New("keychain unavailable")

func (s *MemorySecrets) Get(user string) (string, error) {
	if s.Fail {
		return "", errNoKeychain
	}
	return s.m[user], nil
}

func (s *MemorySecrets) Set(user, password string) error {
	if s.Fail {
		return errNoKeychain
	}
	if s.m == nil {
		s.m = map[string]string{}
	}
	s.m[user] = password
	return nil
}

func (s *MemorySecrets) Delete(user string) error {
	if s.Fail {
		return errNoKeychain
	}
	delete(s.m, user)
	return nil
}

// StorePassword saves the password in secrets, falling back to the config
// file. It reports whether the fallback was used.
func StorePassword(store *Store, sec Secrets, user, password string) (inFile bool, err error) {
	if serr := sec.Set(user, password); serr == nil {
		_, err = store.Update(func(c *Config) error { c.MQTT.Password = ""; return nil })
		return false, err
	}
	_, err = store.Update(func(c *Config) error { c.MQTT.Password = password; return nil })
	return true, err
}

// LoadPassword returns the MQTT password from the keychain or the config file.
func LoadPassword(c Config, sec Secrets) string {
	if c.MQTT.Password != "" {
		return c.MQTT.Password
	}
	if c.MQTT.Username == "" {
		return ""
	}
	p, _ := sec.Get(c.MQTT.Username)
	return p
}

package web

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/tlanfer/linkctl/internal/config"
	"github.com/tlanfer/linkctl/internal/service"
)

func (s *Server) patchControls(w http.ResponseWriter, r *http.Request) {
	var body map[string]int
	if err := decode(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	s.respond(w, s.Svc.SetControls(body))
}

func (s *Server) resetControls(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Keys []string `json:"keys"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	s.respond(w, s.Svc.ResetControls(body.Keys))
}

func (s *Server) center(w http.ResponseWriter, r *http.Request) { s.respond(w, s.Svc.Center()) }

func (s *Server) nudge(w http.ResponseWriter, r *http.Request) {
	var body map[string]int
	if err := decode(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	s.respond(w, s.Svc.Nudge(body))
}

func (s *Server) move(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Pan   *float64 `json:"pan"`
		Tilt  *float64 `json:"tilt"`
		Zoom  *float64 `json:"zoom"`
		TTLMS int      `json:"ttl_ms"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	vel := map[string]float64{}
	for k, v := range map[string]*float64{"pan": body.Pan, "tilt": body.Tilt, "zoom": body.Zoom} {
		if v != nil {
			vel[k] = *v
		}
	}
	err := s.Svc.Move(vel, time.Duration(body.TTLMS)*time.Millisecond)
	if err != nil {
		writeErr(w, err)
		return
	}
	// Motion commands are sent every ~200ms; skip the full state body.
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) createPreset(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string   `json:"name"`
		Groups []string `json:"groups"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	_, err := s.Svc.SavePreset(body.Name, body.Groups)
	s.respond(w, err)
}

func (s *Server) updatePreset(w http.ResponseWriter, r *http.Request) {
	var body service.PresetUpdate
	if err := decode(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	s.respond(w, s.Svc.UpdatePreset(r.PathValue("id"), body))
}

func (s *Server) deletePreset(w http.ResponseWriter, r *http.Request) {
	s.respond(w, s.Svc.DeletePreset(r.PathValue("id")))
}

func (s *Server) applyPreset(w http.ResponseWriter, r *http.Request) {
	s.respond(w, s.Svc.ApplyPreset(r.PathValue("id")))
}

func (s *Server) orderPresets(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	s.respond(w, s.Svc.ReorderPresets(body.IDs))
}

func (s *Server) defaultPreset(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	s.respond(w, s.Svc.SetDefaultPreset(body.ID))
}

// mqttSettings is the settings view of the MQTT config. Password is
// write-only: it is never returned.
type mqttSettings struct {
	Enabled         bool           `json:"enabled"`
	Host            string         `json:"host"`
	Port            int            `json:"port"`
	TLS             config.TLSMode `json:"tls"`
	Username        string         `json:"username"`
	Password        *string        `json:"password,omitempty"`
	PasswordSet     bool           `json:"password_set"`
	PasswordInFile  bool           `json:"password_in_file"`
	DeviceName      string         `json:"device_name"`
	BaseTopic       string         `json:"base_topic"`
	DiscoveryPrefix string         `json:"discovery_prefix"`
}

type settings struct {
	HTTPPort       int          `json:"http_port"`
	PollIntervalMS int          `json:"poll_interval_ms"`
	StartOnLogin   bool         `json:"start_on_login"`
	ConfigPath     string       `json:"config_path"`
	MQTT           mqttSettings `json:"mqtt"`
}

func (s *Server) settings() settings {
	c := s.Store.Get()
	out := settings{
		HTTPPort:       c.HTTPPort,
		PollIntervalMS: c.PollIntervalMS,
		ConfigPath:     s.Store.Path(),
		MQTT: mqttSettings{
			Enabled: c.MQTT.Enabled, Host: c.MQTT.Host, Port: c.MQTT.Port, TLS: c.MQTT.TLS,
			Username: c.MQTT.Username, DeviceName: c.MQTT.DeviceName,
			BaseTopic: c.MQTT.BaseTopic, DiscoveryPrefix: c.MQTT.DiscoveryPrefix,
			PasswordInFile: c.MQTT.Password != "",
			PasswordSet:    config.LoadPassword(c, s.Secrets) != "",
		},
	}
	if s.Autostart != nil {
		out.StartOnLogin = s.Autostart.Enabled()
	}
	return out
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.settings())
}

// settingsUpdate uses pointers so omitted fields stay unchanged.
type settingsUpdate struct {
	HTTPPort       *int  `json:"http_port"`
	PollIntervalMS *int  `json:"poll_interval_ms"`
	StartOnLogin   *bool `json:"start_on_login"`
	MQTT           *struct {
		Enabled         *bool           `json:"enabled"`
		Host            *string         `json:"host"`
		Port            *int            `json:"port"`
		TLS             *config.TLSMode `json:"tls"`
		Username        *string         `json:"username"`
		Password        *string         `json:"password"`
		DeviceName      *string         `json:"device_name"`
		BaseTopic       *string         `json:"base_topic"`
		DiscoveryPrefix *string         `json:"discovery_prefix"`
		// Read-only fields the UI may echo back; ignored.
		PasswordSet    *bool `json:"password_set"`
		PasswordInFile *bool `json:"password_in_file"`
	} `json:"mqtt"`
	ConfigPath *string `json:"config_path"` // read-only, ignored
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var u settingsUpdate
	if err := decode(r, &u); err != nil {
		writeErr(w, err)
		return
	}
	if u.PollIntervalMS != nil && (*u.PollIntervalMS < 200 || *u.PollIntervalMS > 60000) {
		writeErr(w, errors.New("poll interval must be between 200 and 60000 ms"))
		return
	}
	if u.HTTPPort != nil && (*u.HTTPPort < 1024 || *u.HTTPPort > 65535) {
		writeErr(w, errors.New("port must be between 1024 and 65535"))
		return
	}
	if u.MQTT != nil && u.MQTT.Port != nil && (*u.MQTT.Port < 1 || *u.MQTT.Port > 65535) {
		writeErr(w, errors.New("broker port must be between 1 and 65535"))
		return
	}
	if u.MQTT != nil && u.MQTT.TLS != nil {
		switch *u.MQTT.TLS {
		case config.TLSOff, config.TLSOn, config.TLSInsecure:
		default:
			writeErr(w, errors.New(`tls must be "off", "on" or "insecure"`))
			return
		}
	}
	oldUser := s.Store.Get().MQTT.Username
	_, err := s.Store.Update(func(c *config.Config) error {
		set := func(dst *int, v *int) {
			if v != nil {
				*dst = *v
			}
		}
		set(&c.HTTPPort, u.HTTPPort)
		set(&c.PollIntervalMS, u.PollIntervalMS)
		if m := u.MQTT; m != nil {
			if m.Enabled != nil {
				c.MQTT.Enabled = *m.Enabled
			}
			setS := func(dst *string, v *string) {
				if v != nil {
					*dst = *v
				}
			}
			setS(&c.MQTT.Host, m.Host)
			set(&c.MQTT.Port, m.Port)
			if m.TLS != nil {
				c.MQTT.TLS = *m.TLS
			}
			setS(&c.MQTT.Username, m.Username)
			setS(&c.MQTT.DeviceName, m.DeviceName)
			setS(&c.MQTT.BaseTopic, m.BaseTopic)
			setS(&c.MQTT.DiscoveryPrefix, m.DiscoveryPrefix)
		}
		return nil
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	if m := u.MQTT; m != nil {
		cur := s.Store.Get()
		newUser := cur.MQTT.Username
		pw := m.Password
		if pw == nil && newUser != oldUser {
			// Username changed: move the stored password along.
			p := config.LoadPassword(config.Config{MQTT: config.MQTT{Username: oldUser, Password: cur.MQTT.Password}}, s.Secrets)
			pw = &p
		}
		if pw != nil {
			_ = s.Secrets.Delete(oldUser)
			if *pw == "" {
				_, err = s.Store.Update(func(c *config.Config) error { c.MQTT.Password = ""; return nil })
			} else {
				_, err = config.StorePassword(s.Store, s.Secrets, newUser, *pw)
			}
			if err != nil {
				writeErr(w, err)
				return
			}
		}
	}
	if u.StartOnLogin != nil && s.Autostart != nil {
		if err := s.Autostart.SetEnabled(*u.StartOnLogin); err != nil {
			writeErr(w, err)
			return
		}
	}
	if s.MQTT != nil {
		s.MQTT.Reload()
	}
	s.Svc.RequestPoll()
	writeJSON(w, http.StatusOK, s.settings())
}

func (s *Server) testMQTT(w http.ResponseWriter, r *http.Request) {
	var body mqttSettings
	if err := decode(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	c := s.Store.Get()
	pw := config.LoadPassword(c, s.Secrets)
	if body.Password != nil {
		pw = *body.Password
	}
	if body.Port == 0 {
		body.Port = 1883
	}
	if body.TLS == "" {
		body.TLS = config.TLSOff
	}
	cfg := config.MQTT{Host: body.Host, Port: body.Port, TLS: body.TLS, Username: body.Username}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	type result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error,omitempty"`
	}
	if err := s.TestMQTT(ctx, cfg, pw); err != nil {
		writeJSON(w, http.StatusOK, result{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result{OK: true})
}

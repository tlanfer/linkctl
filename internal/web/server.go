// Package web serves the localhost control panel and its JSON API.
package web

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tlanfer/linkctl/internal/config"
	"github.com/tlanfer/linkctl/internal/mqtt"
	"github.com/tlanfer/linkctl/internal/service"
)

//go:embed ui
var uiFS embed.FS

// MQTTControl is the part of the MQTT bridge the web UI needs.
type MQTTControl interface {
	Status() mqtt.Status
	Subscribe() (<-chan struct{}, func())
	Reload()
}

// Autostart toggles start-on-login.
type Autostart interface {
	Enabled() bool
	SetEnabled(bool) error
}

// Server is the HTTP front end.
type Server struct {
	Svc       *service.Service
	Store     *config.Store
	Secrets   config.Secrets
	MQTT      MQTTControl
	Autostart Autostart
	Log       *slog.Logger
	// TestMQTT checks broker settings (defaults to mqtt.Test).
	TestMQTT func(ctx context.Context, c config.MQTT, password string) error

	port int
}

// Listen binds 127.0.0.1:port. Binding fails if another instance runs.
func Listen(port int) (net.Listener, error) {
	return net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
}

// Serve runs the server on l until ctx is cancelled.
func (s *Server) Serve(ctx context.Context, l net.Listener) error {
	s.port = l.Addr().(*net.TCPAddr).Port
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	err := srv.Serve(l)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Handler returns the guarded HTTP handler. The port used for Host/Origin
// checks is taken from Serve, or from SetPort in tests.
func (s *Server) Handler() http.Handler {
	if s.Log == nil {
		s.Log = slog.Default()
	}
	if s.TestMQTT == nil {
		s.TestMQTT = mqtt.Test
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/state", s.getState)
	mux.HandleFunc("GET /api/v1/events", s.events)
	mux.HandleFunc("PATCH /api/v1/controls", s.patchControls)
	mux.HandleFunc("POST /api/v1/controls/reset", s.resetControls)
	mux.HandleFunc("POST /api/v1/ptz/center", s.center)
	mux.HandleFunc("POST /api/v1/ptz/nudge", s.nudge)
	mux.HandleFunc("POST /api/v1/ptz/move", s.move)
	mux.HandleFunc("POST /api/v1/presets", s.createPreset)
	mux.HandleFunc("PUT /api/v1/presets/order", s.orderPresets)
	mux.HandleFunc("PUT /api/v1/presets/default", s.defaultPreset)
	mux.HandleFunc("PUT /api/v1/presets/{id}", s.updatePreset)
	mux.HandleFunc("DELETE /api/v1/presets/{id}", s.deletePreset)
	mux.HandleFunc("POST /api/v1/presets/{id}/apply", s.applyPreset)
	mux.HandleFunc("GET /api/v1/settings", s.getSettings)
	mux.HandleFunc("PUT /api/v1/settings", s.putSettings)
	mux.HandleFunc("POST /api/v1/mqtt/test", s.testMQTT)

	static, _ := fs.Sub(uiFS, "ui")
	files := http.FileServerFS(static)
	mux.Handle("GET /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	}))
	return s.guard(mux)
}

// SetPort sets the port used for Host/Origin validation (tests).
func (s *Server) SetPort(p int) { s.port = p }

// guard rejects requests that did not come from our own origin. Binding to
// loopback is not enough: a malicious web page can make the browser send
// requests to 127.0.0.1 (CSRF) or resolve its own hostname to it (DNS
// rebinding). Checking Host defeats rebinding, checking Origin defeats CSRF.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.allowedHost(r.Host) {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && !s.allowedOrigin(o) {
			http.Error(w, "forbidden origin", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.ContentLength != 0 {
			if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				http.Error(w, "content type must be application/json", http.StatusUnsupportedMediaType)
				return
			}
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) allowedHost(host string) bool {
	p := strconv.Itoa(s.port)
	switch strings.ToLower(host) {
	case "127.0.0.1:" + p, "localhost:" + p, "[::1]:" + p:
		return true
	}
	return false
}

func (s *Server) allowedOrigin(origin string) bool {
	p := strconv.Itoa(s.port)
	switch strings.ToLower(origin) {
	case "http://127.0.0.1:" + p, "http://localhost:" + p, "http://[::1]:" + p:
		return true
	}
	return false
}

// StateResponse is the payload of /api/v1/state and SSE "state" events.
type StateResponse struct {
	service.Snapshot
	MQTT   mqtt.Status `json:"mqtt"`
	Moving bool        `json:"moving"`
}

func (s *Server) state() StateResponse {
	r := StateResponse{Snapshot: s.Svc.Snapshot(), Moving: s.Svc.Moving()}
	if s.MQTT != nil {
		r.MQTT = s.MQTT.Status()
	} else {
		r.MQTT = mqtt.Status{State: mqtt.StateDisabled}
	}
	return r
}

func (s *Server) getState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.state())
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	svcCh, cancel := s.Svc.Subscribe()
	defer cancel()
	var mqttCh <-chan struct{}
	if s.MQTT != nil {
		ch, c2 := s.MQTT.Subscribe()
		defer c2()
		mqttCh = ch
	}
	send := func() bool {
		raw, _ := json.Marshal(s.state())
		if _, err := fmt.Fprintf(w, "event: state\ndata: %s\n\n", raw); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if !send() {
		return
	}
	keepalive := time.NewTicker(25 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-svcCh:
		case <-mqttCh:
		case <-keepalive.C:
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
			continue
		}
		// Coalesce bursts (e.g. joystick motion) to at most ~20 updates/s.
		time.Sleep(50 * time.Millisecond)
		if !send() {
			return
		}
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

type apiError struct {
	Error string `json:"error"`
}

func writeErr(w http.ResponseWriter, err error) {
	code := http.StatusBadRequest
	switch {
	case errors.Is(err, service.ErrNoCamera):
		code = http.StatusServiceUnavailable
	case errors.Is(err, service.ErrPresetNotFound):
		code = http.StatusNotFound
	}
	writeJSON(w, code, apiError{Error: err.Error()})
}

func decode(r *http.Request, v any) error {
	if r.ContentLength == 0 {
		return nil
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}

// respond writes the new state or the error.
func (s *Server) respond(w http.ResponseWriter, err error) {
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.state())
}

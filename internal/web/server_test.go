package web

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tlanfer/linkctl/internal/camera"
	"github.com/tlanfer/linkctl/internal/config"
	"github.com/tlanfer/linkctl/internal/service"
)

type fakeAutostart struct{ on bool }

func (f *fakeAutostart) Enabled() bool           { return f.on }
func (f *fakeAutostart) SetEnabled(b bool) error { f.on = b; return nil }

type env struct {
	ts      *httptest.Server
	base    string
	svc     *service.Service
	store   *config.Store
	secrets *config.MemorySecrets
	auto    *fakeAutostart
}

func newEnv(t *testing.T) *env {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := config.NewMemory(config.Defaults())
	svc := service.New(camera.NewFakeBackend(), store, log, service.Options{DiscoverInterval: 10 * time.Millisecond, MotionTick: 10 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go svc.Run(ctx)
	for i := 0; i < 300 && !svc.Snapshot().Connected; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	e := &env{svc: svc, store: store, secrets: &config.MemorySecrets{}, auto: &fakeAutostart{}}
	srv := &Server{Svc: svc, Store: store, Secrets: e.secrets, Autostart: e.auto, Log: log,
		TestMQTT: func(context.Context, config.MQTT, string) error { return errors.New("refused") }}
	e.ts = httptest.NewServer(srv.Handler())
	t.Cleanup(e.ts.Close)
	u, _ := url.Parse(e.ts.URL)
	port, _ := strconv.Atoi(u.Port())
	srv.SetPort(port)
	e.base = e.ts.URL
	return e
}

func (e *env) do(t *testing.T, method, path, body string, hdr map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, _ := http.NewRequest(method, e.base+path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		if k == "Host" {
			req.Host = v
		} else {
			req.Header.Set(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp, raw
}

func TestGuard(t *testing.T) {
	e := newEnv(t)
	port := strings.TrimPrefix(e.base, "http://127.0.0.1:")
	cases := []struct {
		name   string
		method string
		hdr    map[string]string
		body   string
		ctype  bool
		want   int
	}{
		{"plain get", "GET", nil, "", false, 200},
		{"localhost host", "GET", map[string]string{"Host": "localhost:" + port}, "", false, 200},
		{"rebinding host", "GET", map[string]string{"Host": "evil.example:" + port}, "", false, 403},
		{"wrong port host", "GET", map[string]string{"Host": "127.0.0.1:1"}, "", false, 403},
		{"foreign origin", "PATCH", map[string]string{"Origin": "https://evil.example"}, `{"brightness":10}`, true, 403},
		{"same origin", "PATCH", map[string]string{"Origin": "http://127.0.0.1:" + port}, `{"brightness":10}`, true, 200},
		{"form post", "PATCH", map[string]string{"Content-Type": "text/plain"}, `{"brightness":10}`, false, 415},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := "/api/v1/state"
			if c.method != "GET" {
				path = "/api/v1/controls"
			}
			req, _ := http.NewRequest(c.method, e.base+path, strings.NewReader(c.body))
			if c.ctype {
				req.Header.Set("Content-Type", "application/json")
			}
			for k, v := range c.hdr {
				if k == "Host" {
					req.Host = v
				} else {
					req.Header.Set(k, v)
				}
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != c.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, c.want)
			}
		})
	}
}

func TestControlsAndPTZ(t *testing.T) {
	e := newEnv(t)
	resp, raw := e.do(t, "PATCH", "/api/v1/controls", `{"brightness": 80, "zoom": 150}`, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, raw)
	}
	var st StateResponse
	_ = json.Unmarshal(raw, &st)
	if st.Values["brightness"] != 80 || st.Values["zoom"] != 150 || st.MQTT.State != "disabled" {
		t.Fatalf("state = %+v", st.Values)
	}
	resp, raw = e.do(t, "PATCH", "/api/v1/controls", `{"bogus": 1}`, nil)
	if resp.StatusCode != 400 || !strings.Contains(string(raw), "bogus") {
		t.Fatalf("%d %s", resp.StatusCode, raw)
	}
	e.do(t, "POST", "/api/v1/ptz/nudge", `{"pan": 15}`, nil)
	if e.svc.Snapshot().Values["pan"] != 15 {
		t.Fatal("nudge not applied")
	}
	resp, _ = e.do(t, "POST", "/api/v1/ptz/move", `{"tilt": 1, "ttl_ms": 100}`, nil)
	if resp.StatusCode != 204 {
		t.Fatalf("move status %d", resp.StatusCode)
	}
	e.do(t, "POST", "/api/v1/ptz/center", "", nil)
	e.do(t, "POST", "/api/v1/controls/reset", `{"keys":["brightness"]}`, nil)
	if v := e.svc.Snapshot().Values; v["brightness"] != 50 {
		t.Fatalf("reset: %v", v)
	}
}

func TestPresetsAPI(t *testing.T) {
	e := newEnv(t)
	resp, raw := e.do(t, "POST", "/api/v1/presets", `{"name":"Desk","groups":["position"]}`, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, raw)
	}
	var st StateResponse
	_ = json.Unmarshal(raw, &st)
	id := st.Presets[0].ID
	if resp, raw := e.do(t, "PUT", "/api/v1/presets/"+id, `{"name":"Desk 2"}`, nil); resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, raw)
	}
	if resp, _ := e.do(t, "POST", "/api/v1/presets/"+id+"/apply", "", nil); resp.StatusCode != 200 {
		t.Fatal("apply failed")
	}
	if resp, _ := e.do(t, "PUT", "/api/v1/presets/default", `{"id":"`+id+`"}`, nil); resp.StatusCode != 200 {
		t.Fatal("default failed")
	}
	if resp, _ := e.do(t, "POST", "/api/v1/presets/nope/apply", "", nil); resp.StatusCode != 404 {
		t.Fatalf("unknown preset: %d", resp.StatusCode)
	}
	if resp, _ := e.do(t, "DELETE", "/api/v1/presets/"+id, "", nil); resp.StatusCode != 200 {
		t.Fatal("delete failed")
	}
	if n := len(e.store.Get().Presets); n != 0 {
		t.Fatalf("presets left: %d", n)
	}
}

func TestSettingsPasswordIsWriteOnly(t *testing.T) {
	e := newEnv(t)
	resp, raw := e.do(t, "PUT", "/api/v1/settings",
		`{"start_on_login": true, "mqtt": {"enabled": true, "host": "broker.lan", "username": "cam", "password": "s3cret"}}`, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, raw)
	}
	if strings.Contains(string(raw), "s3cret") {
		t.Fatal("password echoed back")
	}
	var s settings
	_ = json.Unmarshal(raw, &s)
	if !s.MQTT.PasswordSet || s.MQTT.PasswordInFile || !s.StartOnLogin || s.MQTT.Host != "broker.lan" {
		t.Fatalf("settings = %+v", s)
	}
	if p, _ := e.secrets.Get("cam"); p != "s3cret" {
		t.Fatalf("keychain = %q", p)
	}
	if e.store.Get().MQTT.Password != "" {
		t.Fatal("password written to config although keychain works")
	}

	// Keychain failure falls back to the config file and is reported.
	e.secrets.Fail = true
	_, raw = e.do(t, "PUT", "/api/v1/settings", `{"mqtt": {"password": "other"}}`, nil)
	_ = json.Unmarshal(raw, &s)
	if !s.MQTT.PasswordInFile {
		t.Fatalf("fallback not reported: %s", raw)
	}

	resp, _ = e.do(t, "PUT", "/api/v1/settings", `{"poll_interval_ms": 5}`, nil)
	if resp.StatusCode != 400 {
		t.Fatalf("bad interval accepted: %d", resp.StatusCode)
	}
	resp, _ = e.do(t, "PUT", "/api/v1/settings", `{"mqtt": {"tls": "maybe"}}`, nil)
	if resp.StatusCode != 400 {
		t.Fatalf("bad tls accepted: %d", resp.StatusCode)
	}
	_, raw = e.do(t, "POST", "/api/v1/mqtt/test", `{"host":"x"}`, nil)
	if !strings.Contains(string(raw), "refused") {
		t.Fatalf("test result = %s", raw)
	}
}

func TestSSE(t *testing.T) {
	e := newEnv(t)
	resp, err := http.Get(e.base + "/api/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	rd := bufio.NewReader(resp.Body)
	readEvent := func() StateResponse {
		for {
			line, err := rd.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(line, "data: ") {
				var st StateResponse
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &st); err != nil {
					t.Fatal(err)
				}
				return st
			}
		}
	}
	if st := readEvent(); !st.Connected {
		t.Fatal("initial event not connected")
	}
	go func() { _ = e.svc.SetControls(map[string]int{"saturation": 12}) }()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if readEvent().Values["saturation"] == 12 {
			return
		}
	}
	t.Fatal("no SSE update")
}

func TestStaticUI(t *testing.T) {
	e := newEnv(t)
	resp, raw := e.do(t, "GET", "/", "", nil)
	if resp.StatusCode != 200 || !strings.Contains(string(raw), "<html") {
		t.Fatalf("%d", resp.StatusCode)
	}
}

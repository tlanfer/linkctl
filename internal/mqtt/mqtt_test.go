package mqtt

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	mochi "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/mochi-mqtt/server/v2/packets"

	"github.com/tlanfer/linkctl/internal/camera"
	"github.com/tlanfer/linkctl/internal/config"
	"github.com/tlanfer/linkctl/internal/service"
)

func TestParseCommand(t *testing.T) {
	specs := camera.DefaultSpecs()
	cmd, err := ParseCommand([]byte(`{"brightness": 60.4, "hdr": "ON", "mirror": false, "anti_flicker": "50 Hz",
		"preset": "Desk", "action": "center", "nudge": {"pan": -5}, "move": {"tilt": 1}, "ttl_ms": 300}`), specs)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"brightness": 60, "hdr": 1, "mirror": 0, "anti_flicker": 1}
	for k, v := range want {
		if cmd.Values[k] != v {
			t.Errorf("%s = %d, want %d", k, cmd.Values[k], v)
		}
	}
	if cmd.Preset != "Desk" || cmd.Action != "center" || cmd.Nudge["pan"] != -5 || cmd.Move["tilt"] != 1 || cmd.MoveTTL != 300*time.Millisecond {
		t.Errorf("cmd = %+v", cmd)
	}
	if _, err := ParseCommand([]byte(`{"nope": 1}`), specs); err == nil {
		t.Error("unknown key accepted")
	}
	if _, err := ParseCommand([]byte(`[1]`), specs); err == nil {
		t.Error("non-object accepted")
	}
	if _, err := ParseCommand([]byte(`{"anti_flicker": "70 Hz"}`), specs); err == nil {
		t.Error("bad enum accepted")
	}
}

func TestStatePayloadTypes(t *testing.T) {
	v1, v0, v2 := 1, 0, 2
	snap := service.Snapshot{
		Connected: true, Host: "pc",
		Controls: []service.ControlState{
			{ControlSpec: camera.ControlSpec{Key: "hdr", Kind: camera.KindBool}, Value: &v1},
			{ControlSpec: camera.ControlSpec{Key: "anti_flicker", Kind: camera.KindEnum, Options: camera.AntiFlickerOptions}, Value: &v2},
			{ControlSpec: camera.ControlSpec{Key: "pan", Kind: camera.KindRange}, Value: &v0},
		},
		Presets:      []service.PresetInfo{{ID: "a", Name: "Desk"}},
		ActivePreset: "a",
	}
	var doc map[string]any
	if err := json.Unmarshal(StatePayload(snap), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["hdr"] != true || doc["anti_flicker"] != "60 Hz" || doc["pan"] != 0.0 || doc["preset"] != "Desk" || doc["host"] != "pc" {
		t.Fatalf("doc = %v", doc)
	}
}

func TestDiscoveryPayload(t *testing.T) {
	snap := fakeSnapshot(t)
	topics := TopicsFor("insta360link", "homeassistant", snap.DeviceID)
	var doc struct {
		Dev  map[string]any            `json:"dev"`
		Cmps map[string]map[string]any `json:"cmps"`
		Avty string                    `json:"avty_t"`
		Stat string                    `json:"stat_t"`
		Cmd  string                    `json:"cmd_t"`
	}
	if err := json.Unmarshal(DiscoveryPayload(snap, topics, "Desk Cam"), &doc); err != nil {
		t.Fatal(err)
	}
	if topics.Discovery != "homeassistant/device/"+snap.DeviceID+"/config" {
		t.Errorf("discovery topic = %s", topics.Discovery)
	}
	if doc.Dev["name"] != "Desk Cam" || doc.Dev["sn"] != "17B0FAKE0000000000001" {
		t.Errorf("dev = %v", doc.Dev)
	}
	if doc.Avty != topics.Availability || doc.Stat != topics.State || doc.Cmd != topics.Set {
		t.Errorf("topics = %s %s %s", doc.Avty, doc.Stat, doc.Cmd)
	}
	wantPlatforms := map[string]string{
		"brightness": "number", "pan": "number", "hdr": "switch", "anti_flicker": "select",
		"preset": "select", "center": "button", "pan_left": "button", "host": "sensor",
		"streaming": "binary_sensor",
		"ai_mode":   "select", "composition": "select",
		"tracking_speed": "select",
	}
	for k, p := range wantPlatforms {
		if doc.Cmps[k]["p"] != p {
			t.Errorf("%s platform = %v, want %s", k, doc.Cmps[k]["p"], p)
		}
	}
	uids := map[any]bool{}
	for k, c := range doc.Cmps {
		if uids[c["unique_id"]] {
			t.Errorf("duplicate unique_id for %s", k)
		}
		uids[c["unique_id"]] = true
	}
	if doc.Cmps["pan"]["min"] != -145.0 || doc.Cmps["brightness"]["entity_category"] != "config" {
		t.Errorf("pan/brightness = %v / %v", doc.Cmps["pan"], doc.Cmps["brightness"])
	}
}

func fakeSnapshot(t *testing.T) service.Snapshot {
	t.Helper()
	svc, _ := startService(t)
	return svc.Snapshot()
}

func startService(t *testing.T) (*service.Service, *camera.FakeBackend) {
	t.Helper()
	fb := camera.NewFakeBackend()
	store := config.NewMemory(config.Defaults())
	svc := service.New(fb, store, quietLog(), service.Options{DiscoverInterval: 20 * time.Millisecond, MotionTick: 10 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go svc.Run(ctx)
	waitFor(t, "camera", func() bool { return svc.Snapshot().Connected })
	return svc, fb
}

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

type recorder struct {
	mu   sync.Mutex
	msgs map[string][]byte
}

func (r *recorder) get(topic string) []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.msgs[topic]
}

func startBroker(t *testing.T) (*mochi.Server, int, *recorder) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	srv := mochi.New(&mochi.Options{InlineClient: true, Logger: quietLog()})
	_ = srv.AddHook(new(auth.AllowHook), nil)
	if err := srv.AddListener(listeners.NewTCP(listeners.Config{ID: "t", Address: "127.0.0.1:" + strconv.Itoa(port)})); err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve() }()
	t.Cleanup(func() { srv.Close() })
	rec := &recorder{msgs: map[string][]byte{}}
	_ = srv.Subscribe("#", 1, func(_ *mochi.Client, _ packets.Subscription, pk packets.Packet) {
		rec.mu.Lock()
		rec.msgs[pk.TopicName] = append([]byte(nil), pk.Payload...)
		rec.mu.Unlock()
	})
	return srv, port, rec
}

func TestBridgeEndToEnd(t *testing.T) {
	srv, port, rec := startBroker(t)
	fb := camera.NewFakeBackend()
	cfg := config.Defaults()
	cfg.MQTT.Enabled = true
	cfg.MQTT.Host = "127.0.0.1"
	cfg.MQTT.Port = port
	store := config.NewMemory(cfg)
	svc := service.New(fb, store, quietLog(), service.Options{DiscoverInterval: 20 * time.Millisecond, MotionTick: 10 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go svc.Run(ctx)
	waitFor(t, "camera", func() bool { return svc.Snapshot().Connected })

	br := NewBridge(svc, store, &config.MemorySecrets{}, quietLog())
	go br.Run(ctx)
	waitFor(t, "connected", func() bool { return br.Status().State == StateConnected })

	id := svc.Snapshot().DeviceID
	tp := TopicsFor("insta360link", "homeassistant", id)
	waitFor(t, "discovery", func() bool { return rec.get(tp.Discovery) != nil })
	waitFor(t, "online", func() bool { return string(rec.get(tp.Availability)) == "online" })
	waitFor(t, "state", func() bool { return rec.get(tp.State) != nil })

	// Command from Home Assistant.
	if err := srv.Publish(tp.Set, []byte(`{"brightness": 72, "hdr": true}`), false, 1); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "brightness applied", func() bool { return svc.Snapshot().Values[camera.Brightness] == 72 })
	waitFor(t, "state republished", func() bool {
		var doc map[string]any
		_ = json.Unmarshal(rec.get(tp.State), &doc)
		return doc["brightness"] == 72.0 && doc["hdr"] == true
	})

	// Preset select from HA and options in discovery.
	_ = svc.SetControls(map[string]int{camera.Pan: 20})
	p, err := svc.SavePreset("Desk", []string{"position"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "preset in discovery", func() bool {
		var doc struct {
			Cmps map[string]struct {
				Options []string `json:"options"`
			} `json:"cmps"`
		}
		_ = json.Unmarshal(rec.get(tp.Discovery), &doc)
		return len(doc.Cmps["preset"].Options) == 2 && doc.Cmps["preset"].Options[1] == "Desk"
	})
	_ = svc.Center()
	_ = srv.Publish(tp.Set, []byte(`{"preset": "Desk"}`), false, 1)
	waitFor(t, "preset applied", func() bool { return svc.Snapshot().ActivePreset == p.ID })

	// Nudge button.
	_ = srv.Publish(tp.Set, []byte(`{"nudge":{"pan":-10}}`), false, 1)
	waitFor(t, "nudge", func() bool { return svc.Snapshot().Values[camera.Pan] == 10 })

	// Unplug -> offline, replug -> online.
	fb.Plug(false)
	svc.RequestPoll()
	waitFor(t, "offline", func() bool { return string(rec.get(tp.Availability)) == "offline" })
	fb.Plug(true)
	waitFor(t, "online again", func() bool { return string(rec.get(tp.Availability)) == "online" })

	// Disabling MQTT disconnects cleanly and marks offline.
	_, _ = store.Update(func(c *config.Config) error { c.MQTT.Enabled = false; return nil })
	br.Reload()
	waitFor(t, "disabled", func() bool { return br.Status().State == StateDisabled })
	waitFor(t, "offline on disable", func() bool { return string(rec.get(tp.Availability)) == "offline" })
}

func TestConnectionTest(t *testing.T) {
	_, port, _ := startBroker(t)
	ok := config.MQTT{Host: "127.0.0.1", Port: port, TLS: config.TLSOff}
	if err := Test(context.Background(), ok, ""); err != nil {
		t.Fatalf("test against running broker: %v", err)
	}
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	dead := l.Addr().(*net.TCPAddr).Port
	l.Close()
	bad := config.MQTT{Host: "127.0.0.1", Port: dead, TLS: config.TLSOff}
	if err := Test(context.Background(), bad, ""); err == nil {
		t.Fatal("expected error for closed port")
	}
}

package mqtt

import (
	"github.com/tlanfer/linkctl/internal/camera"
	"testing"
)

func TestReadmeExamples(t *testing.T) {
	for _, p := range []string{`{"brightness": 60, "hdr": true, "anti_flicker": "50 Hz"}`, `{"preset": "Desk"}`, `{"ai_mode": "Tracking", "composition": "Upper body"}`, `{"action": "center"}`, `{"nudge": {"pan": -3, "zoom": 25}}`, `{"move": {"pan": 0.5}, "ttl_ms": 500}`} {
		c, err := ParseCommand([]byte(p), camera.DefaultSpecs())
		if err != nil {
			t.Errorf("%s: %v", p, err)
		}
		_ = c
	}
	c, _ := ParseCommand([]byte(`{"ai_mode": "Tracking", "composition": "Upper body"}`), camera.DefaultSpecs())
	if c.Values["ai_mode"] != 1 || c.Values["composition"] != 2 {
		t.Errorf("values %v", c.Values)
	}
}

package mqtt

import (
	"encoding/json"
	"fmt"

	"github.com/tlanfer/linkctl/internal/camera"
	"github.com/tlanfer/linkctl/internal/service"
)

// SoftwareName and SoftwareVersion identify linkctl as the discovery origin.
var (
	SoftwareName    = "linkctl"
	SoftwareVersion = "dev"
	SoftwareURL     = "https://github.com/tlanfer/linkctl"
)

// nudge buttons: key -> (label, icon, payload)
var nudgeButtons = []struct {
	key, name, icon string
	payload         string
}{
	{"center", "Center", "mdi:image-filter-center-focus", `{"action":"center"}`},
	{"pan_left", "Pan left", "mdi:arrow-left", `{"nudge":{"pan":-3}}`},
	{"pan_right", "Pan right", "mdi:arrow-right", `{"nudge":{"pan":3}}`},
	{"tilt_up", "Tilt up", "mdi:arrow-up", `{"nudge":{"tilt":3}}`},
	{"tilt_down", "Tilt down", "mdi:arrow-down", `{"nudge":{"tilt":-3}}`},
	{"zoom_in", "Zoom in", "mdi:magnify-plus-outline", `{"nudge":{"zoom":25}}`},
	{"zoom_out", "Zoom out", "mdi:magnify-minus-outline", `{"nudge":{"zoom":-25}}`},
}

var controlIcons = map[string]string{
	camera.Brightness:       "mdi:brightness-6",
	camera.Contrast:         "mdi:contrast-box",
	camera.Saturation:       "mdi:palette",
	camera.Sharpness:        "mdi:image-filter-tilt-shift",
	camera.Hue:              "mdi:looks",
	camera.ExposureComp:     "mdi:camera-iris",
	camera.WhiteBalanceAuto: "mdi:white-balance-auto",
	camera.WhiteBalanceTemp: "mdi:thermometer",
	camera.AntiFlicker:      "mdi:sine-wave",
	camera.HDR:              "mdi:hdr",
	camera.Mirror:           "mdi:flip-horizontal",
	camera.FocusAuto:        "mdi:focus-auto",
	camera.Focus:            "mdi:image-filter-center-focus-weak",
	camera.Pan:              "mdi:pan-horizontal",
	camera.Tilt:             "mdi:pan-vertical",
	camera.Zoom:             "mdi:magnify",
	camera.AIMode:           "mdi:robot-outline",
	camera.Composition:      "mdi:human-male-height-variant",
	camera.TrackingSpeed:    "mdi:speedometer",
}

// DiscoveryPayload renders the Home Assistant device-based discovery message.
func DiscoveryPayload(snap service.Snapshot, t Topics, deviceName string) []byte {
	id := snap.DeviceID
	dev := map[string]any{
		"ids":  []string{"linkctl_" + id},
		"name": deviceName,
		"mf":   "Insta360",
		"mdl":  "Link 2",
	}
	if snap.Device != nil {
		dev["mdl"] = snap.Device.Model
		if snap.Device.Firmware != "" {
			dev["sw"] = snap.Device.Firmware
		}
		if snap.Device.Serial != "" {
			dev["sn"] = snap.Device.Serial
		}
	}
	cmps := map[string]any{}
	for _, c := range snap.Controls {
		uid := fmt.Sprintf("linkctl_%s_%s", id, c.Key)
		vt := fmt.Sprintf("{{ value_json.%s }}", c.Key)
		base := map[string]any{
			"unique_id":      uid,
			"name":           c.Label,
			"icon":           controlIcons[c.Key],
			"value_template": vt,
		}
		if c.Group == camera.GroupPicture {
			base["entity_category"] = "config"
		}
		if c.ReadOnly {
			if c.Kind == camera.KindBool {
				base["p"] = "binary_sensor"
				base["value_template"] = fmt.Sprintf("{{ 'ON' if value_json.%s else 'OFF' }}", c.Key)
				base["payload_on"] = "ON"
				base["payload_off"] = "OFF"
			} else {
				base["p"] = "sensor"
				base["state_class"] = "measurement"
			}
			cmps[c.Key] = base
			continue
		}
		switch c.Kind {
		case camera.KindBool:
			base["p"] = "switch"
			base["value_template"] = fmt.Sprintf("{{ 'ON' if value_json.%s else 'OFF' }}", c.Key)
			base["payload_on"] = fmt.Sprintf(`{"%s":true}`, c.Key)
			base["payload_off"] = fmt.Sprintf(`{"%s":false}`, c.Key)
			base["state_on"] = "ON"
			base["state_off"] = "OFF"
		case camera.KindEnum:
			base["p"] = "select"
			base["options"] = c.Options
			base["command_template"] = fmt.Sprintf(`{"%s": "{{ value }}"}`, c.Key)
		default:
			base["p"] = "number"
			base["min"] = c.Min
			base["max"] = c.Max
			base["step"] = c.Step
			base["mode"] = "slider"
			base["command_template"] = fmt.Sprintf(`{"%s": {{ value }}}`, c.Key)
			switch c.Unit {
			case "°":
				base["unit_of_measurement"] = "°"
			case "%":
				base["unit_of_measurement"] = "%"
			case "K":
				base["unit_of_measurement"] = "K"
			}
		}
		cmps[c.Key] = base
	}

	options := []string{service.NoPresetOption}
	for _, p := range snap.Presets {
		options = append(options, p.Name)
	}
	cmps["preset"] = map[string]any{
		"p":                "select",
		"unique_id":        fmt.Sprintf("linkctl_%s_preset", id),
		"name":             "Preset",
		"icon":             "mdi:camera-switch",
		"options":          options,
		"value_template":   "{{ value_json.preset }}",
		"command_template": `{"preset": "{{ value }}"}`,
	}
	for _, b := range nudgeButtons {
		cmps[b.key] = map[string]any{
			"p":             "button",
			"unique_id":     fmt.Sprintf("linkctl_%s_%s", id, b.key),
			"name":          b.name,
			"icon":          b.icon,
			"payload_press": b.payload,
		}
	}
	cmps["reset"] = map[string]any{
		"p":               "button",
		"unique_id":       fmt.Sprintf("linkctl_%s_reset", id),
		"name":            "Reset picture",
		"icon":            "mdi:restore",
		"payload_press":   `{"action":"reset"}`,
		"entity_category": "config",
	}
	cmps["streaming"] = map[string]any{
		"p":              "binary_sensor",
		"unique_id":      fmt.Sprintf("linkctl_%s_streaming", id),
		"name":           "In use",
		"device_class":   "running",
		"icon":           "mdi:video",
		"value_template": "{{ 'ON' if value_json.streaming else 'OFF' }}",
		"payload_on":     "ON",
		"payload_off":    "OFF",
	}
	cmps["host"] = map[string]any{
		"p":               "sensor",
		"unique_id":       fmt.Sprintf("linkctl_%s_host", id),
		"name":            "Connected to",
		"icon":            "mdi:desktop-tower-monitor",
		"value_template":  "{{ value_json.host }}",
		"entity_category": "diagnostic",
	}

	doc := map[string]any{
		"dev": dev,
		"o": map[string]any{
			"name": SoftwareName,
			"sw":   SoftwareVersion,
			"url":  SoftwareURL,
		},
		"cmps":   cmps,
		"avty_t": t.Availability,
		"stat_t": t.State,
		"cmd_t":  t.Set,
		"qos":    1,
	}
	raw, _ := json.Marshal(doc)
	return raw
}

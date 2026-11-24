// Package mqtt bridges the service to an MQTT broker: it publishes the camera
// state, accepts commands and announces the device to Home Assistant.
package mqtt

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tlanfer/linkctl/internal/camera"
	"github.com/tlanfer/linkctl/internal/service"
)

// Topics for one device.
type Topics struct {
	Base, Availability, State, Set, Discovery string
}

// TopicsFor builds the topic set for a device id.
func TopicsFor(base, discoveryPrefix, id string) Topics {
	b := strings.TrimSuffix(base, "/") + "/" + id
	return Topics{
		Base:         b,
		Availability: b + "/availability",
		State:        b + "/state",
		Set:          b + "/set",
		Discovery:    strings.TrimSuffix(discoveryPrefix, "/") + "/device/" + id + "/config",
	}
}

// StatePayload renders the retained state document.
//
// Ranges are numbers, bools are true/false, enums are their label, the
// active preset is its name (or "None").
func StatePayload(snap service.Snapshot) []byte {
	doc := map[string]any{
		"connected": snap.Connected,
		"streaming": snap.Streaming,
		"host":      snap.Host,
		"preset":    service.NoPresetOption,
	}
	if snap.Device != nil {
		doc["model"] = snap.Device.Model
		doc["serial"] = snap.Device.Serial
		doc["firmware"] = snap.Device.Firmware
	}
	for _, p := range snap.Presets {
		if p.ID == snap.ActivePreset {
			doc["preset"] = p.Name
		}
	}
	for _, c := range snap.Controls {
		if c.Value == nil {
			continue
		}
		v := *c.Value
		switch c.Kind {
		case camera.KindBool:
			doc[c.Key] = v != 0
		case camera.KindEnum:
			if v >= 0 && v < len(c.Options) {
				doc[c.Key] = c.Options[v]
			} else {
				doc[c.Key] = v
			}
		default:
			doc[c.Key] = v
		}
	}
	raw, _ := json.Marshal(doc)
	return raw
}

// Command is a parsed message from the set topic.
type Command struct {
	Values  map[string]int
	Preset  string
	Action  string // "center" or "reset"
	Nudge   map[string]int
	Move    map[string]float64
	MoveTTL time.Duration
}

// ParseCommand decodes a set-topic payload against the control specs.
func ParseCommand(payload []byte, specs []camera.ControlSpec) (Command, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		return Command{}, fmt.Errorf("payload must be a JSON object: %w", err)
	}
	cmd := Command{Values: map[string]int{}}
	var errs []error
	for key, val := range raw {
		switch key {
		case "preset":
			if err := json.Unmarshal(val, &cmd.Preset); err != nil {
				errs = append(errs, fmt.Errorf("preset: expected string"))
			}
		case "action":
			if err := json.Unmarshal(val, &cmd.Action); err != nil || (cmd.Action != "center" && cmd.Action != "reset") {
				errs = append(errs, fmt.Errorf("action: expected \"center\" or \"reset\""))
			}
		case "nudge":
			if err := json.Unmarshal(val, &cmd.Nudge); err != nil {
				errs = append(errs, fmt.Errorf("nudge: expected {\"pan\": -5, …}"))
			}
		case "move":
			if err := json.Unmarshal(val, &cmd.Move); err != nil {
				errs = append(errs, fmt.Errorf("move: expected {\"pan\": 1, …}"))
			}
		case "ttl_ms":
			var ms float64
			if err := json.Unmarshal(val, &ms); err != nil {
				errs = append(errs, fmt.Errorf("ttl_ms: expected number"))
			}
			cmd.MoveTTL = time.Duration(ms) * time.Millisecond
		default:
			i := slices.IndexFunc(specs, func(s camera.ControlSpec) bool { return s.Key == key })
			if i < 0 {
				errs = append(errs, fmt.Errorf("unknown key %q", key))
				continue
			}
			v, err := decodeValue(specs[i], val)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", key, err))
				continue
			}
			cmd.Values[key] = v
		}
	}
	return cmd, errors.Join(errs...)
}

func decodeValue(spec camera.ControlSpec, raw json.RawMessage) (int, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, err
	}
	switch x := v.(type) {
	case bool:
		if x {
			return 1, nil
		}
		return 0, nil
	case float64:
		return int(math.Round(x)), nil
	case string:
		s := strings.TrimSpace(x)
		switch strings.ToUpper(s) {
		case "ON", "TRUE":
			if spec.Kind == camera.KindBool {
				return 1, nil
			}
		case "OFF", "FALSE":
			if spec.Kind == camera.KindBool {
				return 0, nil
			}
		}
		for i, o := range spec.Options {
			if strings.EqualFold(o, s) {
				return i, nil
			}
		}
		if n, err := strconv.ParseFloat(s, 64); err == nil {
			return int(math.Round(n)), nil
		}
		return 0, fmt.Errorf("invalid value %q", s)
	}
	return 0, fmt.Errorf("unsupported value %s", raw)
}

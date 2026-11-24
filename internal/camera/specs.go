// Package camera models the Insta360 Link controls independently of the
// operating system. OS backends implement Driver; Camera adds vendor
// extension-unit controls and device identity on top.
//
// Control ranges, XU selectors and value encodings are derived from
// link-studio (MIT, https://github.com/jweaver60/link-studio).
package camera

// Kind describes how a control's value is interpreted.
type Kind string

const (
	KindRange Kind = "range"
	KindBool  Kind = "bool"
	KindEnum  Kind = "enum"
)

// Group is used by presets to decide which controls are captured.
type Group string

const (
	GroupPicture  Group = "picture"
	GroupPosition Group = "position"
	// GroupAI holds the firmware AI features (modes, tracking, presence).
	// Presets never capture it.
	GroupAI Group = "ai"
)

// ControlSpec is the metadata of one control.
type ControlSpec struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Group   Group    `json:"group"`
	Kind    Kind     `json:"kind"`
	Min     int      `json:"min"`
	Max     int      `json:"max"`
	Step    int      `json:"step"`
	Default int      `json:"default"`
	Unit    string   `json:"unit,omitempty"`
	Options []string `json:"options,omitempty"` // enum labels, index == value
	// AutoKey names a bool control that, when enabled, drives this control
	// automatically (e.g. white_balance_temperature -> white_balance_auto).
	AutoKey string `json:"auto_key,omitempty"`
	// Vendor marks controls implemented through the Insta360 extension unit.
	Vendor bool `json:"vendor,omitempty"`
	// ReadOnly controls are status values reported by the camera.
	ReadOnly bool `json:"read_only,omitempty"`
}

// Clamp limits v to the spec's range.
func (s ControlSpec) Clamp(v int) int {
	if v < s.Min {
		return s.Min
	}
	if v > s.Max {
		return s.Max
	}
	return v
}

// Control keys.
const (
	Brightness       = "brightness"
	Contrast         = "contrast"
	Saturation       = "saturation"
	Sharpness        = "sharpness"
	Hue              = "hue"
	WhiteBalanceAuto = "white_balance_auto"
	WhiteBalanceTemp = "white_balance_temperature"
	AntiFlicker      = "anti_flicker"
	ExposureComp     = "exposure_compensation"
	HDR              = "hdr"
	Mirror           = "mirror"
	FocusAuto        = "focus_auto"
	Focus            = "focus"
	AIMode           = "ai_mode"
	// Composition merges the smart-composition switch (feature bit 0) and its
	// framing (XU 9/0x13): 0 off, 1 head, 2 upper body, 3 full body.
	Composition   = "composition"
	TrackingSpeed = "tracking_speed"
	Pan           = "pan"
	Tilt          = "tilt"
	Zoom          = "zoom"
)

// StandardKeys are implemented by OS drivers through standard UVC controls.
var StandardKeys = []string{
	Brightness, Contrast, Saturation, Sharpness, Hue,
	WhiteBalanceAuto, WhiteBalanceTemp, AntiFlicker,
	FocusAuto, Focus, Pan, Tilt, Zoom,
}

// AI mode options. Index = control value; AIModeIDs maps to the firmware
// video-mode id in XU 9/0x02 byte 0. The firmware also has group tracking (7)
// and whiteboard (4); they are deliberately not offered (untested).
var (
	AIModeOptions = []string{"Normal", "Tracking", "DeskView"}
	AIModeIDs     = []byte{0x00, 0x01, 0x06}
	// AIModeFlags is byte 1 of the mode write, as sent by the official app.
	AIModeFlags = []byte{0x00, 0x00, 0x10}
	// AIModeTracking and AIModeDeskView are control values.
	AIModeTracking = 1
	AIModeDeskView = 2
	// DeskViewTilt is where linkctl points the gimbal after switching to
	// DeskView (the firmware does not tilt down by itself).
	DeskViewTilt = -42

	// CompositionOptions: index 0 = off, otherwise index = XU 9/0x13 value.
	CompositionOptions = []string{"Off", "Head", "Upper body", "Full body"}
	// TrackingSpeedOptions: XU 9/0x12 value = 2 - index (the firmware's
	// 0 is the fastest; confirmed by feel on hardware).
	TrackingSpeedOptions = []string{"Slow", "Normal", "Fast"}
)

// AntiFlickerOptions are the anti_flicker enum labels (UVC power line frequency).
var AntiFlickerOptions = []string{"Off", "50 Hz", "60 Hz", "Auto"}

// DefaultSpecs is the control table in display order. Ranges are fallbacks;
// drivers may override them with values reported by the device.
func DefaultSpecs() []ControlSpec {
	return []ControlSpec{
		{Key: Brightness, Label: "Brightness", Group: GroupPicture, Kind: KindRange, Min: 0, Max: 100, Step: 1, Default: 50},
		{Key: Contrast, Label: "Contrast", Group: GroupPicture, Kind: KindRange, Min: 0, Max: 100, Step: 1, Default: 50},
		{Key: Saturation, Label: "Saturation", Group: GroupPicture, Kind: KindRange, Min: 0, Max: 100, Step: 1, Default: 50},
		{Key: Sharpness, Label: "Sharpness", Group: GroupPicture, Kind: KindRange, Min: 0, Max: 100, Step: 1, Default: 50},
		{Key: Hue, Label: "Hue", Group: GroupPicture, Kind: KindRange, Min: -15, Max: 15, Step: 1, Default: 0},
		{Key: ExposureComp, Label: "Exposure compensation", Group: GroupPicture, Kind: KindRange, Min: 0, Max: 100, Step: 1, Default: 50, Vendor: true},
		{Key: WhiteBalanceAuto, Label: "Auto white balance", Group: GroupPicture, Kind: KindBool, Min: 0, Max: 1, Step: 1, Default: 1},
		{Key: WhiteBalanceTemp, Label: "Color temperature", Group: GroupPicture, Kind: KindRange, Min: 2000, Max: 10000, Step: 100, Default: 6400, Unit: "K", AutoKey: WhiteBalanceAuto},
		{Key: AntiFlicker, Label: "Anti-flicker", Group: GroupPicture, Kind: KindEnum, Min: 0, Max: 3, Step: 1, Default: 3, Options: AntiFlickerOptions},
		{Key: HDR, Label: "HDR", Group: GroupPicture, Kind: KindBool, Min: 0, Max: 1, Step: 1, Default: 0, Vendor: true},
		{Key: Mirror, Label: "Mirror", Group: GroupPicture, Kind: KindBool, Min: 0, Max: 1, Step: 1, Default: 0, Vendor: true},
		{Key: FocusAuto, Label: "Autofocus", Group: GroupPicture, Kind: KindBool, Min: 0, Max: 1, Step: 1, Default: 1},
		{Key: Focus, Label: "Focus", Group: GroupPicture, Kind: KindRange, Min: 0, Max: 100, Step: 1, Default: 50, AutoKey: FocusAuto},
		{Key: AIMode, Label: "Mode", Group: GroupAI, Kind: KindEnum, Min: 0, Max: len(AIModeOptions) - 1, Step: 1, Default: 0, Options: AIModeOptions, Vendor: true},
		{Key: Composition, Label: "Composition", Group: GroupAI, Kind: KindEnum, Min: 0, Max: 3, Step: 1, Default: 0, Options: CompositionOptions, Vendor: true},
		{Key: TrackingSpeed, Label: "Tracking speed", Group: GroupAI, Kind: KindEnum, Min: 0, Max: 2, Step: 1, Default: 1, Options: TrackingSpeedOptions, Vendor: true},
		{Key: Pan, Label: "Pan", Group: GroupPosition, Kind: KindRange, Min: -145, Max: 145, Step: 1, Default: 0, Unit: "°"},
		{Key: Tilt, Label: "Tilt", Group: GroupPosition, Kind: KindRange, Min: -90, Max: 100, Step: 1, Default: 0, Unit: "°"},
		{Key: Zoom, Label: "Zoom", Group: GroupPosition, Kind: KindRange, Min: 100, Max: 400, Step: 1, Default: 100, Unit: "%"},
	}
}

// Insta360 USB identifiers.
const VendorID = 0x2E1A

// Products maps known product ids to model names.
var Products = map[uint16]string{
	0x4C01: "Insta360 Link",
	0x4C02: "Insta360 Link 2C",
	0x4C03: "Insta360 Link 2 Pro",
	0x4C04: "Insta360 Link 2",
}

// Extension unit addressing (UVC unit ids as used by the Linux driver).
const (
	XUFeatureUnit = 9
	XUPrivacyUnit = 10

	XUSelDeviceInfo   = 0x03
	XUSelExposureComp = 0x09
	XUSelFeatureMask  = 0x1B

	// Gimbal speed control, as used by the official Insta360 app's joystick:
	// [panDir, panSpeed, tiltDir, tiltSpeed], dir -1/0/+1 as int8, speed
	// 1..127 (about 2 deg/s per step). Note tilt dir +1 moves DOWN.
	// The camera keeps moving until a stop ({0,1,0,1}) is written.
	XUSelPanTiltRelative = 0x16
	// Actual gimbal position, live during motion: int32 LE tilt, int32 LE
	// pan, in arc-seconds (1/3600 deg). The standard UVC pan/tilt reads only
	// refresh about once per second.
	XUSelPanTiltAbsolute = 0x1A

	XUSelVideoMode  = 0x02 // 61 bytes: [0] mode id (0xFF = switching), [1] flag, [54] status
	XUSelTrackSpeed = 0x12
	XUSelFraming    = 0x13
	VideoModeLen    = 61

	FeatureBitSmartComposition = 0

	FeatureBitHDR    = 2
	FeatureBitMirror = 3
)

// XU unit GUIDs (from the UVC descriptors), used by backends that address
// extension units by GUID rather than unit id.
const (
	XUFeatureUnitGUID = "FAF1672D-B71B-4793-8C91-7B1C9B7F95F8"
	XUPrivacyUnitGUID = "E307E649-4618-A3FF-82FC-2D8B5F216773"
)

// MigrateValues rewrites control values saved by older versions: the former
// "smart_composition" (bool) + "composition_framing" (0 head .. 2 full body)
// pair becomes "composition". It reports whether anything changed.
func MigrateValues(v map[string]int) bool {
	on, hasOn := v["smart_composition"]
	fr, hasFr := v["composition_framing"]
	if !hasOn && !hasFr {
		return false
	}
	delete(v, "smart_composition")
	delete(v, "composition_framing")
	if _, ok := v[Composition]; !ok {
		switch {
		case hasOn && on == 0:
			v[Composition] = 0
		case hasFr:
			v[Composition] = min(max(fr+1, 1), 3)
		default:
			v[Composition] = 2 // on without framing: upper body
		}
	}
	return true
}

package camera

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ErrUnsupported is returned for controls a driver does not implement.
var ErrUnsupported = errors.New("control not supported")

// Descriptor identifies an attached camera before it is opened.
type Descriptor struct {
	Path      string `json:"path"` // OS-specific handle (device path, registry id, …)
	Name      string `json:"name"`
	VendorID  uint16 `json:"vendor_id"`
	ProductID uint16 `json:"product_id"`
}

// Model returns the human model name.
func (d Descriptor) Model() string {
	if m, ok := Products[d.ProductID]; ok {
		return m
	}
	return d.Name
}

// Range is a range reported by the device for a standard control.
type Range struct {
	Min, Max, Step, Default int
}

// Backend enumerates and opens cameras on one OS.
type Backend interface {
	List() ([]Descriptor, error)
	Open(d Descriptor) (Driver, error)
}

// Driver is the OS-specific transport for one opened camera.
//
// Standard keys (StandardKeys) are expressed in the units of ControlSpec:
// pan/tilt in degrees, zoom in percent, bools as 0/1. Setting a value on a
// control with an AutoKey implicitly switches that control to manual.
type Driver interface {
	StdRange(key string) (Range, error)
	StdGet(key string) (int, error)
	StdSet(key string, value int) error
	// XUGet reads the current value of an extension-unit selector. Unit ids
	// follow the UVC descriptor (XUFeatureUnit, XUPrivacyUnit).
	XUGet(unit, selector uint8) ([]byte, error)
	XUSet(unit, selector uint8, data []byte) error
	Close() error
}

// Info is the identity of an opened camera.
type Info struct {
	Model     string `json:"model"`
	Name      string `json:"name"`
	ProductID uint16 `json:"product_id"`
	Serial    string `json:"serial,omitempty"`
	Firmware  string `json:"firmware,omitempty"`
}

// Camera is an opened camera with the full iteration-1 control set.
type Camera struct {
	mu    sync.Mutex
	drv   Driver
	info  Info
	specs []ControlSpec
	index map[string]int
	// drive reports whether the gimbal speed control (XU 9/0x16) exists.
	drive bool
	// precise reports whether the live position (XU 9/0x1A) is readable.
	precise bool
	// lastMode is the last stable AI mode index (the firmware reports 0xFF
	// while switching).
	lastMode int
}

// Open opens the camera and resolves its control ranges and identity.
func Open(b Backend, d Descriptor) (*Camera, error) {
	drv, err := b.Open(d)
	if err != nil {
		return nil, err
	}
	return New(drv, d), nil
}

// New wraps an already opened driver.
func New(drv Driver, d Descriptor) *Camera {
	c := &Camera{drv: drv, info: Info{Model: d.Model(), Name: d.Name, ProductID: d.ProductID}}
	if raw, err := drv.XUGet(XUFeatureUnit, XUSelDeviceInfo); err == nil {
		c.info.Serial, c.info.Firmware = ParseDeviceInfo(raw)
	}
	if raw, err := drv.XUGet(XUFeatureUnit, XUSelPanTiltRelative); err == nil && len(raw) == 4 {
		c.drive = true
	}
	if raw, err := drv.XUGet(XUFeatureUnit, XUSelPanTiltAbsolute); err == nil && len(raw) >= 8 {
		c.precise = true
	}
	c.specs = DefaultSpecs()
	c.index = make(map[string]int, len(c.specs))
	for i := range c.specs {
		s := &c.specs[i]
		c.index[s.Key] = i
		if s.Vendor || s.Kind != KindRange {
			continue
		}
		if r, err := drv.StdRange(s.Key); err == nil && r.Max > r.Min {
			s.Min, s.Max, s.Default = r.Min, r.Max, r.Default
			if r.Step > 0 && s.Key != WhiteBalanceTemp {
				s.Step = r.Step
			}
		}
	}
	return c
}

// Info returns the camera identity.
func (c *Camera) Info() Info { return c.info }

// Specs returns the control metadata in display order.
func (c *Camera) Specs() []ControlSpec { return append([]ControlSpec(nil), c.specs...) }

// Spec looks up a control's metadata.
func (c *Camera) Spec(key string) (ControlSpec, bool) {
	i, ok := c.index[key]
	if !ok {
		return ControlSpec{}, false
	}
	return c.specs[i], true
}

// Close releases the driver.
func (c *Camera) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.drv.Close()
}

// Get reads one control.
func (c *Camera) Get(key string) (int, error) {
	if _, ok := c.index[key]; !ok {
		return 0, fmt.Errorf("unknown control %q", key)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.get(key)
}

func (c *Camera) get(key string) (int, error) {
	switch key {
	case HDR:
		return c.featureBit(FeatureBitHDR)
	case Mirror:
		return c.featureBit(FeatureBitMirror)
	case ExposureComp:
		raw, err := c.drv.XUGet(XUFeatureUnit, XUSelExposureComp)
		if err != nil {
			return 0, err
		}
		if len(raw) < 2 {
			return 0, fmt.Errorf("exposure compensation: short read (%d bytes)", len(raw))
		}
		return DecodeExposureComp(raw), nil
	case Composition:
		on, err := c.featureBit(FeatureBitSmartComposition)
		if err != nil || on == 0 {
			return 0, err
		}
		v, err := c.xuByte(XUFeatureUnit, XUSelFraming, 0)
		return min(max(v, 1), 3), err
	case AIMode:
		raw, err := c.videoMode()
		if err != nil {
			return 0, err
		}
		for i, id := range AIModeIDs {
			if raw[0] == id {
				c.lastMode = i
				return i, nil
			}
		}
		return c.lastMode, nil // 0xFF while switching, or a mode we do not model
	case TrackingSpeed:
		v, err := c.xuByte(XUFeatureUnit, XUSelTrackSpeed, 0)
		return 2 - min(v, 2), err
	default:
		return c.drv.StdGet(key)
	}
}

func (c *Camera) videoMode() ([]byte, error) {
	raw, err := c.drv.XUGet(XUFeatureUnit, XUSelVideoMode)
	if err != nil {
		return nil, err
	}
	if len(raw) < VideoModeLen {
		return nil, fmt.Errorf("video mode: short read (%d bytes)", len(raw))
	}
	return raw, nil
}

func (c *Camera) xuByte(unit, sel uint8, idx int) (int, error) {
	raw, err := c.drv.XUGet(unit, sel)
	if err != nil {
		return 0, err
	}
	if len(raw) <= idx {
		return 0, fmt.Errorf("xu %d/0x%02x: short read (%d bytes)", unit, sel, len(raw))
	}
	return int(raw[idx]), nil
}

// modeSwitchDelay is the pause between the reset and the target mode write,
// mirroring link-studio's observed firmware handshake (rapid writes are
// known to wedge the device).
var modeSwitchDelay = 600 * time.Millisecond

// setVideoMode writes an AI mode. The firmware only applies modes while a
// client streams video. Caller holds c.mu.
func (c *Camera) setVideoMode(idx int) error {
	cur, err := c.videoMode()
	if err != nil {
		return err
	}
	payload := func(id, flag byte) []byte {
		p := make([]byte, VideoModeLen)
		copy(p[52:], cur[52:]) // keep the trailing parameter block
		p[0], p[1] = id, flag
		return p
	}
	if idx != 0 {
		if err := c.drv.XUSet(XUFeatureUnit, XUSelVideoMode, payload(0, 0)); err != nil {
			return err
		}
		time.Sleep(modeSwitchDelay)
	}
	if err := c.drv.XUSet(XUFeatureUnit, XUSelVideoMode, payload(AIModeIDs[idx], AIModeFlags[idx])); err != nil {
		return err
	}
	c.lastMode = idx
	return nil
}

// MaxDriveSpeed is the highest speed Drive accepts (~2 deg/s per step).
const MaxDriveSpeed = 127

// CanDrive reports whether the camera supports gimbal speed control.
func (c *Camera) CanDrive() bool { return c.drive }

// Drive sets the gimbal's continuous pan/tilt speed. Positive pan turns
// right (pan increases), positive tilt turns up (tilt increases); 0 stops the
// axis. The camera keeps moving until Drive(0, 0) is sent.
func (c *Camera) Drive(pan, tilt int) error {
	if !c.drive {
		return ErrUnsupported
	}
	axis := func(v int, invert bool) (byte, byte) {
		if v == 0 {
			return 0, 1
		}
		dir := byte(1)
		if (v < 0) != invert {
			dir = 0xff
		}
		if v < 0 {
			v = -v
		}
		return dir, byte(min(v, MaxDriveSpeed))
	}
	pd, ps := axis(pan, false)
	td, ts := axis(tilt, true) // firmware: tilt dir +1 moves down
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.drv.XUSet(XUFeatureUnit, XUSelPanTiltRelative, []byte{pd, ps, td, ts})
}

// PanTiltPrecise returns the actual gimbal position in degrees.
func (c *Camera) PanTiltPrecise() (pan, tilt float64, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.panTiltPrecise()
}

func (c *Camera) panTiltPrecise() (pan, tilt float64, err error) {
	if !c.precise {
		return 0, 0, ErrUnsupported
	}
	raw, err := c.drv.XUGet(XUFeatureUnit, XUSelPanTiltAbsolute)
	if err != nil {
		return 0, 0, err
	}
	if len(raw) < 8 {
		return 0, 0, fmt.Errorf("pan/tilt position: short read (%d bytes)", len(raw))
	}
	tilt = float64(int32(binary.LittleEndian.Uint32(raw))) / 3600
	pan = float64(int32(binary.LittleEndian.Uint32(raw[4:]))) / 3600
	return pan, tilt, nil
}

// Write sets a standard control without reading it back (used for
// high-rate motion). Values are not validated beyond the spec range.
func (c *Camera) Write(key string, value int) error {
	spec, ok := c.Spec(key)
	if !ok || spec.Vendor {
		return fmt.Errorf("unsupported control %q", key)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.drv.StdSet(key, spec.Clamp(value))
}

// Set validates and writes one control, returning the value read back.
func (c *Camera) Set(key string, value int) (int, error) {
	spec, ok := c.Spec(key)
	if !ok {
		return 0, fmt.Errorf("unknown control %q", key)
	}
	if spec.ReadOnly {
		return 0, fmt.Errorf("%s is read-only", key)
	}
	if value < spec.Min || value > spec.Max {
		return 0, fmt.Errorf("%s must be between %d and %d", key, spec.Min, spec.Max)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var err error
	switch key {
	case HDR:
		err = c.setFeatureBit(FeatureBitHDR, value != 0)
	case Mirror:
		err = c.setFeatureBit(FeatureBitMirror, value != 0)
	case ExposureComp:
		err = c.drv.XUSet(XUFeatureUnit, XUSelExposureComp, EncodeExposureComp(value))
	case Composition:
		// Set the framing first, then enable, so the camera never reframes
		// to a stale framing.
		if value > 0 {
			err = c.drv.XUSet(XUFeatureUnit, XUSelFraming, []byte{byte(value)})
		}
		if err == nil {
			err = c.setFeatureBit(FeatureBitSmartComposition, value > 0)
		}
	case AIMode:
		err = c.setVideoMode(value)
		if err == nil {
			return value, nil // the firmware switches asynchronously (reads 0xFF meanwhile)
		}
	case TrackingSpeed:
		err = c.drv.XUSet(XUFeatureUnit, XUSelTrackSpeed, []byte{byte(2 - value)})
	default:
		err = c.drv.StdSet(key, value)
	}
	if err != nil {
		return 0, err
	}
	return c.get(key)
}

// ReadAll reads every control. Controls that fail are reported in errs.
func (c *Camera) ReadAll() (values map[string]int, errs map[string]error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	values = make(map[string]int, len(c.specs))
	var (
		mask     uint16
		maskErr  error
		maskRead bool
	)
	for _, s := range c.specs {
		var v int
		var err error
		switch s.Key {
		case HDR, Mirror:
			if !maskRead {
				mask, maskErr = c.featureMask()
				maskRead = true
			}
			err = maskErr
			bit := map[string]int{HDR: FeatureBitHDR, Mirror: FeatureBitMirror}[s.Key]
			if err == nil {
				v = int(mask>>bit) & 1
			}
		default:
			v, err = c.get(s.Key)
		}
		if err != nil {
			if errs == nil {
				errs = map[string]error{}
			}
			errs[s.Key] = err
			continue
		}
		values[s.Key] = v
	}
	// The standard pan/tilt reads lag up to a second behind the gimbal; use
	// the live position where available.
	if p, t, err := c.panTiltPrecise(); err == nil {
		values[Pan] = int(math.Round(p))
		values[Tilt] = int(math.Round(t))
		delete(errs, Pan)
		delete(errs, Tilt)
		if len(errs) == 0 {
			errs = nil
		}
	}
	return values, errs
}

func (c *Camera) featureMask() (uint16, error) {
	raw, err := c.drv.XUGet(XUFeatureUnit, XUSelFeatureMask)
	if err != nil {
		return 0, err
	}
	if len(raw) < 2 {
		return 0, fmt.Errorf("feature mask: short read (%d bytes)", len(raw))
	}
	return binary.LittleEndian.Uint16(raw), nil
}

func (c *Camera) featureBit(bit int) (int, error) {
	m, err := c.featureMask()
	if err != nil {
		return 0, err
	}
	return int(m>>bit) & 1, nil
}

func (c *Camera) setFeatureBit(bit int, on bool) error {
	m, err := c.featureMask()
	if err != nil {
		return err
	}
	if on {
		m |= 1 << bit
	} else {
		m &^= 1 << bit
	}
	return c.drv.XUSet(XUFeatureUnit, XUSelFeatureMask, binary.LittleEndian.AppendUint16(nil, m))
}

// DecodeExposureComp converts the XU payload (int16 LE, step 6 around 0) to 0..100.
func DecodeExposureComp(raw []byte) int {
	v := int(int16(binary.LittleEndian.Uint16(raw)))
	// Python floor division semantics, matching link-studio.
	q := v / 6
	if v%6 != 0 && v < 0 {
		q--
	}
	return min(100, max(0, q+50))
}

// EncodeExposureComp converts 0..100 to the XU payload.
func EncodeExposureComp(v int) []byte {
	return binary.LittleEndian.AppendUint16(nil, uint16(int16((v-50)*6)))
}

var (
	reSerial    = regexp.MustCompile(`^[A-Z0-9]{18,30}$`)
	rePrintable = regexp.MustCompile(`[\x20-\x7e]{4,}`)
)

// ParseDeviceInfo extracts the serial and firmware version from the XU
// device-info payload (unit 9, selector 0x03).
func ParseDeviceInfo(raw []byte) (serial, firmware string) {
	for _, m := range rePrintable.FindAll(raw, -1) {
		s := string(m)
		switch {
		case firmware == "" && (strings.HasPrefix(s, "v") || strings.HasPrefix(s, "V")):
			firmware = s
		case serial == "" && reSerial.MatchString(s) && strings.ContainsAny(s, "0123456789"):
			serial = s
		}
	}
	return serial, firmware
}

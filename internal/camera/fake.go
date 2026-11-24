package camera

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"
)

// FakeDriveDegPerStep is the simulated gimbal speed per Drive speed step.
const FakeDriveDegPerStep = 2.0

// ErrDisconnected is returned by the fake driver after Unplug.
var ErrDisconnected = errors.New("device disconnected")

// FakeBackend is an in-memory backend for tests and `linkctl --fake`.
type FakeBackend struct {
	mu      sync.Mutex
	present bool
	Desc    Descriptor
	Serial  string
	drivers []*FakeDriver
	OpenErr error
	// NoDrive opens cameras without gimbal speed control (older models).
	NoDrive bool
}

// NewFakeBackend returns a backend with one attached Link 2.
func NewFakeBackend() *FakeBackend {
	return &FakeBackend{
		present: true,
		Desc:    Descriptor{Path: "fake://link2", Name: "Insta360 Link 2", VendorID: VendorID, ProductID: 0x4C04},
		Serial:  "17B0FAKE0000000000001",
	}
}

func (b *FakeBackend) List() ([]Descriptor, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.present {
		return nil, nil
	}
	return []Descriptor{b.Desc}, nil
}

func (b *FakeBackend) Open(d Descriptor) (Driver, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.present {
		return nil, ErrDisconnected
	}
	if b.OpenErr != nil {
		return nil, b.OpenErr
	}
	drv := NewFakeDriver(b.Serial)
	drv.NoDrive = b.NoDrive
	b.drivers = append(b.drivers, drv)
	return drv, nil
}

// Plug attaches or detaches the fake camera.
func (b *FakeBackend) Plug(present bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.present = present
	if !present {
		for _, d := range b.drivers {
			d.Unplug()
		}
		b.drivers = nil
	}
}

// Last returns the most recently opened driver.
func (b *FakeBackend) Last() *FakeDriver {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.drivers) == 0 {
		return nil
	}
	return b.drivers[len(b.drivers)-1]
}

// FakeDriver simulates a Link 2 in memory.
type FakeDriver struct {
	mu       sync.Mutex
	values   map[string]int
	xu       map[[2]uint8][]byte
	gone     bool
	Writes   []string // log of keys written, for assertions
	FailKeys map[string]error
	// NoDrive disables the simulated speed control (XU 9/0x16).
	NoDrive bool

	// simulated gimbal: speeds in deg/s, integrated on access
	panVel, tiltVel float64
	panPos, tiltPos float64
	movedAt         time.Time
	DriveLog        [][4]byte
	modeWrites      [][2]byte
}

// NewFakeDriver returns a driver initialised to the default values.
func NewFakeDriver(serial string) *FakeDriver {
	d := &FakeDriver{values: map[string]int{}, xu: map[[2]uint8][]byte{}, FailKeys: map[string]error{}}
	for _, s := range DefaultSpecs() {
		if !s.Vendor {
			d.values[s.Key] = s.Default
		}
	}
	info := make([]byte, 234)
	copy(info, serial)
	copy(info[80:], "v3.3.0.8_build3")
	d.xu[[2]uint8{XUFeatureUnit, XUSelDeviceInfo}] = info
	d.xu[[2]uint8{XUFeatureUnit, XUSelFeatureMask}] = []byte{0xC0, 0x02}
	d.xu[[2]uint8{XUFeatureUnit, XUSelExposureComp}] = []byte{0, 0}
	d.xu[[2]uint8{XUFeatureUnit, XUSelPanTiltRelative}] = []byte{0, 1, 0, 1}
	d.xu[[2]uint8{XUFeatureUnit, XUSelVideoMode}] = make([]byte, VideoModeLen)
	d.xu[[2]uint8{XUFeatureUnit, XUSelTrackSpeed}] = []byte{1}
	d.xu[[2]uint8{XUFeatureUnit, XUSelFraming}] = []byte{2}
	d.xu[[2]uint8{XUFeatureUnit, XUSelPanTiltAbsolute}] = make([]byte, 8)
	d.movedAt = time.Now()
	return d
}

// integrate advances the simulated gimbal. Caller holds d.mu.
func (d *FakeDriver) integrate() {
	now := time.Now()
	dt := now.Sub(d.movedAt).Seconds()
	d.movedAt = now
	if d.panVel == 0 && d.tiltVel == 0 {
		return
	}
	clamp := func(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }
	d.panPos = clamp(d.panPos+d.panVel*dt, -145, 145)
	d.tiltPos = clamp(d.tiltPos+d.tiltVel*dt, -90, 100)
	d.values[Pan] = int(math.Round(d.panPos))
	d.values[Tilt] = int(math.Round(d.tiltPos))
}

// Driving reports the simulated gimbal speeds (deg/s).
func (d *FakeDriver) Driving() (pan, tilt float64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.panVel, d.tiltVel
}

func (d *FakeDriver) check(key string) error {
	if d.gone {
		return ErrDisconnected
	}
	if err := d.FailKeys[key]; err != nil {
		return err
	}
	return nil
}

func (d *FakeDriver) StdRange(key string) (Range, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.check(key); err != nil {
		return Range{}, err
	}
	for _, s := range DefaultSpecs() {
		if s.Key == key {
			return Range{Min: s.Min, Max: s.Max, Step: s.Step, Default: s.Default}, nil
		}
	}
	return Range{}, ErrUnsupported
}

func (d *FakeDriver) StdGet(key string) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.check(key); err != nil {
		return 0, err
	}
	d.integrate()
	v, ok := d.values[key]
	if !ok {
		return 0, ErrUnsupported
	}
	return v, nil
}

func (d *FakeDriver) StdSet(key string, value int) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.check(key); err != nil {
		return err
	}
	if _, ok := d.values[key]; !ok {
		return ErrUnsupported
	}
	d.integrate()
	d.values[key] = value
	switch key {
	case Pan:
		d.panPos = float64(value)
	case Tilt:
		d.tiltPos = float64(value)
	case WhiteBalanceTemp:
		d.values[WhiteBalanceAuto] = 0
	case Focus:
		d.values[FocusAuto] = 0
	}
	d.Writes = append(d.Writes, key)
	return nil
}

// Poke changes a value behind the service's back (simulates another app).
func (d *FakeDriver) Poke(key string, value int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.values[key] = value
}

func (d *FakeDriver) XUGet(unit, selector uint8) ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.gone {
		return nil, ErrDisconnected
	}
	if unit == XUFeatureUnit && selector == XUSelPanTiltRelative && d.NoDrive {
		return nil, fmt.Errorf("xu %d/0x%02x: %w", unit, selector, ErrUnsupported)
	}
	if unit == XUFeatureUnit && selector == XUSelPanTiltAbsolute {
		d.integrate()
		out := binary.LittleEndian.AppendUint32(nil, uint32(int32(math.Round(d.tiltPos*3600))))
		return binary.LittleEndian.AppendUint32(out, uint32(int32(math.Round(d.panPos*3600)))), nil
	}
	v, ok := d.xu[[2]uint8{unit, selector}]
	if !ok {
		return nil, fmt.Errorf("xu %d/0x%02x: %w", unit, selector, ErrUnsupported)
	}
	return append([]byte(nil), v...), nil
}

func (d *FakeDriver) XUSet(unit, selector uint8, data []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.gone {
		return ErrDisconnected
	}
	if unit == XUFeatureUnit && selector == XUSelPanTiltRelative && !d.NoDrive && len(data) == 4 {
		d.integrate()
		vel := func(dir, speed byte) float64 {
			switch dir {
			case 1:
				return float64(speed) * FakeDriveDegPerStep
			case 0xff:
				return -float64(speed) * FakeDriveDegPerStep
			}
			return 0
		}
		d.panVel = vel(data[0], data[1])
		d.tiltVel = -vel(data[2], data[3]) // firmware: tilt dir +1 moves down
		d.DriveLog = append(d.DriveLog, [4]byte(data))
	}
	if unit == XUFeatureUnit && selector == XUSelVideoMode && len(data) >= 2 {
		d.modeWrites = append(d.modeWrites, [2]byte{data[0], data[1]})
	}
	k := [2]uint8{unit, selector}
	if _, ok := d.xu[k]; !ok {
		return fmt.Errorf("xu %d/0x%02x: %w", unit, selector, ErrUnsupported)
	}
	d.xu[k] = append([]byte(nil), data...)
	d.Writes = append(d.Writes, fmt.Sprintf("xu:%d:%02x", unit, selector))
	return nil
}

// FeatureMask returns the simulated feature mask.
func (d *FakeDriver) FeatureMask() uint16 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return binary.LittleEndian.Uint16(d.xu[[2]uint8{XUFeatureUnit, XUSelFeatureMask}])
}

// VideoModeWrites returns the mode id/flag pairs written so far.
func (d *FakeDriver) VideoModeWrites() [][2]byte {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([][2]byte(nil), d.modeWrites...)
}

func (d *FakeDriver) Unplug() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.gone = true
}

func (d *FakeDriver) Close() error { return nil }

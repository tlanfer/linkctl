//go:build darwin

package iokit

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"runtime"
	"strconv"
	"sync"
	"unsafe"

	"github.com/tlanfer/linkctl/internal/camera"
	"github.com/tlanfer/linkctl/internal/camera/uvcdesc"
)

// Backend enumerates Insta360 cameras through IOKit.
type Backend struct {
	// Log, when set, receives diagnostic messages.
	Log func(format string, args ...any)
}

// New returns an IOKit backend.
func New() *Backend { return &Backend{} }

func (b *Backend) logf(format string, args ...any) {
	if b.Log != nil {
		b.Log(format, args...)
	}
}

// USB device classes to match, newest first. Entries are de-duplicated by
// registry id.
var deviceClasses = []string{"IOUSBHostDevice", "IOUSBDevice"}

type usbService struct {
	entry     uint32 // io_service_t, owned
	id        uint64
	name      string
	vendorID  uint16
	productID uint16
}

// findServices returns all USB devices with the Insta360 vendor id. Callers
// must release entries via ioObjectRelease.
func findServices(logf func(string, ...any)) ([]usbService, error) {
	if err := load(); err != nil {
		return nil, err
	}
	seen := map[uint64]bool{}
	var out []usbService
	for _, class := range deviceClasses {
		var iter uint32
		if kr := ioServiceGetMatchingServices(0, ioServiceMatching(class), &iter); kr != 0 {
			logf("iokit: IOServiceGetMatchingServices(%s): %v", class, ioErr("match", uint32(kr)))
			continue
		}
		for {
			e := ioIteratorNext(iter)
			if e == 0 {
				break
			}
			vid, _ := registryInt(e, "idVendor")
			if uint16(vid) != camera.VendorID {
				ioObjectRelease(e)
				continue
			}
			var id uint64
			ioRegistryEntryGetRegistryEntryID(e, &id)
			if seen[id] {
				ioObjectRelease(e)
				continue
			}
			seen[id] = true
			pid, _ := registryInt(e, "idProduct")
			name := registryString(e, "kUSBProductString")
			if name == "" {
				name = registryString(e, "USB Product Name")
			}
			logf("iokit: %s id=0x%x vid=0x%04x pid=0x%04x name=%q", class, id, vid, pid, name)
			out = append(out, usbService{entry: e, id: id, name: name, vendorID: uint16(vid), productID: uint16(pid)})
		}
		ioObjectRelease(iter)
	}
	return out, nil
}

// List returns attached Insta360 cameras.
func (b *Backend) List() ([]camera.Descriptor, error) {
	svcs, err := findServices(b.logf)
	if err != nil {
		return nil, err
	}
	out := make([]camera.Descriptor, 0, len(svcs))
	for _, s := range svcs {
		ioObjectRelease(s.entry)
		name := s.name
		if name == "" {
			name = camera.Descriptor{ProductID: s.productID}.Model()
		}
		out = append(out, camera.Descriptor{
			Path: strconv.FormatUint(s.id, 16), Name: name, VendorID: s.vendorID, ProductID: s.productID,
		})
	}
	return out, nil
}

// Open creates the IOUSBDeviceInterface for d without opening the device.
func (b *Backend) Open(d camera.Descriptor) (camera.Driver, error) {
	if err := load(); err != nil {
		return nil, err
	}
	id, err := strconv.ParseUint(d.Path, 16, 64)
	if err != nil {
		return nil, fmt.Errorf("iokit: bad device path %q: %w", d.Path, err)
	}
	dev, err := openDeviceInterface(id, b.logf)
	if err != nil {
		return nil, err
	}
	drv := &driver{dev: dev, xuLen: map[[2]uint8]int{}, logf: b.logf}
	if err := drv.loadTopology(); err != nil {
		dev.release()
		return nil, err
	}
	return drv, nil
}

func openDeviceInterface(id uint64, logf func(string, ...any)) (*comHolder, error) {
	svc := ioServiceGetMatchingService(0, ioRegistryEntryIDMatching(id))
	if svc == 0 {
		return nil, fmt.Errorf("iokit: device 0x%x not found", id)
	}
	defer ioObjectRelease(svc)

	pluginType := cfUUID(uuidUSBDeviceUserClientType)
	defer cfRelease(pluginType)
	ifaceType := cfUUID(uuidCFPlugInInterface)
	defer cfRelease(ifaceType)

	var plugin *comHolder
	var score int32
	if kr := ioCreatePlugInInterfaceForService(svc, pluginType, ifaceType, &plugin, &score); kr != 0 || plugin == nil {
		return nil, ioErr("IOCreatePlugInInterfaceForService", uint32(kr))
	}
	defer plugin.release()
	logf("iokit: plug-in interface created (score %d)", score)

	dev, hr := plugin.queryInterface(uuidBytesUSBDeviceInterface)
	if hr != 0 || dev == nil {
		if hr == 0 {
			hr = 0xe00002bc
		}
		return nil, ioErr("QueryInterface(IOUSBDeviceInterface)", hr)
	}
	logf("iokit: IOUSBDeviceInterface acquired")
	return dev, nil
}

// IOUSBDeviceStruct100 vtable slots.
const (
	slotGetConfigurationDescriptorPtr = 21
	slotDeviceRequest                 = 26
)

// devRequest mirrors IOUSBDevRequest on 64-bit targets.
type devRequest struct {
	bmRequestType uint8
	bRequest      uint8
	wValue        uint16
	wIndex        uint16
	wLength       uint16
	pData         unsafe.Pointer
	wLenDone      uint32
	_             uint32
}

// UVC request codes.
const (
	reqTypeSet = 0x21 // host-to-device, class, interface
	reqTypeGet = 0xA1 // device-to-host, class, interface

	setCur = 0x01
	getCur = 0x81
	getMin = 0x82
	getMax = 0x83
	getRes = 0x84
	getLen = 0x85
	getDef = 0x87
)

var requestNames = map[uint8]string{setCur: "SET_CUR", getCur: "GET_CUR", getMin: "GET_MIN", getMax: "GET_MAX", getRes: "GET_RES", getLen: "GET_LEN", getDef: "GET_DEF"}

type driver struct {
	mu     sync.Mutex
	dev    *comHolder
	closed bool
	top    uvcdesc.Topology
	xuUnit map[uint8]uint8 // canonical unit id (camera.XU*) -> device unit id
	xuLen  map[[2]uint8]int
	logf   func(string, ...any)
}

var errClosed = errors.New("camera closed")

// deviceRequest performs one control transfer. Caller holds d.mu.
func (d *driver) deviceRequest(reqType, req uint8, value, index uint16, buf []byte) (int, error) {
	if d.closed {
		return 0, errClosed
	}
	r := &devRequest{bmRequestType: reqType, bRequest: req, wValue: value, wIndex: index, wLength: uint16(len(buf))}
	var pin runtime.Pinner
	defer pin.Unpin()
	pin.Pin(r)
	if len(buf) > 0 {
		pin.Pin(&buf[0])
		r.pData = unsafe.Pointer(&buf[0])
	}
	kr := uint32(d.dev.call(slotDeviceRequest, uintptr(unsafe.Pointer(r))))
	if kr != 0 {
		name := requestNames[req]
		if name == "" {
			name = fmt.Sprintf("req 0x%02x", req)
		}
		return int(r.wLenDone), ioErr(fmt.Sprintf("DeviceRequest %s sel=0x%02x unit=%d", name, value>>8, index>>8), kr)
	}
	return int(r.wLenDone), nil
}

func (d *driver) uvc(req, unit, selector uint8, buf []byte) (int, error) {
	reqType := uint8(reqTypeGet)
	if req == setCur {
		reqType = reqTypeSet
	}
	return d.deviceRequest(reqType, req, uint16(selector)<<8, uint16(unit)<<8|uint16(d.top.VCInterface), buf)
}

// loadTopology reads the configuration descriptor and resolves unit ids.
func (d *driver) loadTopology() error {
	cfg, err := d.configDescriptor()
	if err != nil {
		return err
	}
	top, err := uvcdesc.Parse(cfg)
	if err != nil {
		return err
	}
	d.top = top
	d.xuUnit = map[uint8]uint8{camera.XUFeatureUnit: camera.XUFeatureUnit, camera.XUPrivacyUnit: camera.XUPrivacyUnit}
	if id, ok := top.UnitForGUID(camera.XUFeatureUnitGUID); ok {
		d.xuUnit[camera.XUFeatureUnit] = id
	}
	if id, ok := top.UnitForGUID(camera.XUPrivacyUnitGUID); ok {
		d.xuUnit[camera.XUPrivacyUnit] = id
	}
	d.logf("iokit: topology VC interface=%d camera terminal=%d processing unit=%d XUs=%+v",
		top.VCInterface, top.CameraTerminalID, top.ProcessingUnitID, top.ExtensionUnits)
	if top.CameraTerminalID == 0 || top.ProcessingUnitID == 0 {
		return fmt.Errorf("iokit: incomplete UVC topology %+v", top)
	}
	return nil
}

// configDescriptor returns the active configuration descriptor, preferring the
// cached copy and falling back to a standard GET_DESCRIPTOR request.
func (d *driver) configDescriptor() ([]byte, error) {
	var p unsafe.Pointer
	kr := uint32(d.dev.call(slotGetConfigurationDescriptorPtr, 0, uintptr(unsafe.Pointer(&p))))
	if kr == 0 && p != nil {
		hdr := unsafe.Slice((*byte)(p), 4)
		total := int(binary.LittleEndian.Uint16(hdr[2:4]))
		if total >= 9 {
			return append([]byte(nil), unsafe.Slice((*byte)(p), total)...), nil
		}
	}
	d.logf("iokit: GetConfigurationDescriptorPtr: %v; falling back to GET_DESCRIPTOR", ioErr("GetConfigurationDescriptorPtr", kr))
	head := make([]byte, 9)
	if _, err := d.deviceRequest(0x80, 0x06, 0x0200, 0, head); err != nil {
		return nil, err
	}
	total := int(binary.LittleEndian.Uint16(head[2:4]))
	if total < 9 {
		return nil, fmt.Errorf("iokit: bad configuration descriptor length %d", total)
	}
	full := make([]byte, total)
	n, err := d.deviceRequest(0x80, 0x06, 0x0200, 0, full)
	if err != nil {
		return nil, err
	}
	return full[:n], nil
}

// Standard control mapping.
type ctrl struct {
	ct       bool // camera terminal, else processing unit
	selector uint8
	size     int
	signed   bool
	autoSel  uint8 // auto control that must be switched off before writing
}

const (
	ctFocusAbs   = 0x06
	ctFocusAuto  = 0x08
	ctZoomAbs    = 0x0B
	ctPanTiltAbs = 0x0D

	puBrightness = 0x02
	puContrast   = 0x03
	puPowerLine  = 0x05
	puHue        = 0x06
	puSaturation = 0x07
	puSharpness  = 0x08
	puWBTemp     = 0x0A
	puWBTempAuto = 0x0B
)

var ctrls = map[string]ctrl{
	camera.Brightness:       {selector: puBrightness, size: 2, signed: true},
	camera.Contrast:         {selector: puContrast, size: 2},
	camera.Hue:              {selector: puHue, size: 2, signed: true},
	camera.Saturation:       {selector: puSaturation, size: 2},
	camera.Sharpness:        {selector: puSharpness, size: 2},
	camera.AntiFlicker:      {selector: puPowerLine, size: 1},
	camera.WhiteBalanceTemp: {selector: puWBTemp, size: 2, autoSel: puWBTempAuto},
	camera.WhiteBalanceAuto: {selector: puWBTempAuto, size: 1},
	camera.Focus:            {ct: true, selector: ctFocusAbs, size: 2, autoSel: ctFocusAuto},
	camera.FocusAuto:        {ct: true, selector: ctFocusAuto, size: 1},
	camera.Zoom:             {ct: true, selector: ctZoomAbs, size: 2},
	// pan/tilt share CT_PANTILT_ABSOLUTE_CONTROL; handled specially.
}

func (d *driver) unitFor(c ctrl) uint8 {
	if c.ct {
		return d.top.CameraTerminalID
	}
	return d.top.ProcessingUnitID
}

func decode(buf []byte, size int, signed bool) int {
	switch size {
	case 1:
		if signed {
			return int(int8(buf[0]))
		}
		return int(buf[0])
	case 2:
		v := binary.LittleEndian.Uint16(buf)
		if signed {
			return int(int16(v))
		}
		return int(v)
	default:
		v := binary.LittleEndian.Uint32(buf)
		if signed {
			return int(int32(v))
		}
		return int(v)
	}
}

func encode(v, size int) []byte {
	buf := make([]byte, size)
	switch size {
	case 1:
		buf[0] = byte(v)
	case 2:
		binary.LittleEndian.PutUint16(buf, uint16(v))
	default:
		binary.LittleEndian.PutUint32(buf, uint32(v))
	}
	return buf
}

func (d *driver) readCtrl(req uint8, c ctrl) (int, error) {
	buf := make([]byte, c.size)
	n, err := d.uvc(req, d.unitFor(c), c.selector, buf)
	if err != nil {
		return 0, err
	}
	if n < c.size {
		return 0, fmt.Errorf("iokit: short read %d/%d bytes for selector 0x%02x", n, c.size, c.selector)
	}
	return decode(buf, c.size, c.signed), nil
}

// panTilt reads CT_PANTILT_ABSOLUTE (arc-seconds).
func (d *driver) panTilt(req uint8) (pan, tilt int32, err error) {
	buf := make([]byte, 8)
	n, err := d.uvc(req, d.top.CameraTerminalID, ctPanTiltAbs, buf)
	if err != nil {
		return 0, 0, err
	}
	if n < 8 {
		return 0, 0, fmt.Errorf("iokit: short pan/tilt read %d/8 bytes", n)
	}
	return int32(binary.LittleEndian.Uint32(buf[0:4])), int32(binary.LittleEndian.Uint32(buf[4:8])), nil
}

func arcsecToDeg(v int32) int { return int(math.Round(float64(v) / 3600)) }

func pickPanTilt(key string, pan, tilt int32) int32 {
	if key == camera.Pan {
		return pan
	}
	return tilt
}

func (d *driver) StdRange(key string) (camera.Range, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if key == camera.Pan || key == camera.Tilt {
		var vals [4]int32
		for i, req := range []uint8{getMin, getMax, getRes, getDef} {
			p, t, err := d.panTilt(req)
			if err != nil {
				return camera.Range{}, err
			}
			vals[i] = pickPanTilt(key, p, t)
		}
		return camera.Range{
			Min: arcsecToDeg(vals[0]), Max: arcsecToDeg(vals[1]),
			Step: max(1, arcsecToDeg(vals[2])), Default: arcsecToDeg(vals[3]),
		}, nil
	}
	c, ok := ctrls[key]
	if !ok || key == camera.WhiteBalanceAuto || key == camera.FocusAuto {
		return camera.Range{}, camera.ErrUnsupported
	}
	var r [4]int
	for i, req := range []uint8{getMin, getMax, getRes, getDef} {
		v, err := d.readCtrl(req, c)
		if err != nil {
			return camera.Range{}, err
		}
		r[i] = v
	}
	return camera.Range{Min: r[0], Max: r[1], Step: r[2], Default: r[3]}, nil
}

func (d *driver) StdGet(key string) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if key == camera.Pan || key == camera.Tilt {
		p, t, err := d.panTilt(getCur)
		if err != nil {
			return 0, err
		}
		return arcsecToDeg(pickPanTilt(key, p, t)), nil
	}
	c, ok := ctrls[key]
	if !ok {
		return 0, camera.ErrUnsupported
	}
	v, err := d.readCtrl(getCur, c)
	if err != nil {
		return 0, err
	}
	if (key == camera.WhiteBalanceAuto || key == camera.FocusAuto) && v != 0 {
		v = 1
	}
	return v, nil
}

func (d *driver) StdSet(key string, value int) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if key == camera.Pan || key == camera.Tilt {
		p, t, err := d.panTilt(getCur)
		if err != nil {
			return err
		}
		if key == camera.Pan {
			p = int32(value) * 3600
		} else {
			t = int32(value) * 3600
		}
		buf := make([]byte, 8)
		binary.LittleEndian.PutUint32(buf[0:4], uint32(p))
		binary.LittleEndian.PutUint32(buf[4:8], uint32(t))
		_, err = d.uvc(setCur, d.top.CameraTerminalID, ctPanTiltAbs, buf)
		return err
	}
	c, ok := ctrls[key]
	if !ok {
		return camera.ErrUnsupported
	}
	unit := d.unitFor(c)
	if c.autoSel != 0 {
		auto := make([]byte, 1)
		if _, err := d.uvc(getCur, unit, c.autoSel, auto); err == nil && auto[0] != 0 {
			if _, err := d.uvc(setCur, unit, c.autoSel, []byte{0}); err != nil {
				return fmt.Errorf("switching to manual: %w", err)
			}
		}
	}
	if key == camera.WhiteBalanceAuto || key == camera.FocusAuto {
		if value != 0 {
			value = 1
		}
	}
	_, err := d.uvc(setCur, unit, c.selector, encode(value, c.size))
	return err
}

func (d *driver) xuLength(unit, selector uint8) (int, error) {
	k := [2]uint8{unit, selector}
	if n, ok := d.xuLen[k]; ok {
		return n, nil
	}
	buf := make([]byte, 2)
	n, err := d.uvc(getLen, unit, selector, buf)
	if err != nil {
		return 0, err
	}
	if n < 2 {
		return 0, fmt.Errorf("iokit: GET_LEN returned %d bytes", n)
	}
	l := int(binary.LittleEndian.Uint16(buf))
	if l <= 0 {
		return 0, fmt.Errorf("iokit: XU %d/0x%02x reports length %d", unit, selector, l)
	}
	d.xuLen[k] = l
	return l, nil
}

func (d *driver) XUGet(unit, selector uint8) ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	u := d.xuUnit[unit]
	if u == 0 {
		u = unit
	}
	l, err := d.xuLength(u, selector)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, l)
	n, err := d.uvc(getCur, u, selector, buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

func (d *driver) XUSet(unit, selector uint8, data []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	u := d.xuUnit[unit]
	if u == 0 {
		u = unit
	}
	l, err := d.xuLength(u, selector)
	if err != nil {
		return err
	}
	buf := make([]byte, l)
	copy(buf, data)
	_, err = d.uvc(setCur, u, selector, buf)
	return err
}

func (d *driver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.closed {
		d.closed = true
		d.dev.release()
	}
	return nil
}

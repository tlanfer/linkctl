//go:build windows

package dshow

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sys/windows"

	"github.com/tlanfer/linkctl/internal/camera"
)

// Backend enumerates Insta360 cameras through DirectShow.
type Backend struct {
	once sync.Once
	th   *comThread
	err  error
	// Log, when set, receives diagnostic messages.
	Log func(format string, args ...any)
}

// New returns a DirectShow backend.
func New() *Backend { return &Backend{} }

func (b *Backend) logf(format string, args ...any) {
	if b.Log != nil {
		b.Log(format, args...)
	}
}

func (b *Backend) thread() (*comThread, error) {
	b.once.Do(func() { b.th, b.err = newCOMThread() })
	return b.th, b.err
}

var reVidPid = regexp.MustCompile(`(?i)vid_([0-9a-f]{4})&pid_([0-9a-f]{4})`)

func parseVidPid(path string) (vid, pid uint16, ok bool) {
	m := reVidPid.FindStringSubmatch(path)
	if m == nil {
		return 0, 0, false
	}
	v, _ := strconv.ParseUint(m[1], 16, 16)
	p, _ := strconv.ParseUint(m[2], 16, 16)
	return uint16(v), uint16(p), true
}

// List returns attached Insta360 cameras.
func (b *Backend) List() ([]camera.Descriptor, error) {
	th, err := b.thread()
	if err != nil {
		return nil, err
	}
	var out []camera.Descriptor
	err = th.do(func() error {
		ms, err := enumerateMonikers()
		if err != nil {
			return err
		}
		for _, m := range ms {
			vid, pid, ok := parseVidPid(m.DevicePath)
			b.logf("dshow: found %q path=%q", m.FriendlyName, m.DevicePath)
			m.obj.Release()
			if !ok || vid != camera.VendorID {
				continue
			}
			out = append(out, camera.Descriptor{Path: m.DevicePath, Name: m.FriendlyName, VendorID: vid, ProductID: pid})
		}
		return nil
	})
	return out, err
}

// Open binds the capture filter for d.
func (b *Backend) Open(d camera.Descriptor) (camera.Driver, error) {
	th, err := b.thread()
	if err != nil {
		return nil, err
	}
	drv := &driver{th: th, xuLen: map[[2]uint8]int{}, xuNode: map[uint8]uint32{}, logf: b.logf}
	err = th.do(func() error {
		ms, err := enumerateMonikers()
		if err != nil {
			return err
		}
		var target comObject
		for _, m := range ms {
			if target == nil && strings.EqualFold(m.DevicePath, d.Path) {
				target = m.obj
				continue
			}
			m.obj.Release()
		}
		if target == nil {
			return fmt.Errorf("device %q not found", d.Path)
		}
		defer target.Release()
		f, err := bindFilter(target)
		if err != nil {
			return err
		}
		drv.f = f
		drv.mapExtensionUnits()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return drv, nil
}

type stdProp struct {
	camCtrl bool // IAMCameraControl, else IAMVideoProcAmp
	id      int32
	autoOf  bool // this key is the Auto flag of the property
}

var stdProps = map[string]stdProp{
	camera.Brightness:       {id: 0},
	camera.Contrast:         {id: 1},
	camera.Hue:              {id: 2},
	camera.Saturation:       {id: 3},
	camera.Sharpness:        {id: 4},
	camera.WhiteBalanceTemp: {id: 7},
	camera.WhiteBalanceAuto: {id: 7, autoOf: true},
	camera.AntiFlicker:      {id: 13},
	camera.Pan:              {camCtrl: true, id: 0},
	camera.Tilt:             {camCtrl: true, id: 1},
	camera.Zoom:             {camCtrl: true, id: 3},
	camera.Focus:            {camCtrl: true, id: 6},
	camera.FocusAuto:        {camCtrl: true, id: 6, autoOf: true},
}

var unitGUIDs = map[uint8]string{
	camera.XUFeatureUnit: camera.XUFeatureUnitGUID,
	camera.XUPrivacyUnit: camera.XUPrivacyUnitGUID,
}

type driver struct {
	th     *comThread
	f      *filter
	closed bool
	xuLen  map[[2]uint8]int
	xuNode map[uint8]uint32
	xuSet  map[uint8]windows.GUID
	logf   func(string, ...any)
}

var errClosed = errors.New("camera closed")

func (d *driver) iface(p stdProp) comObject {
	if p.camCtrl {
		return d.f.camCtrl
	}
	return d.f.procAmp
}

func (d *driver) StdRange(key string) (camera.Range, error) {
	p, ok := stdProps[key]
	if !ok || p.autoOf {
		return camera.Range{}, camera.ErrUnsupported
	}
	var r rawRange
	err := d.th.do(func() error {
		if d.closed {
			return errClosed
		}
		var err error
		r, err = ifaceGetRange(d.iface(p), p.id)
		return err
	})
	return camera.Range{Min: int(r.Min), Max: int(r.Max), Step: int(r.Step), Default: int(r.Default)}, err
}

func (d *driver) StdGet(key string) (int, error) {
	p, ok := stdProps[key]
	if !ok {
		return 0, camera.ErrUnsupported
	}
	var out int
	err := d.th.do(func() error {
		if d.closed {
			return errClosed
		}
		v, flags, err := ifaceGet(d.iface(p), p.id)
		if err != nil {
			return err
		}
		if p.autoOf {
			if flags&flagsAuto != 0 {
				out = 1
			}
		} else {
			out = int(v)
		}
		return nil
	})
	return out, err
}

func (d *driver) StdSet(key string, value int) error {
	p, ok := stdProps[key]
	if !ok {
		return camera.ErrUnsupported
	}
	return d.th.do(func() error {
		if d.closed {
			return errClosed
		}
		o := d.iface(p)
		if p.autoOf {
			cur, _, err := ifaceGet(o, p.id)
			if err != nil {
				return err
			}
			flags := int32(flagsManual)
			if value != 0 {
				flags = flagsAuto
			}
			return ifaceSet(o, p.id, cur, flags)
		}
		err := ifaceSet(o, p.id, int32(value), flagsManual)
		if err != nil && key == camera.AntiFlicker {
			// Power line frequency has no auto/manual capability flags.
			err = ifaceSet(o, p.id, int32(value), 0)
		}
		return err
	})
}

// mapExtensionUnits finds topology nodes for the known XU GUIDs. Runs on the COM thread.
func (d *driver) mapExtensionUnits() {
	d.xuSet = map[uint8]windows.GUID{}
	nodes, err := d.f.devSpecificNodes()
	if err != nil {
		d.logf("dshow: topology: %v", err)
		return
	}
	for _, n := range nodes {
		sets, err := d.f.nodePropertySets(n)
		if err != nil {
			d.logf("dshow: node %d property sets: %v", n, err)
			continue
		}
		for _, s := range sets {
			for unit, g := range unitGUIDs {
				want, _ := windows.GUIDFromString("{" + g + "}")
				if s == want {
					d.xuNode[unit] = n
					d.xuSet[unit] = s
					d.logf("dshow: XU unit %d -> node %d", unit, n)
				}
			}
		}
	}
}

const hrMoreData = 0x800700EA

func (d *driver) xuProp(unit, selector uint8, flags uint32) (*ksPNode, error) {
	node, ok := d.xuNode[unit]
	if !ok {
		return nil, fmt.Errorf("extension unit %d not found", unit)
	}
	return &ksPNode{Set: d.xuSet[unit], ID: uint32(selector), Flags: flags | ksPropertyTypeTopology, NodeID: node}, nil
}

func (d *driver) xuLength(unit, selector uint8) (int, error) {
	k := [2]uint8{unit, selector}
	if n, ok := d.xuLen[k]; ok {
		return n, nil
	}
	p, err := d.xuProp(unit, selector, ksPropertyTypeGet)
	if err != nil {
		return 0, err
	}
	n, err := d.f.ksProperty(p, nil)
	var he *hrError
	if err != nil && !(errors.As(err, &he) && he.hr == hrMoreData) {
		return 0, err
	}
	if n <= 0 {
		return 0, fmt.Errorf("xu %d/0x%02x: driver reported no length", unit, selector)
	}
	d.xuLen[k] = n
	return n, nil
}

func (d *driver) XUGet(unit, selector uint8) ([]byte, error) {
	var out []byte
	err := d.th.do(func() error {
		if d.closed {
			return errClosed
		}
		n, err := d.xuLength(unit, selector)
		if err != nil {
			return err
		}
		p, _ := d.xuProp(unit, selector, ksPropertyTypeGet)
		buf := make([]byte, n)
		got, err := d.f.ksProperty(p, buf)
		if err != nil {
			return err
		}
		out = buf[:got]
		return nil
	})
	return out, err
}

func (d *driver) XUSet(unit, selector uint8, data []byte) error {
	return d.th.do(func() error {
		if d.closed {
			return errClosed
		}
		n, err := d.xuLength(unit, selector)
		if err != nil {
			return err
		}
		buf := make([]byte, n)
		copy(buf, data)
		p, _ := d.xuProp(unit, selector, ksPropertyTypeSet)
		_, err = d.f.ksProperty(p, buf)
		return err
	})
}

func (d *driver) Close() error {
	return d.th.do(func() error {
		if !d.closed {
			d.closed = true
			d.f.release()
		}
		return nil
	})
}

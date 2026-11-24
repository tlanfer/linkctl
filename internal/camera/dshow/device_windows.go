//go:build windows

package dshow

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// moniker describes one DirectShow video input device.
type moniker struct {
	FriendlyName string
	DevicePath   string
	obj          comObject // IMoniker, owned
}

// enumerateMonikers lists video input devices. Caller must release the monikers.
func enumerateMonikers() ([]moniker, error) {
	devEnum, err := coCreateInstance(&clsidSystemDeviceEnum, &iidICreateDevEnum)
	if err != nil {
		return nil, err
	}
	defer devEnum.Release()

	var enum comObject
	hr := devEnum.call(3, uintptr(unsafe.Pointer(&clsidVideoInputDeviceCategory)), uintptr(unsafe.Pointer(&enum)), 0)
	if hr == sFalse || enum == nil {
		return nil, nil // category empty
	}
	if hr != sOK {
		return nil, hresultError("CreateClassEnumerator", hr)
	}
	defer enum.Release()

	var out []moniker
	for {
		var m comObject
		var fetched uint32
		hr := enum.call(3, 1, uintptr(unsafe.Pointer(&m)), uintptr(unsafe.Pointer(&fetched)))
		if hr != sOK || fetched == 0 {
			break
		}
		name, path := readMonikerProps(m)
		out = append(out, moniker{FriendlyName: name, DevicePath: path, obj: m})
	}
	return out, nil
}

func readMonikerProps(m comObject) (name, path string) {
	var bag comObject
	// IMoniker::BindToStorage is vtable slot 9.
	hr := m.call(9, 0, 0, uintptr(unsafe.Pointer(&iidIPropertyBag)), uintptr(unsafe.Pointer(&bag)))
	if hr != sOK {
		return "", ""
	}
	defer bag.Release()
	read := func(prop string) string {
		p, _ := windows.UTF16PtrFromString(prop)
		var v variant
		if bag.call(3, uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&v)), 0) != sOK {
			return ""
		}
		defer v.clear()
		if v.vt != vtBSTR {
			return ""
		}
		return bstrToString(v.val)
	}
	return read("FriendlyName"), read("DevicePath")
}

// filter holds the control interfaces of a bound capture filter.
type filter struct {
	base     comObject
	procAmp  comObject
	camCtrl  comObject
	ksCtrl   comObject
	topology comObject
}

func bindFilter(m comObject) (*filter, error) {
	var base comObject
	// IMoniker::BindToObject is vtable slot 8.
	hr := m.call(8, 0, 0, uintptr(unsafe.Pointer(&iidIBaseFilter)), uintptr(unsafe.Pointer(&base)))
	if hr != sOK {
		return nil, hresultError("BindToObject", hr)
	}
	f := &filter{base: base}
	f.procAmp, _ = base.QueryInterface(&iidIAMVideoProcAmp)
	f.camCtrl, _ = base.QueryInterface(&iidIAMCameraControl)
	f.ksCtrl, _ = base.QueryInterface(&iidIKsControl)
	f.topology, _ = base.QueryInterface(&iidIKsTopologyInfo)
	if f.procAmp == nil && f.camCtrl == nil {
		f.release()
		return nil, errors.New("device exposes neither IAMVideoProcAmp nor IAMCameraControl")
	}
	return f, nil
}

func (f *filter) release() {
	for _, o := range []comObject{f.topology, f.ksCtrl, f.camCtrl, f.procAmp, f.base} {
		o.Release()
	}
}

// Range is a control range as reported by the driver.
type rawRange struct {
	Min, Max, Step, Default int32
	Caps                    int32
}

// Flags for IAMVideoProcAmp / IAMCameraControl.
const (
	flagsAuto   = 0x1
	flagsManual = 0x2
)

// Both IAMVideoProcAmp and IAMCameraControl share the vtable layout
// GetRange(3), Set(4), Get(5).
func ifaceGetRange(o comObject, prop int32) (rawRange, error) {
	if o == nil {
		return rawRange{}, errors.New("interface unavailable")
	}
	var r rawRange
	hr := o.call(3, uintptr(prop), uintptr(unsafe.Pointer(&r.Min)), uintptr(unsafe.Pointer(&r.Max)),
		uintptr(unsafe.Pointer(&r.Step)), uintptr(unsafe.Pointer(&r.Default)), uintptr(unsafe.Pointer(&r.Caps)))
	if hr != sOK {
		return r, hresultError(fmt.Sprintf("GetRange(%d)", prop), hr)
	}
	return r, nil
}

func ifaceGet(o comObject, prop int32) (value, flags int32, err error) {
	if o == nil {
		return 0, 0, errors.New("interface unavailable")
	}
	hr := o.call(5, uintptr(prop), uintptr(unsafe.Pointer(&value)), uintptr(unsafe.Pointer(&flags)))
	if hr != sOK {
		return 0, 0, hresultError(fmt.Sprintf("Get(%d)", prop), hr)
	}
	return value, flags, nil
}

func ifaceSet(o comObject, prop, value, flags int32) error {
	if o == nil {
		return errors.New("interface unavailable")
	}
	hr := o.call(4, uintptr(prop), uintptr(value), uintptr(flags))
	if hr != sOK {
		return hresultError(fmt.Sprintf("Set(%d)", prop), hr)
	}
	return nil
}

// KS property plumbing for extension units.
const (
	ksPropertyTypeGet          = 0x00000001
	ksPropertyTypeSet          = 0x00000002
	ksPropertyTypeSetSupport   = 0x00000100
	ksPropertyTypeBasicSupport = 0x00000200
	ksPropertyTypeTopology     = 0x10000000
)

type ksPNode struct {
	Set      windows.GUID
	ID       uint32
	Flags    uint32
	NodeID   uint32
	Reserved uint32
}

func (f *filter) ksProperty(prop *ksPNode, data []byte) (int, error) {
	if f.ksCtrl == nil {
		return 0, errors.New("IKsControl unavailable")
	}
	var returned uint32
	var dp uintptr
	if len(data) > 0 {
		dp = uintptr(unsafe.Pointer(&data[0]))
	}
	hr := f.ksCtrl.call(3, uintptr(unsafe.Pointer(prop)), unsafe.Sizeof(*prop), dp, uintptr(len(data)),
		uintptr(unsafe.Pointer(&returned)))
	if hr != sOK {
		return int(returned), hresultError(fmt.Sprintf("KsProperty(id=%d,node=%d,flags=0x%x)", prop.ID, prop.NodeID, prop.Flags), hr)
	}
	return int(returned), nil
}

// devSpecificNodes returns topology node ids of type KSNODETYPE_DEV_SPECIFIC
// (UVC extension units).
func (f *filter) devSpecificNodes() ([]uint32, error) {
	if f.topology == nil {
		return nil, errors.New("IKsTopologyInfo unavailable")
	}
	var n uint32
	if hr := f.topology.call(8, uintptr(unsafe.Pointer(&n))); hr != sOK {
		return nil, hresultError("get_NumNodes", hr)
	}
	var out []uint32
	for i := uint32(0); i < n; i++ {
		var g windows.GUID
		if f.topology.call(9, uintptr(i), uintptr(unsafe.Pointer(&g))) != sOK {
			continue
		}
		if g == ksnodetypeDevSpecific {
			out = append(out, i)
		}
	}
	return out, nil
}

// nodePropertySets asks a topology node which property sets it supports.
func (f *filter) nodePropertySets(node uint32) ([]windows.GUID, error) {
	prop := ksPNode{ID: 0, Flags: ksPropertyTypeSetSupport | ksPropertyTypeTopology, NodeID: node}
	// First call with no buffer to learn the size.
	n, err := f.ksProperty(&prop, nil)
	if n == 0 {
		n = 16 * 16
	}
	buf := make([]byte, n)
	n, err = f.ksProperty(&prop, buf)
	if err != nil {
		return nil, err
	}
	var out []windows.GUID
	for off := 0; off+16 <= n; off += 16 {
		out = append(out, *(*windows.GUID)(unsafe.Pointer(&buf[off])))
	}
	return out, nil
}

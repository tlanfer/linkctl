//go:build windows

// Package dshow implements camera control on Windows through DirectShow COM
// interfaces, called directly via syscall (no cgo).
package dshow

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modole32     = windows.NewLazySystemDLL("ole32.dll")
	modoleaut32  = windows.NewLazySystemDLL("oleaut32.dll")
	procCoCreate = modole32.NewProc("CoCreateInstance")
	procVarClear = modoleaut32.NewProc("VariantClear")
)

const (
	clsctxInprocServer = 0x1
	sOK                = 0
	sFalse             = 1
)

var (
	clsidSystemDeviceEnum         = windows.GUID{Data1: 0x62BE5D10, Data2: 0x60EB, Data3: 0x11d0, Data4: [8]byte{0xBD, 0x3B, 0x00, 0xA0, 0xC9, 0x11, 0xCE, 0x86}}
	iidICreateDevEnum             = windows.GUID{Data1: 0x29840822, Data2: 0x5B84, Data3: 0x11D0, Data4: [8]byte{0xBD, 0x3B, 0x00, 0xA0, 0xC9, 0x11, 0xCE, 0x86}}
	clsidVideoInputDeviceCategory = windows.GUID{Data1: 0x860BB310, Data2: 0x5D01, Data3: 0x11d0, Data4: [8]byte{0xBD, 0x3B, 0x00, 0xA0, 0xC9, 0x11, 0xCE, 0x86}}
	iidIPropertyBag               = windows.GUID{Data1: 0x55272A00, Data2: 0x42CB, Data3: 0x11CE, Data4: [8]byte{0x81, 0x35, 0x00, 0xAA, 0x00, 0x4B, 0xB8, 0x51}}
	iidIBaseFilter                = windows.GUID{Data1: 0x56a86895, Data2: 0x0ad4, Data3: 0x11ce, Data4: [8]byte{0xb0, 0x3a, 0x00, 0x20, 0xaf, 0x0b, 0xa7, 0x70}}
	iidIAMVideoProcAmp            = windows.GUID{Data1: 0xC6E13360, Data2: 0x30AC, Data3: 0x11d0, Data4: [8]byte{0xA1, 0x8C, 0x00, 0xA0, 0xC9, 0x11, 0x89, 0x56}}
	iidIAMCameraControl           = windows.GUID{Data1: 0xC6E13370, Data2: 0x30AC, Data3: 0x11d0, Data4: [8]byte{0xA1, 0x8C, 0x00, 0xA0, 0xC9, 0x11, 0x89, 0x56}}
	iidIKsControl                 = windows.GUID{Data1: 0x28F54685, Data2: 0x06FD, Data3: 0x11D2, Data4: [8]byte{0xB2, 0x7A, 0x00, 0xA0, 0xC9, 0x22, 0x31, 0x96}}
	iidIKsTopologyInfo            = windows.GUID{Data1: 0x720D4AC0, Data2: 0x7533, Data3: 0x11D0, Data4: [8]byte{0xA5, 0xD6, 0x28, 0xDB, 0x04, 0xC1, 0x00, 0x00}}
	ksnodetypeDevSpecific         = windows.GUID{Data1: 0x941C7AC0, Data2: 0xC559, Data3: 0x11D0, Data4: [8]byte{0x8A, 0x2B, 0x00, 0xA0, 0xC9, 0x25, 0x5A, 0xC1}}
	propsetidVidcapVideoProcAmp   = windows.GUID{Data1: 0xC6E13360, Data2: 0x30AC, Data3: 0x11d0, Data4: [8]byte{0xA1, 0x8C, 0x00, 0xA0, 0xC9, 0x11, 0x89, 0x56}}
	propsetidVidcapCameraControl  = windows.GUID{Data1: 0xC6E13370, Data2: 0x30AC, Data3: 0x11d0, Data4: [8]byte{0xA1, 0x8C, 0x00, 0xA0, 0xC9, 0x11, 0x89, 0x56}}
	_                             = propsetidVidcapCameraControl
)

// comHolder is the memory layout of every COM object: a vtable pointer first.
type comHolder struct {
	vtbl *[64]uintptr
}

// comObject is a COM interface pointer.
type comObject = *comHolder

func (o *comHolder) call(method int, args ...uintptr) uintptr {
	r, _, _ := syscall.SyscallN(o.vtbl[method], append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...)
	return r
}

func (o *comHolder) Release() {
	if o != nil {
		o.call(2)
	}
}

func (o *comHolder) QueryInterface(iid *windows.GUID) (comObject, error) {
	var out comObject
	hr := o.call(0, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))
	if hr != sOK {
		return nil, hresultError("QueryInterface", hr)
	}
	return out, nil
}

type hrError struct {
	op string
	hr uint32
}

func (e *hrError) Error() string { return fmt.Sprintf("%s failed: HRESULT 0x%08X", e.op, e.hr) }

func hresultError(op string, hr uintptr) error { return &hrError{op: op, hr: uint32(hr)} }

func coCreateInstance(clsid, iid *windows.GUID) (comObject, error) {
	var out comObject
	hr, _, _ := procCoCreate.Call(uintptr(unsafe.Pointer(clsid)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))
	if hr != sOK {
		return nil, hresultError("CoCreateInstance", hr)
	}
	return out, nil
}

// variant mirrors the Win32 VARIANT layout (24 bytes on 64-bit, 16 on 32-bit).
type variant struct {
	vt  uint16
	_   [3]uint16
	val unsafe.Pointer
	_   uintptr
}

const vtBSTR = 8

func (v *variant) clear() { procVarClear.Call(uintptr(unsafe.Pointer(v))) }

func bstrToString(p unsafe.Pointer) string {
	if p == nil {
		return ""
	}
	// BSTR length prefix (bytes) sits 4 bytes before the pointer.
	n := *(*uint32)(unsafe.Add(p, -4)) / 2
	return windows.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(p)), n))
}

// comThread runs all COM calls on one locked OS thread in the multithreaded
// apartment, so interface pointers never cross apartments.
type comThread struct {
	jobs chan func()
}

func newCOMThread() (*comThread, error) {
	t := &comThread{jobs: make(chan func())}
	ready := make(chan error)
	go func() {
		runtime.LockOSThread()
		if err := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED); err != nil {
			ready <- fmt.Errorf("CoInitializeEx: %w", err)
			return
		}
		ready <- nil
		for job := range t.jobs {
			job()
		}
		windows.CoUninitialize()
	}()
	if err := <-ready; err != nil {
		return nil, err
	}
	return t, nil
}

func (t *comThread) do(fn func() error) error {
	done := make(chan error, 1)
	t.jobs <- func() { done <- fn() }
	return <-done
}

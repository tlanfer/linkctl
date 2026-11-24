//go:build darwin

// Package iokit implements camera control on macOS by sending UVC class
// requests on the default control pipe through IOKit's IOUSBLib, called via
// purego (no cgo). The device is never opened exclusively, so the system
// camera stack (UVCAssistant) and other apps keep streaming.
package iokit

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// CoreFoundation / IOKit bindings, resolved lazily.
var (
	loadOnce sync.Once
	loadErr  error

	ioServiceMatching                 func(name string) uintptr
	ioServiceGetMatchingServices      func(mainPort uint32, matching uintptr, iter *uint32) int32
	ioServiceGetMatchingService       func(mainPort uint32, matching uintptr) uint32
	ioIteratorNext                    func(iter uint32) uint32
	ioObjectRelease                   func(obj uint32) int32
	ioRegistryEntryGetRegistryEntryID func(entry uint32, id *uint64) int32
	ioRegistryEntryIDMatching         func(id uint64) uintptr
	ioRegistryEntryCreateCFProperty   func(entry uint32, key uintptr, alloc uintptr, options uint32) uintptr
	ioCreatePlugInInterfaceForService func(service uint32, pluginType, interfaceType uintptr, plugin **comHolder, score *int32) int32
	cfStringCreateWithCString         func(alloc uintptr, s string, enc uint32) uintptr
	cfStringGetCString                func(s uintptr, buf *byte, size int, enc uint32) bool
	cfGetTypeID                       func(ref uintptr) uintptr
	cfStringGetTypeID                 func() uintptr
	cfNumberGetTypeID                 func() uintptr
	cfNumberGetValue                  func(num uintptr, typ int, out unsafe.Pointer) bool
	cfRelease                         func(ref uintptr)
	cfUUIDCreateFromString            func(alloc uintptr, s uintptr) uintptr
)

const (
	cfStringEncodingUTF8 = 0x08000100
	cfNumberSInt64Type   = 4
)

func load() error {
	loadOnce.Do(func() {
		defer func() {
			if r := recover(); r != nil {
				loadErr = fmt.Errorf("iokit: binding symbols: %v", r)
			}
		}()
		iokit, err := purego.Dlopen("/System/Library/Frameworks/IOKit.framework/IOKit", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			loadErr = fmt.Errorf("iokit: dlopen IOKit: %w", err)
			return
		}
		cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			loadErr = fmt.Errorf("iokit: dlopen CoreFoundation: %w", err)
			return
		}
		purego.RegisterLibFunc(&ioServiceMatching, iokit, "IOServiceMatching")
		purego.RegisterLibFunc(&ioServiceGetMatchingServices, iokit, "IOServiceGetMatchingServices")
		purego.RegisterLibFunc(&ioServiceGetMatchingService, iokit, "IOServiceGetMatchingService")
		purego.RegisterLibFunc(&ioIteratorNext, iokit, "IOIteratorNext")
		purego.RegisterLibFunc(&ioObjectRelease, iokit, "IOObjectRelease")
		purego.RegisterLibFunc(&ioRegistryEntryGetRegistryEntryID, iokit, "IORegistryEntryGetRegistryEntryID")
		purego.RegisterLibFunc(&ioRegistryEntryIDMatching, iokit, "IORegistryEntryIDMatching")
		purego.RegisterLibFunc(&ioRegistryEntryCreateCFProperty, iokit, "IORegistryEntryCreateCFProperty")
		purego.RegisterLibFunc(&ioCreatePlugInInterfaceForService, iokit, "IOCreatePlugInInterfaceForService")
		purego.RegisterLibFunc(&cfStringCreateWithCString, cf, "CFStringCreateWithCString")
		purego.RegisterLibFunc(&cfStringGetCString, cf, "CFStringGetCString")
		purego.RegisterLibFunc(&cfGetTypeID, cf, "CFGetTypeID")
		purego.RegisterLibFunc(&cfStringGetTypeID, cf, "CFStringGetTypeID")
		purego.RegisterLibFunc(&cfNumberGetTypeID, cf, "CFNumberGetTypeID")
		purego.RegisterLibFunc(&cfNumberGetValue, cf, "CFNumberGetValue")
		purego.RegisterLibFunc(&cfRelease, cf, "CFRelease")
		purego.RegisterLibFunc(&cfUUIDCreateFromString, cf, "CFUUIDCreateFromString")
	})
	return loadErr
}

// UUIDs from IOUSBLib.h / IOCFPlugIn.h.
const (
	uuidUSBDeviceUserClientType = "9DC7B780-9EC0-11D4-A54F-000A27052861"
	uuidCFPlugInInterface       = "C244E858-109C-11D4-91D4-0050E4C6426F"
)

// kIOUSBDeviceInterfaceID (IOUSBDeviceStruct100) as raw CFUUIDBytes.
var uuidBytesUSBDeviceInterface = [16]byte{0x5c, 0x81, 0x87, 0xd0, 0x9e, 0xf3, 0x11, 0xD4, 0x8b, 0x45, 0x00, 0x0a, 0x27, 0x05, 0x28, 0x61}

func cfString(s string) uintptr { return cfStringCreateWithCString(0, s, cfStringEncodingUTF8) }

func cfUUID(s string) uintptr {
	str := cfString(s)
	defer cfRelease(str)
	return cfUUIDCreateFromString(0, str)
}

// registryString reads a string property; "" if absent.
func registryString(entry uint32, key string) string {
	k := cfString(key)
	defer cfRelease(k)
	ref := ioRegistryEntryCreateCFProperty(entry, k, 0, 0)
	if ref == 0 {
		return ""
	}
	defer cfRelease(ref)
	if cfGetTypeID(ref) != cfStringGetTypeID() {
		return ""
	}
	buf := make([]byte, 512)
	if !cfStringGetCString(ref, &buf[0], len(buf), cfStringEncodingUTF8) {
		return ""
	}
	for i, c := range buf {
		if c == 0 {
			return string(buf[:i])
		}
	}
	return string(buf)
}

// registryInt reads a numeric property.
func registryInt(entry uint32, key string) (int64, bool) {
	k := cfString(key)
	defer cfRelease(k)
	ref := ioRegistryEntryCreateCFProperty(entry, k, 0, 0)
	if ref == 0 {
		return 0, false
	}
	defer cfRelease(ref)
	if cfGetTypeID(ref) != cfNumberGetTypeID() {
		return 0, false
	}
	var v int64
	if !cfNumberGetValue(ref, cfNumberSInt64Type, unsafe.Pointer(&v)) {
		return 0, false
	}
	return v, true
}

// comHolder is the layout of a CFPlugIn interface object: a vtable pointer first.
// IOKit hands out pointers to these (the "interface **" convention).
type comHolder struct {
	vtbl *[64]uintptr
}

func (o *comHolder) call(slot int, args ...uintptr) uintptr {
	r, _, _ := purego.SyscallN(o.vtbl[slot], append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...)
	return r
}

// IUnknown slots in CFPlugIn interfaces (slot 0 is _reserved).
const (
	slotQueryInterface = 1
	slotRelease        = 3
)

func (o *comHolder) release() {
	if o != nil {
		o.call(slotRelease)
	}
}

// queryInterface passes CFUUIDBytes by value; a 16-byte integer aggregate is
// passed in two general-purpose registers on both arm64 and amd64.
func (o *comHolder) queryInterface(id [16]byte) (*comHolder, uint32) {
	lo := *(*uint64)(unsafe.Pointer(&id[0]))
	hi := *(*uint64)(unsafe.Pointer(&id[8]))
	var out *comHolder
	hr := o.call(slotQueryInterface, uintptr(lo), uintptr(hi), uintptr(unsafe.Pointer(&out)))
	return out, uint32(hr)
}

// IOReturn formatting.
var ioReturnNames = map[uint32]string{
	0xe00002bc: "kIOReturnError",
	0xe00002bd: "kIOReturnNoMemory",
	0xe00002be: "kIOReturnNoResources",
	0xe00002c0: "kIOReturnNoDevice",
	0xe00002c1: "kIOReturnNotPrivileged",
	0xe00002c2: "kIOReturnBadArgument",
	0xe00002c5: "kIOReturnExclusiveAccess",
	0xe00002c7: "kIOReturnUnsupported",
	0xe00002cd: "kIOReturnNotOpen",
	0xe00002d6: "kIOReturnTimeout",
	0xe00002eb: "kIOReturnAborted",
	0xe00002ed: "kIOReturnNotResponding",
	0xe000404f: "kIOUSBPipeStalled",
	0xe0004051: "kIOUSBTransactionTimeout",
	0xe0004061: "kIOUSBUnknownPipeErr",
}

// IOError is a non-zero IOReturn / kern_return_t.
type IOError struct {
	Op   string
	Code uint32
}

func (e *IOError) Error() string {
	name := ioReturnNames[e.Code]
	if name == "" {
		name = "unknown"
	}
	return fmt.Sprintf("%s: IOReturn 0x%08x (%s)", e.Op, e.Code, name)
}

func ioErr(op string, code uint32) error {
	if code == 0 {
		return nil
	}
	return &IOError{Op: op, Code: code}
}

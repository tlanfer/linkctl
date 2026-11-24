//go:build darwin

package platform

import (
	"fmt"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// CoreMediaIO property address (CMIOObjectPropertyAddress).
type cmioAddress struct {
	selector, scope, element uint32
}

const (
	cmioSystemObject         = 1
	cmioHardwarePropDevices  = 0x64657623 // 'dev#'
	cmioScopeGlobal          = 0x676c6f62 // 'glob'
	cmioElementMain          = 0
	cmioPropName             = 0x6c6e616d // 'lnam'
	cmioPropRunningSomewhere = 0x676f6e65 // 'gone'
	cfStringEncodingUTF8     = 0x08000100
)

var (
	cmioOnce sync.Once
	cmioErr  error

	cmioGetPropertyDataSize func(obj uint32, addr *cmioAddress, qualSize uint32, qual unsafe.Pointer, size *uint32) int32
	cmioGetPropertyData     func(obj uint32, addr *cmioAddress, qualSize uint32, qual unsafe.Pointer, size uint32, used *uint32, data unsafe.Pointer) int32
	cfStringGetCString      func(s uintptr, buf *byte, size int, enc uint32) bool
	cfRelease               func(ref uintptr)

	inUseMu     sync.Mutex
	inUseDevice uint32 // cached CMIO device id, 0 = unknown
)

func loadCMIO() error {
	cmioOnce.Do(func() {
		defer func() {
			if r := recover(); r != nil {
				cmioErr = fmt.Errorf("coremediaio: binding symbols: %v", r)
			}
		}()
		cmio, err := purego.Dlopen("/System/Library/Frameworks/CoreMediaIO.framework/CoreMediaIO", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			cmioErr = fmt.Errorf("coremediaio: dlopen: %w", err)
			return
		}
		cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			cmioErr = fmt.Errorf("coremediaio: dlopen CoreFoundation: %w", err)
			return
		}
		purego.RegisterLibFunc(&cmioGetPropertyDataSize, cmio, "CMIOObjectGetPropertyDataSize")
		purego.RegisterLibFunc(&cmioGetPropertyData, cmio, "CMIOObjectGetPropertyData")
		purego.RegisterLibFunc(&cfStringGetCString, cf, "CFStringGetCString")
		purego.RegisterLibFunc(&cfRelease, cf, "CFRelease")
	})
	return cmioErr
}

func cmioDevices() ([]uint32, error) {
	addr := cmioAddress{cmioHardwarePropDevices, cmioScopeGlobal, cmioElementMain}
	var size uint32
	if st := cmioGetPropertyDataSize(cmioSystemObject, &addr, 0, nil, &size); st != 0 {
		return nil, fmt.Errorf("coremediaio: device list size: OSStatus %d", st)
	}
	n := size / 4
	if n == 0 {
		return nil, nil
	}
	ids := make([]uint32, n)
	var used uint32
	if st := cmioGetPropertyData(cmioSystemObject, &addr, 0, nil, size, &used, unsafe.Pointer(&ids[0])); st != 0 {
		return nil, fmt.Errorf("coremediaio: device list: OSStatus %d", st)
	}
	return ids[:used/4], nil
}

func cmioName(dev uint32) string {
	addr := cmioAddress{cmioPropName, cmioScopeGlobal, cmioElementMain}
	var ref uintptr
	var used uint32
	if st := cmioGetPropertyData(dev, &addr, 0, nil, uint32(unsafe.Sizeof(ref)), &used, unsafe.Pointer(&ref)); st != 0 || ref == 0 {
		return ""
	}
	defer cfRelease(ref)
	buf := make([]byte, 256)
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

func cmioRunning(dev uint32) (bool, error) {
	addr := cmioAddress{cmioPropRunningSomewhere, cmioScopeGlobal, cmioElementMain}
	var v, used uint32
	if st := cmioGetPropertyData(dev, &addr, 0, nil, 4, &used, unsafe.Pointer(&v)); st != 0 {
		return false, fmt.Errorf("coremediaio: DeviceIsRunningSomewhere: OSStatus %d", st)
	}
	return v != 0, nil
}

// findInsta360 returns the CMIO id of the physical Insta360 camera, 0 if none.
func findInsta360() (uint32, string, error) {
	ids, err := cmioDevices()
	if err != nil {
		return 0, "", err
	}
	for _, id := range ids {
		name := cmioName(id)
		if strings.Contains(name, "Insta360") && !strings.Contains(name, "Virtual") {
			return id, name, nil
		}
	}
	return 0, "", nil
}

// inUse reports whether any process is streaming from the Insta360 camera.
func inUse() (bool, error) {
	if err := loadCMIO(); err != nil {
		return false, err
	}
	inUseMu.Lock()
	defer inUseMu.Unlock()
	if inUseDevice != 0 {
		if running, err := cmioRunning(inUseDevice); err == nil {
			return running, nil
		}
		inUseDevice = 0 // stale id (camera re-plugged); re-enumerate
	}
	id, _, err := findInsta360()
	if err != nil || id == 0 {
		return false, err
	}
	running, err := cmioRunning(id)
	if err != nil {
		return false, err
	}
	inUseDevice = id
	return running, nil
}

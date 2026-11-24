// Package uvcdesc parses the UVC VideoControl topology out of a USB
// configuration descriptor. It is OS-independent so it can be unit tested
// everywhere; the macOS backend uses it to address units directly.
package uvcdesc

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Descriptor types and UVC subtypes.
const (
	descInterface   = 0x04
	descCSInterface = 0x24

	classVideo           = 0x0E
	subclassVideoControl = 0x01

	vcInputTerminal  = 0x02
	vcProcessingUnit = 0x05
	vcExtensionUnit  = 0x06
	ittCamera        = 0x0201
)

// ExtensionUnit is one UVC extension unit.
type ExtensionUnit struct {
	ID   uint8
	GUID string // canonical upper-case form, e.g. FAF1672D-B71B-4793-8C91-7B1C9B7F95F8
}

// Topology is the subset of the VideoControl interface we need.
type Topology struct {
	VCInterface      uint8
	CameraTerminalID uint8
	ProcessingUnitID uint8
	ExtensionUnits   []ExtensionUnit
}

// UnitForGUID returns the unit id of the extension unit with guid (case-insensitive match on canonical form).
func (t Topology) UnitForGUID(guid string) (uint8, bool) {
	for _, xu := range t.ExtensionUnits {
		if equalFold(xu.GUID, guid) {
			return xu.ID, true
		}
	}
	return 0, false
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if 'a' <= x && x <= 'z' {
			x -= 32
		}
		if 'a' <= y && y <= 'z' {
			y -= 32
		}
		if x != y {
			return false
		}
	}
	return true
}

// FormatGUID renders 16 bytes in Microsoft GUID layout (first three fields little-endian).
func FormatGUID(b []byte) string {
	if len(b) < 16 {
		return ""
	}
	return fmt.Sprintf("%08X-%04X-%04X-%02X%02X-%02X%02X%02X%02X%02X%02X",
		binary.LittleEndian.Uint32(b[0:4]), binary.LittleEndian.Uint16(b[4:6]), binary.LittleEndian.Uint16(b[6:8]),
		b[8], b[9], b[10], b[11], b[12], b[13], b[14], b[15])
}

// Parse walks a full configuration descriptor.
func Parse(cfg []byte) (Topology, error) {
	var t Topology
	inVC := false
	foundVC := false
	for off := 0; off+2 <= len(cfg); {
		l := int(cfg[off])
		if l < 2 || off+l > len(cfg) {
			break
		}
		d := cfg[off : off+l]
		off += l
		switch d[1] {
		case descInterface:
			if l < 9 {
				continue
			}
			inVC = d[5] == classVideo && d[6] == subclassVideoControl
			if inVC && !foundVC {
				t.VCInterface = d[2]
				foundVC = true
			}
		case descCSInterface:
			if !inVC || l < 4 {
				continue
			}
			switch d[2] {
			case vcInputTerminal:
				if l >= 6 && binary.LittleEndian.Uint16(d[4:6]) == ittCamera && t.CameraTerminalID == 0 {
					t.CameraTerminalID = d[3]
				}
			case vcProcessingUnit:
				if t.ProcessingUnitID == 0 {
					t.ProcessingUnitID = d[3]
				}
			case vcExtensionUnit:
				if l >= 20 {
					t.ExtensionUnits = append(t.ExtensionUnits, ExtensionUnit{ID: d[3], GUID: FormatGUID(d[4:20])})
				}
			}
		}
	}
	if !foundVC {
		return t, errors.New("no UVC VideoControl interface in configuration descriptor")
	}
	return t, nil
}

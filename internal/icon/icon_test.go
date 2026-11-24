package icon

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"testing"
)

func TestICO(t *testing.T) {
	ico := ICO()
	if binary.LittleEndian.Uint16(ico[2:]) != 1 || binary.LittleEndian.Uint16(ico[4:]) != 4 {
		t.Fatalf("bad header % x", ico[:6])
	}
	off := binary.LittleEndian.Uint32(ico[6+12:])
	if _, err := png.Decode(bytes.NewReader(ico[off:])); err != nil {
		t.Fatalf("first entry not a PNG: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(TemplatePNG(36))); err != nil {
		t.Fatal(err)
	}
}

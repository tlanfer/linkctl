//go:build windows && hardware

package dshow

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"
)

// TestXUWatch samples std pan/tilt and XU 9/0x16 + 9/0x1A (read-only).
// XUWATCH_SECONDS controls the duration.
func TestXUWatch(t *testing.T) {
	secs, _ := strconv.Atoi(os.Getenv("XUWATCH_SECONDS"))
	if secs == 0 {
		secs = 1
	}
	b := New()
	ds, _ := b.List()
	d, err := b.Open(ds[0])
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var last string
	start := time.Now()
	for time.Since(start) < time.Duration(secs)*time.Second {
		p, _ := d.StdGet("pan")
		tl, _ := d.StdGet("tilt")
		rel, _ := d.XUGet(9, 0x16)
		abs, _ := d.XUGet(9, 0x1a)
		line := fmt.Sprintf("std %d/%d rel %s", p, tl, hex.EncodeToString(rel))
		if len(abs) >= 8 {
			ap := int32(binary.LittleEndian.Uint32(abs))
			at := int32(binary.LittleEndian.Uint32(abs[4:]))
			line += fmt.Sprintf(" abs %.2f/%.2f", float64(ap)/3600, float64(at)/3600)
		}
		if line != last {
			t.Logf("%6dms %s", time.Since(start).Milliseconds(), line)
			last = line
		}
		time.Sleep(15 * time.Millisecond)
	}
}

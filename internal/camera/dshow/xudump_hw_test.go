//go:build windows && hardware

package dshow

import (
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// TestXUDump lists length and current value of every selector on all vendor
// extension units. Read-only (GET_LEN / GET_CUR).
func TestXUDump(t *testing.T) {
	th, _ := newCOMThread()
	th.do(func() error {
		ms, _ := enumerateMonikers()
		var f *filter
		for _, m := range ms {
			if f == nil && strings.Contains(strings.ToLower(m.DevicePath), "vid_2e1a") {
				f, _ = bindFilter(m.obj)
			}
		}
		nodes, _ := f.devSpecificNodes()
		for _, n := range nodes {
			sets, _ := f.nodePropertySets(n)
			for sel := uint32(1); sel <= 0x30; sel++ {
				p := ksPNode{Set: sets[0], ID: sel, Flags: ksPropertyTypeGet | ksPropertyTypeTopology, NodeID: n}
				size, err := f.ksProperty(&p, nil)
				var he *hrError
				if err != nil && !(errors.As(err, &he) && he.hr == hrMoreData) {
					continue
				}
				if size == 0 {
					continue
				}
				if size > 64 {
					t.Logf("node %d sel 0x%02x len %3d (not read)", n, sel, size)
					continue
				}
				buf := make([]byte, size)
				got, err := f.ksProperty(&p, buf)
				t.Logf("node %d sel 0x%02x len %3d err=%v %s", n, sel, size, err, hex.EncodeToString(buf[:got]))
			}
		}
		return nil
	})
}

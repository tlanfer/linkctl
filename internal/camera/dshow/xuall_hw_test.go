//go:build windows && hardware

package dshow

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestXUAllWatch records byte-level changes of every readable vendor XU
// selector (read-only: GET_LEN / GET_CUR), to discover which register the
// official app writes for a feature. XUWATCH_SECONDS sets the duration.
// Selectors that change constantly with gimbal motion are skipped.
func TestXUAllWatch(t *testing.T) {
	secs, _ := strconv.Atoi(os.Getenv("XUWATCH_SECONDS"))
	if secs == 0 {
		secs = 5
	}
	th, _ := newCOMThread()
	type reg struct {
		node uint32
		p    ksPNode
		size int
		last []byte
	}
	var f *filter
	var regs []*reg
	th.do(func() error {
		ms, _ := enumerateMonikers()
		for _, m := range ms {
			if f == nil && strings.Contains(strings.ToLower(m.DevicePath), "vid_2e1a") {
				f, _ = bindFilter(m.obj)
			}
		}
		nodes, _ := f.devSpecificNodes()
		for _, n := range nodes {
			sets, _ := f.nodePropertySets(n)
			for sel := uint32(1); sel <= 0x30; sel++ {
				if n == 1 && (sel == 0x1a || sel == 0x03) { // live position, static device info
					continue
				}
				p := ksPNode{Set: sets[0], ID: sel, Flags: ksPropertyTypeGet | ksPropertyTypeTopology, NodeID: n}
				size, err := f.ksProperty(&p, nil)
				var he *hrError
				if (err != nil && !(errors.As(err, &he) && he.hr == hrMoreData)) || size == 0 || size > 300 {
					continue
				}
				regs = append(regs, &reg{node: n, p: p, size: size})
			}
		}
		return nil
	})
	t.Logf("watching %d selectors for %ds", len(regs), secs)
	start := time.Now()
	for time.Since(start) < time.Duration(secs)*time.Second {
		th.do(func() error {
			for _, r := range regs {
				buf := make([]byte, r.size)
				got, err := f.ksProperty(&r.p, buf)
				if err != nil {
					continue
				}
				buf = buf[:got]
				if r.last != nil && string(buf) != string(r.last) {
					var diffs []string
					for i := range buf {
						if i >= len(r.last) || buf[i] != r.last[i] {
							old := -1
							if i < len(r.last) {
								old = int(r.last[i])
							}
							diffs = append(diffs, fmt.Sprintf("[%d] %02x->%02x", i, old, buf[i]))
						}
					}
					if len(diffs) > 12 {
						diffs = append(diffs[:12], fmt.Sprintf("… (%d bytes changed)", len(diffs)))
					}
					unit := map[uint32]int{1: 9, 2: 10, 3: 11}[r.node]
					t.Logf("%7.1fs unit %d sel 0x%02x len %d: %s", time.Since(start).Seconds(), unit, r.p.ID, r.size, strings.Join(diffs, " "))
				}
				r.last = buf
			}
			return nil
		})
		time.Sleep(100 * time.Millisecond)
	}
}

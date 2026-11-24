//go:build windows && hardware

package dshow

import (
	"testing"
)

func TestProbe(t *testing.T) {
	th, err := newCOMThread()
	if err != nil {
		t.Fatal(err)
	}
	err = th.do(func() error {
		ms, err := enumerateMonikers()
		if err != nil {
			return err
		}
		for _, m := range ms {
			t.Logf("device %q path=%q", m.FriendlyName, m.DevicePath)
			f, err := bindFilter(m.obj)
			if err != nil {
				t.Logf("  bind: %v", err)
				continue
			}
			for p := int32(0); p <= 13; p++ {
				r, err := ifaceGetRange(f.procAmp, p)
				v, fl, gerr := ifaceGet(f.procAmp, p)
				t.Logf("  procamp %2d range=%+v err=%v value=%d flags=%d err=%v", p, r, err, v, fl, gerr)
			}
			for p := int32(0); p <= 19; p++ {
				r, err := ifaceGetRange(f.camCtrl, p)
				v, fl, gerr := ifaceGet(f.camCtrl, p)
				t.Logf("  camctrl %2d range=%+v err=%v value=%d flags=%d err=%v", p, r, err, v, fl, gerr)
			}
			nodes, err := f.devSpecificNodes()
			t.Logf("  xu nodes=%v err=%v", nodes, err)
			for _, n := range nodes {
				sets, err := f.nodePropertySets(n)
				t.Logf("    node %d sets=%v err=%v", n, sets, err)
			}
			f.release()
			m.obj.Release()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

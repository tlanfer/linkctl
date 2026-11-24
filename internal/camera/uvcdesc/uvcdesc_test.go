package uvcdesc

import "testing"

func guidBytes() []byte {
	// FAF1672D-B71B-4793-8C91-7B1C9B7F95F8 in on-the-wire (mixed-endian) order.
	return []byte{0x2D, 0x67, 0xF1, 0xFA, 0x1B, 0xB7, 0x93, 0x47, 0x8C, 0x91, 0x7B, 0x1C, 0x9B, 0x7F, 0x95, 0xF8}
}

func TestParse(t *testing.T) {
	var cfg []byte
	cfg = append(cfg, 9, 0x02, 0, 0, 2, 1, 0, 0x80, 50)                                  // configuration
	cfg = append(cfg, 8, 0x0B, 0, 2, 0x0E, 0x03, 0, 0)                                   // IAD
	cfg = append(cfg, 9, 0x04, 0, 0, 1, 0x0E, 0x01, 0, 0)                                // VC interface 0
	cfg = append(cfg, 13, 0x24, 0x01, 0, 1, 0, 0, 0, 0, 0, 0, 1, 1)                      // VC header
	cfg = append(cfg, 18, 0x24, 0x02, 1, 0x01, 0x02, 0, 0, 0, 0, 0, 0, 0, 0, 0, 3, 0, 0) // camera IT id 1
	cfg = append(cfg, 11, 0x24, 0x05, 3, 1, 0, 0, 2, 0xFF, 0xFF, 0)                      // PU id 3
	xu := []byte{27, 0x24, 0x06, 9}
	xu = append(xu, guidBytes()...)
	xu = append(xu, 24, 1, 3, 3, 0xFF, 0xFF, 0xFF)
	cfg = append(cfg, xu...)
	cfg = append(cfg, 9, 0x04, 1, 0, 1, 0x0E, 0x02, 0, 0)                                // VS interface 1
	cfg = append(cfg, 18, 0x24, 0x02, 7, 0x01, 0x02, 0, 0, 0, 0, 0, 0, 0, 0, 0, 3, 0, 0) // not VC: ignored

	top, err := Parse(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if top.VCInterface != 0 || top.CameraTerminalID != 1 || top.ProcessingUnitID != 3 {
		t.Fatalf("unexpected topology %+v", top)
	}
	if len(top.ExtensionUnits) != 1 || top.ExtensionUnits[0].GUID != "FAF1672D-B71B-4793-8C91-7B1C9B7F95F8" {
		t.Fatalf("unexpected XUs %+v", top.ExtensionUnits)
	}
	if id, ok := top.UnitForGUID("faf1672d-b71b-4793-8c91-7b1c9b7f95f8"); !ok || id != 9 {
		t.Fatalf("UnitForGUID = %d %v", id, ok)
	}
}

func TestParseNoVC(t *testing.T) {
	if _, err := Parse([]byte{9, 0x02, 0, 0, 1, 1, 0, 0x80, 50}); err == nil {
		t.Fatal("expected error")
	}
}

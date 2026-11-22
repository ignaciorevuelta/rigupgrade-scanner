package main

import (
	"encoding/binary"
	"strings"
	"testing"
)

// st builds one SMBIOS structure: formatted area of length l with fields set, plus strings.
func st(typ byte, l int, set func(f []byte), strs ...string) []byte {
	f := make([]byte, l)
	f[0], f[1] = typ, byte(l)
	if set != nil {
		set(f)
	}
	if len(strs) == 0 {
		return append(f, 0, 0)
	}
	return append(append(f, []byte(strings.Join(strs, "\x00"))...), 0, 0)
}

func array(maxKB uint32, slots uint16) []byte {
	return st(16, 0x17, func(f []byte) {
		f[5] = 3
		binary.LittleEndian.PutUint32(f[7:], maxKB)
		binary.LittleEndian.PutUint16(f[0x0D:], slots)
	})
}

func dimm(sizeMB uint16, ff, typ byte, speed uint16, loc string) []byte {
	return st(17, 0x28, func(f []byte) {
		binary.LittleEndian.PutUint16(f[0x0C:], sizeMB)
		f[0x0E], f[0x10], f[0x12] = ff, 1, typ
		binary.LittleEndian.PutUint16(f[0x15:], speed)
		f[0x17], f[0x18], f[0x1A] = 2, 3, 4 // 0x18 = serial: must never be exported
		binary.LittleEndian.PutUint16(f[0x20:], speed)
	}, loc, "Kingston", "SERIAL-SECRET", "KF432S20IB/16")
}

func cat(parts ...[]byte) []byte {
	var b []byte
	for _, p := range parts {
		b = append(b, p...)
	}
	return append(b, st(127, 4, nil)...)
}

func TestDesktopFreeSlots(t *testing.T) {
	s := parseSMBIOS(cat(
		st(1, 0x1B, func(f []byte) { f[4], f[5], f[7] = 1, 2, 3 }, "ASUS", "To Be Filled By O.E.M.", "SYS-SERIAL"),
		st(2, 0x0F, func(f []byte) { f[4], f[5] = 1, 2 }, "ASUSTeK", "PRIME B550M-A"),
		array(128*1024*1024, 4),
		dimm(16384, 0x09, 0x1A, 3200, "DIMM_A1"), dimm(0, 0x09, 0x1A, 0, "DIMM_A2"),
		dimm(16384, 0x09, 0x1A, 3200, "DIMM_B1"), dimm(0, 0x09, 0x1A, 0, "DIMM_B2"),
	))
	if s.System.Mfr != "ASUS" || s.System.Model != "" || s.Board.Model != "PRIME B550M-A" {
		t.Fatalf("system/board: %+v %+v", s.System, s.Board)
	}
	if s.Mem.MaxMB != 131072 || s.Mem.Slots != 4 || len(s.Dimms) != 4 {
		t.Fatalf("mem: %+v dimms=%d", s.Mem, len(s.Dimms))
	}
	d := s.Dimms[0]
	if *d.SizeMB != 16384 || d.Type != "DDR4" || d.FF != "DIMM" || d.Speed != 3200 || d.Part != "KF432S20IB/16" || d.Loc != "DIMM_A1" {
		t.Fatalf("dimm: %+v", d)
	}
	if *s.Dimms[1].SizeMB != 0 {
		t.Fatal("empty slot must be size 0")
	}
}

func TestSerialNeverExported(t *testing.T) {
	data, _ := encode(Payload{V: 1, SMBIOS: parseSMBIOS(cat(
		st(1, 0x1B, func(f []byte) { f[4], f[5], f[7] = 1, 2, 3 }, "Dell", "XPS", "SYS-SERIAL"),
		dimm(8192, 0x0D, 0x1A, 2666, "A"),
	))})
	if strings.Contains(data, "SERIAL") {
		t.Fatal("serial leaked")
	}
	s := parseSMBIOS(cat(dimm(8192, 0x0D, 0x1A, 2666, "A")))
	if strings.Contains(s.Dimms[0].Mfr+s.Dimms[0].Part+s.Dimms[0].Loc, "SERIAL") {
		t.Fatal("serial leaked into dimm")
	}
}

func TestLaptopSolderedAndUnknown(t *testing.T) {
	s := parseSMBIOS(cat(
		array(0, 0),                                // firmware declares nothing
		dimm(0xFFFF, 0x0B, 0x1E, 4266, "Onboard"), // LPDDR4, row of chips, unknown size
	))
	if s.Mem.MaxMB != 0 || s.Mem.Slots != 0 {
		t.Fatalf("mem: %+v", s.Mem)
	}
	d := s.Dimms[0]
	if d.SizeMB != nil || d.FF != "CHIP" || d.Type != "LPDDR4" {
		t.Fatalf("dimm: %+v", d)
	}
}

func TestExtendedSizesAndTruncation(t *testing.T) {
	big := st(17, 0x28, func(f []byte) {
		binary.LittleEndian.PutUint16(f[0x0C:], 0x7FFF)
		binary.LittleEndian.PutUint32(f[0x1C:], 65536)
		f[0x0E], f[0x12] = 0x09, 0x22
	})
	ext := st(16, 0x17, func(f []byte) {
		f[5] = 3
		binary.LittleEndian.PutUint32(f[7:], 0x80000000)
		binary.LittleEndian.PutUint64(f[0x0F:], 256<<30)
		binary.LittleEndian.PutUint16(f[0x0D:], 2)
	})
	s := parseSMBIOS(cat(ext, big))
	if s.Mem.MaxMB != 262144 || *s.Dimms[0].SizeMB != 65536 || s.Dimms[0].Type != "DDR5" {
		t.Fatalf("%+v %+v", s.Mem, s.Dimms[0])
	}
	// Truncated / garbage input must not panic.
	for i := 0; i < 40; i++ {
		parseSMBIOS(cat(ext, big)[:i])
	}
	parseSMBIOS(nil)
}

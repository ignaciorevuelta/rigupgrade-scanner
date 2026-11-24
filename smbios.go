package main

import (
	"bytes"
	"encoding/binary"
	"strings"
)

// Pure SMBIOS parser (no Windows calls) so it can be tested with synthetic tables.
// Only whitelisted fields are read. Serial numbers, UUIDs and asset tags are never touched.

type Board struct {
	Mfr   string `json:"mfr,omitempty"`
	Model string `json:"model,omitempty"`
}

// System adds the two model fields some brands need: Lenovo puts the machine type in Model
// and the readable name ("ThinkPad T480") in Version and Family. Never the serial number.
type System struct {
	Mfr     string `json:"mfr,omitempty"`
	Model   string `json:"model,omitempty"`
	Version string `json:"ver,omitempty"`
	Family  string `json:"family,omitempty"`
}

type Dimm struct {
	Loc    string `json:"loc,omitempty"`
	SizeMB *int   `json:"size_mb"` // nil = unknown, 0 = empty slot
	Type   string `json:"type,omitempty"`
	FF     string `json:"ff,omitempty"`
	Speed  int    `json:"speed,omitempty"`
	CSpeed int    `json:"cspeed,omitempty"`
	Mfr    string `json:"mfr,omitempty"`
	Part   string `json:"part,omitempty"`
}

type Memory struct {
	MaxMB int `json:"max_mb"` // 0 = not declared
	Slots int `json:"slots"`
}

type SMBIOS struct {
	System System `json:"sys"`
	Board  Board  `json:"board"`
	Mem    Memory `json:"mem"`
	Dimms  []Dimm `json:"dimms"`
}

var memTypes = map[byte]string{
	0x12: "DDR", 0x13: "DDR2", 0x18: "DDR3", 0x1A: "DDR4", 0x22: "DDR5",
	0x1B: "LPDDR", 0x1C: "LPDDR2", 0x1D: "LPDDR3", 0x1E: "LPDDR4", 0x23: "LPDDR5",
}

func formFactor(b byte) string {
	switch b {
	case 0x09:
		return "DIMM"
	case 0x0D:
		return "SODIMM"
	case 0x05, 0x0A, 0x0B: // chip, TSOP, row of chips: memory on the board
		return "CHIP"
	case 0x00, 0x01, 0x02:
		return ""
	}
	return "OTHER"
}

var junk = []string{"to be filled by o.e.m.", "default string", "not specified", "system product name",
	"system manufacturer", "unknown", "none", "n/a", "0x0000", "undefined"}

func clean(s string) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s))
	for _, j := range junk {
		if strings.EqualFold(s, j) {
			return ""
		}
	}
	return s
}

func parseSMBIOS(t []byte) SMBIOS {
	s := SMBIOS{Dimms: []Dimm{}}
	for len(t) >= 4 {
		typ, l := t[0], int(t[1])
		if l < 4 || l+2 > len(t) {
			break
		}
		f := t[:l]
		end := bytes.Index(t[l:], []byte{0, 0})
		if end < 0 {
			break
		}
		var strs []string
		if end > 0 {
			strs = strings.Split(string(t[l:l+end]), "\x00")
		}
		str := func(off int) string {
			if off >= l {
				return ""
			}
			i := int(f[off])
			if i == 0 || i > len(strs) {
				return ""
			}
			return clean(strs[i-1])
		}
		switch typ {
		case 1:
			s.System = System{Mfr: str(4), Model: str(5), Version: str(6)}
			if l >= 0x1B {
				s.System.Family = str(0x1A)
			}
		case 2:
			s.Board = Board{str(4), str(5)}
		case 16:
			if l >= 0x0F && f[5] == 3 { // use = system memory
				mx := binary.LittleEndian.Uint32(f[7:])
				if mx == 0x80000000 {
					if l >= 0x17 {
						s.Mem.MaxMB += int(binary.LittleEndian.Uint64(f[0x0F:]) >> 20)
					}
				} else {
					s.Mem.MaxMB += int(mx / 1024)
				}
				s.Mem.Slots += int(binary.LittleEndian.Uint16(f[0x0D:]))
			}
		case 17:
			if l >= 0x15 {
				s.Dimms = append(s.Dimms, parseDimm(f, str))
			}
		case 127:
			return s
		}
		t = t[l+end+2:]
	}
	return s
}

func parseDimm(f []byte, str func(int) string) Dimm {
	l := len(f)
	d := Dimm{Loc: str(0x10), Type: memTypes[f[0x12]], FF: formFactor(f[0x0E])}
	size := binary.LittleEndian.Uint16(f[0x0C:])
	var v int
	switch {
	case size == 0xFFFF:
		v = -1
	case size == 0x7FFF:
		v = -1
		if l >= 0x20 {
			v = int(binary.LittleEndian.Uint32(f[0x1C:]) & 0x7FFFFFFF)
		}
	case size&0x8000 != 0:
		v = int(size&0x7FFF) / 1024 // KB units
	default:
		v = int(size)
	}
	if v >= 0 {
		d.SizeMB = &v
	}
	if l >= 0x17 {
		if sp := binary.LittleEndian.Uint16(f[0x15:]); sp != 0xFFFF {
			d.Speed = int(sp)
		}
	}
	d.Mfr = str(0x17)
	d.Part = str(0x1A)
	if l >= 0x22 {
		if sp := binary.LittleEndian.Uint16(f[0x20:]); sp != 0xFFFF {
			d.CSpeed = int(sp)
		}
	}
	return d
}

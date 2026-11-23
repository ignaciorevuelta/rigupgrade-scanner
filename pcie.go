package main

// PCIe generation of NVMe drives and of the port they sit on, read from the Windows device
// tree (SetupAPI + cfgmgr32). No admin rights needed. Nothing here identifies the machine:
// only small integers leave the program.

import (
	"syscall"
	"unsafe"
)

type PCIe struct {
	Link int `json:"link_gen,omitempty"` // generation the drive is running at now
	Dev  int `json:"dev_gen,omitempty"`  // highest generation the drive supports
	Slot int `json:"slot_gen,omitempty"` // highest generation of the port (slot) above it
}

type guid struct {
	d1     uint32
	d2, d3 uint16
	d4     [8]byte
}

type devPropKey struct {
	fmtid guid
	pid   uint32
}

var (
	setupapi   = syscall.NewLazyDLL("setupapi.dll")
	cfgmgr32   = syscall.NewLazyDLL("cfgmgr32.dll")
	pGetClass  = setupapi.NewProc("SetupDiGetClassDevsW")
	pEnumIface = setupapi.NewProc("SetupDiEnumDeviceInterfaces")
	pIfaceDet  = setupapi.NewProc("SetupDiGetDeviceInterfaceDetailW")
	pDestroy   = setupapi.NewProc("SetupDiDestroyDeviceInfoList")
	pParent    = cfgmgr32.NewProc("CM_Get_Parent")
	pDevProp   = cfgmgr32.NewProc("CM_Get_DevNode_PropertyW")

	guidDiskIface = guid{0x53f56307, 0xb6bf, 0x11d0, [8]byte{0x94, 0xf2, 0x00, 0xa0, 0xc9, 0x1e, 0xfb, 0x8b}}
	pciFmt        = guid{0x3ab22e31, 0x8264, 0x4b4e, [8]byte{0x9a, 0xf5, 0xa8, 0xd2, 0xd8, 0xe3, 0x3e, 0x62}}
	keyCurSpeed   = devPropKey{pciFmt, 9}
	keyMaxSpeed   = devPropKey{pciFmt, 11}
)

const (
	digcfPresent         = 0x2
	digcfDeviceInterface = 0x10
	ioctlGetDeviceNumber = 0x2D1080
	devpropTypeUint32    = 7
)

// linkGen turns the PCI link-speed code into a PCIe generation (1 = 2.5 GT/s … 5 = 32 GT/s).
func linkGen(code uint32) int {
	if code >= 1 && code <= 6 {
		return int(code)
	}
	return 0
}

func devPropU32(inst uint32, key *devPropKey) (uint32, bool) {
	var typ, v uint32
	size := uint32(4)
	r, _, _ := pDevProp.Call(uintptr(inst), uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(&typ)),
		uintptr(unsafe.Pointer(&v)), uintptr(unsafe.Pointer(&size)), 0)
	return v, r == 0 && typ == devpropTypeUint32
}

func parent(inst uint32) (uint32, bool) {
	var p uint32
	r, _, _ := pParent.Call(uintptr(unsafe.Pointer(&p)), uintptr(inst), 0)
	return p, r == 0
}

// pcieFor walks up from a disk's device node to the first PCI device (the NVMe controller)
// and reads its link speeds, then the maximum speed of the port above it.
func pcieFor(inst uint32) *PCIe {
	for i := 0; i < 8; i++ {
		p, ok := parent(inst)
		if !ok {
			return nil
		}
		inst = p
		cur, ok := devPropU32(inst, &keyCurSpeed)
		if !ok {
			continue
		}
		out := &PCIe{Link: linkGen(cur)}
		if m, ok := devPropU32(inst, &keyMaxSpeed); ok {
			out.Dev = linkGen(m)
		}
		if port, ok := parent(inst); ok {
			if m, ok := devPropU32(port, &keyMaxSpeed); ok {
				out.Slot = linkGen(m)
			}
		}
		if out.Link == 0 && out.Dev == 0 && out.Slot == 0 {
			return nil
		}
		return out
	}
	return nil
}

// pcieByDrive maps PhysicalDrive numbers to PCIe link data. Missing entries mean "unknown".
func pcieByDrive() map[uint32]*PCIe {
	out := map[uint32]*PCIe{}
	if pGetClass.Find() != nil || pDevProp.Find() != nil {
		return out
	}
	set, _, _ := pGetClass.Call(uintptr(unsafe.Pointer(&guidDiskIface)), 0, 0, digcfPresent|digcfDeviceInterface)
	if set == 0 || set == ^uintptr(0) {
		return out
	}
	defer pDestroy.Call(set)

	type ifaceData struct {
		cbSize   uint32
		class    guid
		flags    uint32
		reserved uintptr
	}
	type devinfoData struct {
		cbSize   uint32
		class    guid
		devInst  uint32
		reserved uintptr
	}
	for i := uintptr(0); i < 64; i++ {
		ifd := ifaceData{cbSize: uint32(unsafe.Sizeof(ifaceData{}))}
		r, _, _ := pEnumIface.Call(set, 0, uintptr(unsafe.Pointer(&guidDiskIface)), i, uintptr(unsafe.Pointer(&ifd)))
		if r == 0 {
			break
		}
		// Detail struct: DWORD cbSize (8 on 64-bit) followed by the device path.
		buf := make([]uint16, 1024)
		*(*uint32)(unsafe.Pointer(&buf[0])) = 8
		did := devinfoData{cbSize: uint32(unsafe.Sizeof(devinfoData{}))}
		r, _, _ = pIfaceDet.Call(set, uintptr(unsafe.Pointer(&ifd)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)*2), 0, uintptr(unsafe.Pointer(&did)))
		if r == 0 {
			continue
		}
		path := &buf[2]
		h, err := syscall.CreateFile(path, 0, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0, 0)
		if err != nil {
			continue
		}
		var num [3]uint32 // DeviceType, DeviceNumber, PartitionNumber
		var ret uint32
		err = syscall.DeviceIoControl(h, ioctlGetDeviceNumber, nil, 0, (*byte)(unsafe.Pointer(&num[0])), 12, &ret, nil)
		syscall.CloseHandle(h)
		if err != nil || ret < 8 {
			continue
		}
		if p := pcieFor(did.devInst); p != nil {
			out[num[1]] = p
		}
	}
	return out
}

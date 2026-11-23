//go:build windows

package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// Overridable at build time: -ldflags "-X main.baseURL=http://localhost:8787"
var baseURL = "https://rigupgrade.com"

var (
	kernel32     = syscall.NewLazyDLL("kernel32.dll")
	procFirmware = kernel32.NewProc("GetSystemFirmwareTable")
	procUILang   = kernel32.NewProc("GetUserDefaultUILanguage")
	procShellEx  = syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW")
	procMsgBox   = syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")
)

func main() {
	p := Payload{V: 1, SMBIOS: parseSMBIOS(readSMBIOS()), CPU: cpuName(), OS: osName(), Disks: readDisks()}

	if len(os.Args) > 1 && os.Args[1] == "-json" { // for inspection: RigUpgradeScanner.exe -json > out.json
		b, _ := json.MarshalIndent(p, "", "  ")
		os.Stdout.Write(append(b, '\n'))
		return
	}

	es := false
	if l, _, _ := procUILang.Call(); l&0x3FF == 0x0A { // LANG_SPANISH
		es = true
	}
	data, err := encode(p)
	if err != nil {
		msgBox("RigUpgrade", "Error: "+err.Error())
		os.Exit(1)
	}
	path := "/en/r#"
	if es {
		path = "/es/r#"
	}
	url := baseURL + path + data
	if !openURL(url) {
		text := "Could not open your browser. Copy this link (Ctrl+C copies this whole message):\n\n" + url
		if es {
			text = "No se ha podido abrir el navegador. Copia este enlace (Ctrl+C copia todo este mensaje):\n\n" + url
		}
		msgBox("RigUpgrade", text)
	}
}

func u16(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }

func openURL(url string) bool {
	r, _, _ := procShellEx.Call(0, uintptr(unsafe.Pointer(u16("open"))), uintptr(unsafe.Pointer(u16(url))), 0, 0, 1)
	return r > 32
}

func msgBox(title, text string) {
	procMsgBox.Call(0, uintptr(unsafe.Pointer(u16(text))), uintptr(unsafe.Pointer(u16(title))), 0x40)
}

func readSMBIOS() []byte {
	const rsmb = 0x52534D42 // 'RSMB'
	n, _, _ := procFirmware.Call(rsmb, 0, 0, 0)
	if n < 8 {
		return nil
	}
	b := make([]byte, n)
	got, _, _ := procFirmware.Call(rsmb, 0, uintptr(unsafe.Pointer(&b[0])), n)
	if got < 8 || got > n {
		return nil
	}
	l := int(binary.LittleEndian.Uint32(b[4:8])) // RawSMBIOSData header: 4 bytes + Length
	if 8+l > int(got) {
		l = int(got) - 8
	}
	return b[8 : 8+l]
}

func regStr(path, name string) string {
	var h syscall.Handle
	if syscall.RegOpenKeyEx(syscall.HKEY_LOCAL_MACHINE, u16(path), 0, syscall.KEY_READ, &h) != nil {
		return ""
	}
	defer syscall.RegCloseKey(h)
	buf := make([]uint16, 512)
	n := uint32(len(buf) * 2)
	var typ uint32
	if syscall.RegQueryValueEx(h, u16(name), nil, &typ, (*byte)(unsafe.Pointer(&buf[0])), &n) != nil || typ != syscall.REG_SZ {
		return ""
	}
	return strings.TrimSpace(syscall.UTF16ToString(buf[:n/2]))
}

func cpuName() string {
	return clean(regStr(`HARDWARE\DESCRIPTION\System\CentralProcessor\0`, "ProcessorNameString"))
}

func osName() string {
	const k = `SOFTWARE\Microsoft\Windows NT\CurrentVersion`
	name, build := regStr(k, "ProductName"), regStr(k, "CurrentBuild")
	if b, _ := strconv.Atoi(build); b >= 22000 { // Windows 11 still reports "Windows 10" here
		name = strings.Replace(name, "Windows 10", "Windows 11", 1)
	}
	if build != "" {
		name += " (build " + build + ")"
	}
	return name
}

var busNames = map[uint32]string{1: "scsi", 2: "atapi", 3: "ata", 7: "usb", 8: "raid", 10: "sas", 11: "sata", 12: "sd", 13: "mmc", 17: "nvme"}

func readDisks() []Disk {
	disks := []Disk{}
	links := pcieByDrive()
	for i := 0; i < 32; i++ {
		// Access 0: metadata queries only, works without administrator rights.
		h, err := syscall.CreateFile(u16(fmt.Sprintf(`\\.\PhysicalDrive%d`, i)), 0,
			syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0, 0)
		if err != nil {
			continue
		}
		if d, ok := queryDisk(h); ok {
			if d.Bus == "nvme" {
				d.PCIe = links[uint32(i)]
			}
			disks = append(disks, d)
		}
		syscall.CloseHandle(h)
	}
	return disks
}

func queryDisk(h syscall.Handle) (Disk, bool) {
	const ioctlQueryProperty, ioctlGeometryEx = 0x2D1400, 0x700A0
	var d Disk
	var ret uint32
	q := make([]byte, 12) // STORAGE_PROPERTY_QUERY{PropertyId, QueryType, AdditionalParameters}
	buf := make([]byte, 1024)
	if syscall.DeviceIoControl(h, ioctlQueryProperty, &q[0], 12, &buf[0], 1024, &ret, nil) != nil || ret < 32 {
		return d, false
	}
	desc := buf[:ret]
	bus := binary.LittleEndian.Uint32(desc[28:])
	if bus >= 14 && bus <= 16 { // virtual, file-backed, storage spaces
		return d, false
	}
	str := func(off uint32) string {
		if off == 0 || int(off) >= len(desc) {
			return ""
		}
		s := desc[off:]
		if i := bytes.IndexByte(s, 0); i >= 0 {
			s = s[:i]
		}
		return clean(string(s))
	}
	// Vendor + product only; the serial number offset (desc[24:]) is deliberately ignored.
	d.Model = strings.TrimSpace(str(binary.LittleEndian.Uint32(desc[12:])) + " " + str(binary.LittleEndian.Uint32(desc[16:])))
	d.Bus = busNames[bus]
	if d.Bus == "" {
		d.Bus = "other"
	}
	d.Removable = desc[10] != 0

	binary.LittleEndian.PutUint32(q, 7) // StorageDeviceSeekPenaltyProperty
	sp := make([]byte, 12)
	if syscall.DeviceIoControl(h, ioctlQueryProperty, &q[0], 12, &sp[0], 12, &ret, nil) == nil && ret >= 9 {
		ssd := sp[8] == 0
		d.SSD = &ssd
	}
	g := make([]byte, 256)
	if syscall.DeviceIoControl(h, ioctlGeometryEx, nil, 0, &g[0], 256, &ret, nil) == nil && ret >= 32 {
		d.SizeGB = int(binary.LittleEndian.Uint64(g[24:]) / 1e9)
	}
	return d, true
}

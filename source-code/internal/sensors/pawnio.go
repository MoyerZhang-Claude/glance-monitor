//go:build windows

package sensors

import (
	_ "embed"
	"encoding/binary"
	"errors"
	"syscall"
	"time"
	"unsafe"

	"glance/internal/w32"
)

// Signed PawnIO modules (LGPL-2.1, from namazso/PawnIO.Modules 0.2.11,
// as shipped by LibreHardwareMonitor). They run inside the PawnIO driver.
var (
	//go:embed pawnio/IntelMSR.bin
	modIntelMSR []byte
	//go:embed pawnio/AMDFamily17.bin
	modAMD17 []byte
	//go:embed pawnio/Nvidia.bin
	modNvidia []byte
)

const (
	pawnDeviceType  = 41394 << 16
	ioctlPawnLoad   = pawnDeviceType | (0x821 << 2)
	ioctlPawnExec   = pawnDeviceType | (0x841 << 2)
	pawnFnNameLen   = 32
)

var errPawnMissing = errors.New("PawnIO driver not available")

// pawnModule is one loaded PawnIO module (each needs its own handle).
type pawnModule struct {
	h   w32.HANDLE
	in  []byte
	out []byte
}

// PawnIOInstalledVersion reads the installed PawnIO version from the
// uninstall registry key ("" when not installed).
func PawnIOInstalledVersion() string {
	for _, view := range []uint32{0x0100 /*KEY_WOW64_64KEY*/, 0x0200 /*KEY_WOW64_32KEY*/} {
		var k syscall.Handle
		path, _ := syscall.UTF16PtrFromString(`SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\PawnIO`)
		if syscall.RegOpenKeyEx(syscall.HKEY_LOCAL_MACHINE, path, 0, syscall.KEY_READ|view, &k) != nil {
			continue
		}
		v := regString(k, "DisplayVersion")
		syscall.RegCloseKey(k)
		if v != "" {
			return v
		}
	}
	return ""
}

func regString(k syscall.Handle, name string) string {
	n, _ := syscall.UTF16PtrFromString(name)
	var typ, size uint32
	if syscall.RegQueryValueEx(k, n, nil, &typ, nil, &size) != nil || size == 0 {
		return ""
	}
	buf := make([]uint16, size/2+1)
	if syscall.RegQueryValueEx(k, n, nil, &typ, (*byte)(unsafe.Pointer(&buf[0])), &size) != nil {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

func regDWORD(root syscall.Handle, path, name string) (uint32, bool) {
	var k syscall.Handle
	p, _ := syscall.UTF16PtrFromString(path)
	if syscall.RegOpenKeyEx(root, p, 0, syscall.KEY_READ, &k) != nil {
		return 0, false
	}
	defer syscall.RegCloseKey(k)
	n, _ := syscall.UTF16PtrFromString(name)
	var typ, v uint32
	size := uint32(4)
	if syscall.RegQueryValueEx(k, n, nil, &typ, (*byte)(unsafe.Pointer(&v)), &size) != nil {
		return 0, false
	}
	return v, true
}

func regStr(root syscall.Handle, path, name string) string {
	var k syscall.Handle
	p, _ := syscall.UTF16PtrFromString(path)
	if syscall.RegOpenKeyEx(root, p, 0, syscall.KEY_READ, &k) != nil {
		return ""
	}
	defer syscall.RegCloseKey(k)
	return regString(k, name)
}

func openPawnModule(blob []byte) (*pawnModule, error) {
	h, err := w32.CreateFile(`\\?\GLOBALROOT\Device\PawnIO`, w32.GENERIC_READ|w32.GENERIC_WRITE,
		w32.FILE_SHARE_READ|w32.FILE_SHARE_WRITE, w32.OPEN_EXISTING, w32.FILE_ATTRIBUTE_NORMAL)
	if err != nil {
		return nil, err
	}
	if _, err := w32.DeviceIoControl(h, ioctlPawnLoad, blob, nil); err != nil {
		w32.CloseHandle(h)
		return nil, err
	}
	return &pawnModule{h: h}, nil
}

// exec calls a module function with int64 arguments.
func (m *pawnModule) exec(fn string, args []uint64, outN int) ([]uint64, error) {
	if m == nil || m.h == 0 {
		return nil, errPawnMissing
	}
	need := pawnFnNameLen + 8*len(args)
	if cap(m.in) < need {
		m.in = make([]byte, need)
	}
	in := m.in[:need]
	for i := range in[:pawnFnNameLen] {
		in[i] = 0
	}
	copy(in[:pawnFnNameLen-1], fn)
	for i, a := range args {
		binary.LittleEndian.PutUint64(in[pawnFnNameLen+8*i:], a)
	}
	if cap(m.out) < 8*outN {
		m.out = make([]byte, 8*outN)
	}
	out := m.out[:8*outN]
	n, err := w32.DeviceIoControl(m.h, ioctlPawnExec, in, out)
	if err != nil {
		return nil, err
	}
	res := make([]uint64, n/8)
	for i := range res {
		res[i] = binary.LittleEndian.Uint64(out[8*i:])
	}
	return res, nil
}

func (m *pawnModule) readMSR(idx uint32) (uint64, bool) {
	r, err := m.exec("ioctl_read_msr", []uint64{uint64(idx)}, 1)
	if err != nil || len(r) < 1 {
		return 0, false
	}
	return r[0], true
}

func (m *pawnModule) close() {
	if m != nil && m.h != 0 {
		w32.CloseHandle(m.h)
		m.h = 0
	}
}

// ---- cross-tool bus mutexes (shared with HWiNFO, LHM, etc.) ----

type globalMutex struct{ h w32.HANDLE }

func newGlobalMutex(name string) *globalMutex {
	h, _ := w32.CreateMutex(name)
	return &globalMutex{h: h}
}

func (g *globalMutex) lock(timeout time.Duration) bool {
	if g == nil || g.h == 0 {
		return true
	}
	r, _ := syscall.WaitForSingleObject(syscall.Handle(g.h), uint32(timeout/time.Millisecond))
	return r == syscall.WAIT_OBJECT_0 || r == 0x80 /*WAIT_ABANDONED*/
}

var procReleaseMutex = syscall.NewLazyDLL("kernel32.dll").NewProc("ReleaseMutex")

func (g *globalMutex) unlock() {
	if g != nil && g.h != 0 {
		procReleaseMutex.Call(g.h)
	}
}

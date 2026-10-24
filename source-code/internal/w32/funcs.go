//go:build windows

package w32

import (
	"sync"
	"syscall"
	"unsafe"
)

func GetModuleHandle() HANDLE {
	r, _, _ := procGetModuleHandleW.Call(0)
	return r
}

// CreateMutex returns the handle and whether the mutex already existed.
func CreateMutex(name string) (HANDLE, bool) {
	r, _, err := procCreateMutexW.Call(0, 0, UPtr(name))
	return r, IsErr(err, syscall.ERROR_ALREADY_EXISTS)
}

func CloseHandle(h HANDLE) { syscall.CloseHandle(syscall.Handle(h)) }

func QPC() int64 {
	var v int64
	procQueryPerformanceCounter.Call(uintptr(unsafe.Pointer(&v)))
	return v
}

func QPF() int64 {
	var v int64
	procQueryPerformanceFrequency.Call(uintptr(unsafe.Pointer(&v)))
	return v
}

func TickCount64() uint64 {
	r, _, _ := procGetTickCount64.Call()
	return uint64(r)
}

func GlobalMemoryStatus() (MEMORYSTATUSEX, bool) {
	var m MEMORYSTATUSEX
	m.Length = uint32(unsafe.Sizeof(m))
	r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&m)))
	return m, r != 0
}

func GetSystemTimes() (idle, kernel, user uint64, ok bool) {
	var i, k, u FILETIME
	r, _, _ := procGetSystemTimes.Call(uintptr(unsafe.Pointer(&i)), uintptr(unsafe.Pointer(&k)), uintptr(unsafe.Pointer(&u)))
	return i.U64(), k.U64(), u.U64(), r != 0
}

func GetSystemPowerStatus() (SYSTEM_POWER_STATUS, bool) {
	var s SYSTEM_POWER_STATUS
	r, _, _ := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&s)))
	return s, r != 0
}

func ActiveProcessorCount() int {
	r, _, _ := procGetActiveProcessorCount.Call(0xFFFF) // ALL_PROCESSOR_GROUPS
	return int(r)
}

const (
	GENERIC_READ          = 0x80000000
	GENERIC_WRITE         = 0x40000000
	FILE_SHARE_READ       = 0x1
	FILE_SHARE_WRITE      = 0x2
	OPEN_EXISTING         = 3
	FILE_ATTRIBUTE_NORMAL = 0x80
	INVALID_HANDLE        = ^uintptr(0)
)

func CreateFile(name string, access, share, disposition, flags uint32) (HANDLE, error) {
	r, _, err := procCreateFileW.Call(UPtr(name), uintptr(access), uintptr(share), 0, uintptr(disposition), uintptr(flags), 0)
	if r == INVALID_HANDLE {
		return 0, err
	}
	return r, nil
}

func DeviceIoControl(h HANDLE, code uint32, in []byte, out []byte) (uint32, error) {
	var ret uint32
	var pin, pout uintptr
	if len(in) > 0 {
		pin = uintptr(unsafe.Pointer(&in[0]))
	}
	if len(out) > 0 {
		pout = uintptr(unsafe.Pointer(&out[0]))
	}
	r, _, err := procDeviceIoControl.Call(h, uintptr(code), pin, uintptr(len(in)), pout, uintptr(len(out)), uintptr(unsafe.Pointer(&ret)), 0)
	if r == 0 {
		return ret, err
	}
	return ret, nil
}

// SetThreadAffinity sets the affinity of the calling OS thread and returns
// the previous mask (0 on failure).
func SetThreadAffinity(mask uintptr) uintptr {
	th, _, _ := procGetCurrentThread.Call()
	r, _, _ := procSetThreadAffinityMask.Call(th, mask)
	return r
}

const (
	PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
	PROCESS_SYNCHRONIZE               = 0x00100000
)

func OpenProcess(access uint32, pid uint32) HANDLE {
	r, _, _ := procOpenProcess.Call(uintptr(access), 0, uintptr(pid))
	return r
}

func ProcessImagePath(pid uint32) string {
	h := OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, pid)
	if h == 0 {
		return ""
	}
	defer CloseHandle(h)
	buf := make([]uint16, 1024)
	n := uint32(len(buf))
	r, _, _ := procQueryFullProcessImageNameW.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}

// ProcessNames returns pid -> exe name for all processes (no handles needed,
// so it also works for protected game processes).
func ProcessNames() map[uint32]string {
	const TH32CS_SNAPPROCESS = 0x2
	m := make(map[uint32]string, 256)
	snap, _, _ := procCreateToolhelp32Snapshot.Call(TH32CS_SNAPPROCESS, 0)
	if snap == INVALID_HANDLE || snap == 0 {
		return m
	}
	defer CloseHandle(snap)
	var e PROCESSENTRY32W
	e.Size = uint32(unsafe.Sizeof(e))
	r, _, _ := procProcess32FirstW.Call(snap, uintptr(unsafe.Pointer(&e)))
	for r != 0 {
		m[e.ProcessID] = syscall.UTF16ToString(e.ExeFile[:])
		r, _, _ = procProcess32NextW.Call(snap, uintptr(unsafe.Pointer(&e)))
	}
	return m
}

// CoreInfo describes one physical core (processor group 0 only).
type CoreInfo struct {
	Mask       uint64 // logical processors of the core
	Efficiency int    // EfficiencyClass: higher = faster (P-core) on hybrid CPUs
}

// Cores enumerates physical cores via GetLogicalProcessorInformationEx.
func Cores() []CoreInfo {
	const RelationProcessorCore = 0
	var out []CoreInfo
	var size uint32
	procGetLogicalProcessorInformationEx.Call(RelationProcessorCore, 0, uintptr(unsafe.Pointer(&size)))
	if size == 0 {
		return out
	}
	buf := make([]byte, size)
	r, _, _ := procGetLogicalProcessorInformationEx.Call(RelationProcessorCore, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r == 0 {
		return out
	}
	// SYSTEM_LOGICAL_PROCESSOR_INFORMATION_EX { DWORD Relationship; DWORD Size;
	//   PROCESSOR_RELATIONSHIP { BYTE Flags; BYTE EfficiencyClass; BYTE Reserved[20];
	//   WORD GroupCount; GROUP_AFFINITY GroupMask[] { KAFFINITY Mask; WORD Group; WORD Reserved[3]; } } }
	off := 0
	for off+8 <= int(size) {
		recSize := int(*(*uint32)(unsafe.Pointer(&buf[off+4])))
		if recSize <= 0 {
			break
		}
		eff := int(buf[off+8+1])
		groupCount := int(*(*uint16)(unsafe.Pointer(&buf[off+8+22])))
		gm := off + 8 + 24
		for g := 0; g < groupCount && gm+16 <= off+recSize; g++ {
			mask := *(*uint64)(unsafe.Pointer(&buf[gm]))
			group := *(*uint16)(unsafe.Pointer(&buf[gm+8]))
			if group == 0 && mask != 0 {
				out = append(out, CoreInfo{Mask: mask, Efficiency: eff})
			}
			gm += 16
		}
		off += recSize
	}
	return out
}

const LOAD_LIBRARY_SEARCH_SYSTEM32 = 0x800
const LOAD_WITH_ALTERED_SEARCH_PATH = 0x8

// LoadDLL loads a DLL from an absolute path, or System32 when only a name is given.
func LoadDLL(pathOrName string, absolute bool) (*syscall.DLL, error) {
	flags := uintptr(LOAD_LIBRARY_SEARCH_SYSTEM32)
	if absolute {
		flags = LOAD_WITH_ALTERED_SEARCH_PATH
	}
	h, _, err := procLoadLibraryExW.Call(UPtr(pathOrName), 0, flags)
	if h == 0 {
		return nil, err
	}
	return &syscall.DLL{Name: pathOrName, Handle: syscall.Handle(h)}, nil
}

// ---- windows & messages ----

func RegisterClass(wc *WNDCLASSEXW) uint16 {
	wc.Size = uint32(unsafe.Sizeof(*wc))
	r, _, _ := procRegisterClassExW.Call(uintptr(unsafe.Pointer(wc)))
	return uint16(r)
}

func CreateWindow(exStyle uint32, class, title string, style uint32, x, y, w, h int32, parent HWND, inst HANDLE) HWND {
	r, _, _ := procCreateWindowExW.Call(uintptr(exStyle), UPtr(class), UPtr(title), uintptr(style),
		uintptr(x), uintptr(y), uintptr(w), uintptr(h), parent, 0, inst, 0)
	return r
}

func DestroyWindow(h HWND) { procDestroyWindow.Call(h) }

func DefWindowProc(h HWND, msg uint32, w, l uintptr) uintptr {
	r, _, _ := procDefWindowProcW.Call(h, uintptr(msg), w, l)
	return r
}

func GetMessage(m *MSG) int32 {
	r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(m)), 0, 0, 0)
	return int32(r)
}

func TranslateMessage(m *MSG) { procTranslateMessage.Call(uintptr(unsafe.Pointer(m))) }
func DispatchMessage(m *MSG)  { procDispatchMessageW.Call(uintptr(unsafe.Pointer(m))) }

func PostMessage(h HWND, msg uint32, w, l uintptr) bool {
	r, _, _ := procPostMessageW.Call(h, uintptr(msg), w, l)
	return r != 0
}

func PostQuitMessage(code int) { procPostQuitMessage.Call(uintptr(code)) }

func ShowWindow(h HWND, cmd int32) { procShowWindow.Call(h, uintptr(cmd)) }

func SetWindowPos(h, after HWND, x, y, w, hh int32, flags uint32) bool {
	r, _, _ := procSetWindowPos.Call(h, after, uintptr(x), uintptr(y), uintptr(w), uintptr(hh), uintptr(flags))
	return r != 0
}

func GetWindowRect(h HWND) RECT {
	var r RECT
	procGetWindowRect.Call(h, uintptr(unsafe.Pointer(&r)))
	return r
}

func GetCursorPos() POINT {
	var p POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return p
}

func UpdateLayeredWindow(h HWND, dst *POINT, size *SIZE, srcDC HDC, alpha byte) bool {
	var src POINT
	bf := BLENDFUNCTION{BlendOp: AC_SRC_OVER, SourceConstantAlpha: alpha, AlphaFormat: AC_SRC_ALPHA}
	r, _, _ := procUpdateLayeredWindow.Call(h, 0, uintptr(unsafe.Pointer(dst)), uintptr(unsafe.Pointer(size)),
		srcDC, uintptr(unsafe.Pointer(&src)), 0, uintptr(unsafe.Pointer(&bf)), ULW_ALPHA)
	return r != 0
}

func SetWindowLongPtr(h HWND, idx int32, v uintptr) uintptr {
	r, _, _ := procSetWindowLongPtrW.Call(h, uintptr(idx), v)
	return r
}

func GetWindowLongPtr(h HWND, idx int32) uintptr {
	r, _, _ := procGetWindowLongPtrW.Call(h, uintptr(idx))
	return r
}

func SetTimer(h HWND, id uintptr, ms uint32) { procSetTimer.Call(h, id, uintptr(ms), 0) }
func KillTimer(h HWND, id uintptr)          { procKillTimer.Call(h, id) }

func RegisterHotKey(h HWND, id int32, mods, vk uint32) bool {
	r, _, _ := procRegisterHotKey.Call(h, uintptr(id), uintptr(mods), uintptr(vk))
	return r != 0
}

func UnregisterHotKey(h HWND, id int32) { procUnregisterHotKey.Call(h, uintptr(id)) }

func GetForegroundWindow() HWND {
	r, _, _ := procGetForegroundWindow.Call()
	return r
}

func SetForegroundWindow(h HWND) { procSetForegroundWindow.Call(h) }

func WindowPID(h HWND) uint32 {
	var pid uint32
	procGetWindowThreadProcessId.Call(h, uintptr(unsafe.Pointer(&pid)))
	return pid
}

func MonitorFromWindow(h HWND, flags uint32) HMONITOR {
	r, _, _ := procMonitorFromWindow.Call(h, uintptr(flags))
	return r
}

func MonitorFromPoint(p POINT, flags uint32) HMONITOR {
	r, _, _ := procMonitorFromPoint.Call(uintptr(uint32(p.X))|uintptr(uint32(p.Y))<<32, uintptr(flags))
	return r
}

func MonitorFromRect(rc RECT, flags uint32) HMONITOR {
	r, _, _ := procMonitorFromRect.Call(uintptr(unsafe.Pointer(&rc)), uintptr(flags))
	return r
}

func GetMonitorInfo(m HMONITOR) (MONITORINFOEXW, bool) {
	var mi MONITORINFOEXW
	mi.Size = uint32(unsafe.Sizeof(mi))
	r, _, _ := procGetMonitorInfoW.Call(m, uintptr(unsafe.Pointer(&mi)))
	return mi, r != 0
}

var enumMonitorsCb = syscall.NewCallback(func(hm, hdc, rc, data uintptr) uintptr {
	list := (*[]HMONITOR)(unsafe.Pointer(data))
	*list = append(*list, hm)
	return 1
})

func EnumMonitors() []HMONITOR {
	var list []HMONITOR
	procEnumDisplayMonitors.Call(0, 0, enumMonitorsCb, uintptr(unsafe.Pointer(&list)))
	return list
}

func GetDpiForMonitor(m HMONITOR) uint32 {
	var x, y uint32
	r, _, _ := procGetDpiForMonitor.Call(m, MDT_EFFECTIVE_DPI, uintptr(unsafe.Pointer(&x)), uintptr(unsafe.Pointer(&y)))
	if r != 0 || x == 0 {
		return 96
	}
	return x
}

func GetDC(h HWND) HDC              { r, _, _ := procGetDC.Call(h); return r }
func ReleaseDC(h HWND, dc HDC)      { procReleaseDC.Call(h, dc) }
func CreateCompatibleDC(dc HDC) HDC { r, _, _ := procCreateCompatibleDC.Call(dc); return r }
func DeleteDC(dc HDC)               { procDeleteDC.Call(dc) }
func SelectObject(dc HDC, o uintptr) uintptr {
	r, _, _ := procSelectObject.Call(dc, o)
	return r
}
func DeleteObject(o uintptr) { procDeleteObject.Call(o) }

// CreateDIB creates a top-down 32bpp DIB section and returns it with its bits.
func CreateDIB(dc HDC, w, h int32) (uintptr, unsafe.Pointer) {
	var bi BITMAPINFO
	bi.Header.Size = uint32(unsafe.Sizeof(bi.Header))
	bi.Header.Width = w
	bi.Header.Height = -h
	bi.Header.Planes = 1
	bi.Header.BitCount = 32
	var bits unsafe.Pointer
	r, _, _ := procCreateDIBSection.Call(dc, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	return r, bits
}

func CreatePopupMenu() HMENU { r, _, _ := procCreatePopupMenu.Call(); return r }
func AppendMenu(m HMENU, flags uint32, id uintptr, text string) {
	if flags&MF_SEPARATOR != 0 {
		procAppendMenuW.Call(m, uintptr(flags), 0, 0)
		return
	}
	procAppendMenuW.Call(m, uintptr(flags), id, UPtr(text))
}
func TrackPopupMenu(m HMENU, flags uint32, x, y int32, h HWND) int {
	r, _, _ := procTrackPopupMenu.Call(m, uintptr(flags), uintptr(x), uintptr(y), 0, h, 0)
	return int(r)
}
func DestroyMenu(m HMENU) { procDestroyMenu.Call(m) }

func LoadIconRes(inst HANDLE, id uintptr, cx, cy int32) HICON {
	r, _, _ := procLoadImageW.Call(inst, id, IMAGE_ICON, uintptr(cx), uintptr(cy), LR_DEFAULTCOLOR)
	return r
}

func DestroyIcon(h HICON) {
	if h != 0 {
		procDestroyIcon.Call(h)
	}
}

func LoadCursor(id uintptr) uintptr { r, _, _ := procLoadCursorW.Call(0, id); return r }
func SetCursor(c uintptr)           { procSetCursor.Call(c) }

func RegisterWindowMessage(name string) uint32 {
	r, _, _ := procRegisterWindowMessageW.Call(UPtr(name))
	return uint32(r)
}

// AllowMessage lets lower-integrity processes (explorer, a second
// unelevated instance) post msg to our elevated window.
func AllowMessage(h HWND, msg uint32) {
	procChangeWindowMessageFilterEx.Call(h, uintptr(msg), MSGFLT_ALLOW, 0)
}

func SetWinEventHook(evMin, evMax uint32, cb uintptr, flags uint32) HANDLE {
	r, _, _ := procSetWinEventHook.Call(uintptr(evMin), uintptr(evMax), 0, cb, 0, 0, uintptr(flags))
	return r
}

func UnhookWinEvent(h HANDLE) { procUnhookWinEvent.Call(h) }

func GetShellWindow() HWND { r, _, _ := procGetShellWindow.Call(); return r }

func GetClassName(h HWND) string {
	buf := make([]uint16, 256)
	r, _, _ := procGetClassNameW.Call(h, uintptr(unsafe.Pointer(&buf[0])), 256)
	return syscall.UTF16ToString(buf[:r])
}

var enumChildCb = syscall.NewCallback(func(h, data uintptr) uintptr {
	list := (*[]HWND)(unsafe.Pointer(data))
	*list = append(*list, h)
	return 1
})

func ChildWindows(parent HWND) []HWND {
	var list []HWND
	procEnumChildWindows.Call(parent, enumChildCb, uintptr(unsafe.Pointer(&list)))
	return list
}

func IsWindowVisible(h HWND) bool { r, _, _ := procIsWindowVisible.Call(h); return r != 0 }
func IsIconic(h HWND) bool        { r, _, _ := procIsIconic.Call(h); return r != 0 }

func IsCloaked(h HWND) bool {
	var v uint32
	r, _, _ := procDwmGetWindowAttribute.Call(h, DWMWA_CLOAKED, uintptr(unsafe.Pointer(&v)), 4)
	return r == 0 && v != 0
}

func GetSystemMetrics(idx int32) int32 {
	r, _, _ := procGetSystemMetrics.Call(uintptr(idx))
	return int32(r)
}

func GetSystemMetricsForDpi(idx int32, dpi uint32) int32 {
	if procGetSystemMetricsForDpi.Find() != nil {
		return GetSystemMetrics(idx) * int32(dpi) / 96
	}
	r, _, _ := procGetSystemMetricsForDpi.Call(uintptr(idx), uintptr(dpi))
	return int32(r)
}

func FindWindow(class string) HWND {
	r, _, _ := procFindWindowW.Call(UPtr(class), 0)
	return r
}

func SetWindowDisplayAffinity(h HWND, v uint32) bool {
	r, _, _ := procSetWindowDisplayAffinity.Call(h, uintptr(v))
	return r != 0
}

func MessageBox(h HWND, text, title string, flags uint32) int {
	r, _, _ := procMessageBoxW.Call(h, UPtr(text), UPtr(title), uintptr(flags))
	return int(r)
}

func GetAncestor(h HWND, flags uint32) HWND {
	r, _, _ := procGetAncestor.Call(h, uintptr(flags))
	return r
}

func KeyDown(vk int32) bool {
	r, _, _ := procGetKeyState.Call(uintptr(vk))
	return int16(r) < 0
}

func TrackMouseLeave(h HWND) {
	t := TRACKMOUSEEVENT{Flags: TME_LEAVE, Track: h}
	t.Size = uint32(unsafe.Sizeof(t))
	procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&t)))
}

// ---- tray ----

func ShellNotifyIcon(msg uint32, nid *NOTIFYICONDATAW) bool {
	nid.Size = uint32(unsafe.Sizeof(*nid))
	r, _, _ := procShellNotifyIconW.Call(uintptr(msg), uintptr(unsafe.Pointer(nid)))
	return r != 0
}

func ShellNotifyIconGetRect(h HWND, id uint32) (RECT, bool) {
	ni := NOTIFYICONIDENTIFIER{Wnd: h, ID: id}
	ni.Size = uint32(unsafe.Sizeof(ni))
	var rc RECT
	r, _, _ := procShellNotifyIconGetRect.Call(uintptr(unsafe.Pointer(&ni)), uintptr(unsafe.Pointer(&rc)))
	return rc, r == 0 // S_OK
}

const SEE_MASK_NOCLOSEPROCESS = 0x40

// ShellExecute runs verb on file. For "runas" it returns ERROR_CANCELLED
// when the user declines the UAC prompt.
func ShellExecute(verb, file, params, dir string, show int32) error {
	sei := SHELLEXECUTEINFOW{Verb: Ptr(verb), File: Ptr(file), Parameters: Ptr(params), Show: show}
	if dir != "" {
		sei.Directory = Ptr(dir)
	}
	sei.Size = uint32(unsafe.Sizeof(sei))
	r, _, err := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&sei)))
	if r == 0 {
		return err
	}
	return nil
}

// ---- tokens / processes ----

const (
	TOKEN_QUERY          = 0x0008
	TOKEN_DUPLICATE      = 0x0002
	TOKEN_ASSIGN_PRIMARY = 0x0001
	TOKEN_ADJUST_DEFAULT = 0x0080
	TOKEN_ADJUST_SESSIONID = 0x0100
	TokenElevation       = 20
)

func IsElevated() bool {
	cur, _ := syscall.GetCurrentProcess()
	var tok syscall.Token
	if err := syscall.OpenProcessToken(cur, TOKEN_QUERY, &tok); err != nil {
		return false
	}
	defer tok.Close()
	var elev uint32
	var n uint32
	r, _, _ := procGetTokenInformation.Call(uintptr(tok), TokenElevation, uintptr(unsafe.Pointer(&elev)), 4, uintptr(unsafe.Pointer(&n)))
	return r != 0 && elev != 0
}

// StartUnelevated launches cmdLine with the desktop shell's (non-elevated)
// token. Works when we are elevated; returns an error otherwise.
func StartUnelevated(app, cmdLine string) error {
	shell := GetShellWindow()
	if shell == 0 {
		return syscall.ERROR_NOT_FOUND
	}
	pid := WindowPID(shell)
	hp := OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, pid)
	if hp == 0 {
		return syscall.ERROR_ACCESS_DENIED
	}
	defer CloseHandle(hp)
	var tok uintptr
	r, _, err := procOpenProcessToken.Call(hp, TOKEN_DUPLICATE, uintptr(unsafe.Pointer(&tok)))
	if r == 0 {
		return err
	}
	defer CloseHandle(tok)
	var dup uintptr
	r, _, err = procDuplicateTokenEx.Call(tok, TOKEN_QUERY|TOKEN_ASSIGN_PRIMARY|TOKEN_DUPLICATE|TOKEN_ADJUST_DEFAULT|TOKEN_ADJUST_SESSIONID,
		0, 2 /*SecurityImpersonation*/, 1 /*TokenPrimary*/, uintptr(unsafe.Pointer(&dup)))
	if r == 0 {
		return err
	}
	defer CloseHandle(dup)
	var si STARTUPINFOW
	si.Cb = uint32(unsafe.Sizeof(si))
	var pi PROCESS_INFORMATION
	cmd, _ := syscall.UTF16FromString(cmdLine)
	var appPtr uintptr
	if app != "" {
		appPtr = UPtr(app)
	}
	r, _, err = procCreateProcessWithTokenW.Call(dup, 0, appPtr, uintptr(unsafe.Pointer(&cmd[0])), 0, 0, 0,
		uintptr(unsafe.Pointer(&si)), uintptr(unsafe.Pointer(&pi)))
	if r == 0 {
		return err
	}
	CloseHandle(pi.Thread)
	CloseHandle(pi.Process)
	return nil
}

// StartProcess launches cmdLine with our own token.
func StartProcess(app, cmdLine string, hidden bool) (HANDLE, error) {
	var si STARTUPINFOW
	si.Cb = uint32(unsafe.Sizeof(si))
	var pi PROCESS_INFORMATION
	cmd, _ := syscall.UTF16FromString(cmdLine)
	var appPtr uintptr
	if app != "" {
		appPtr = UPtr(app)
	}
	flags := uintptr(0)
	if hidden {
		flags = 0x08000000 // CREATE_NO_WINDOW
	}
	r, _, err := procCreateProcessW.Call(appPtr, uintptr(unsafe.Pointer(&cmd[0])), 0, 0, 0, flags, 0, 0,
		uintptr(unsafe.Pointer(&si)), uintptr(unsafe.Pointer(&pi)))
	if r == 0 {
		return 0, err
	}
	CloseHandle(pi.Thread)
	return pi.Process, nil
}

func WaitProcess(h HANDLE, ms uint32) uint32 {
	procWaitForSingleObject.Call(h, uintptr(ms))
	var code uint32
	syscall.GetExitCodeProcess(syscall.Handle(h), &code)
	return code
}

// ---- fonts ----

var (
	fontNames map[string]bool
	fontMu    sync.Mutex
)

var enumFontCb = syscall.NewCallback(func(lf, tm, fontType, data uintptr) uintptr {
	l := (*LOGFONTW)(unsafe.Pointer(lf))
	name := syscall.UTF16ToString(l.FaceName[:])
	if name != "" && name[0] != '@' {
		fontNames[name] = true
	}
	return 1
})

// FontFamilies lists installed font family names.
func FontFamilies() []string {
	fontMu.Lock()
	defer fontMu.Unlock()
	fontNames = map[string]bool{}
	dc := GetDC(0)
	defer ReleaseDC(0, dc)
	var lf LOGFONTW
	lf.CharSet = 1 // DEFAULT_CHARSET
	procEnumFontFamiliesExW.Call(dc, uintptr(unsafe.Pointer(&lf)), enumFontCb, 0, 0)
	out := make([]string, 0, len(fontNames))
	for k := range fontNames {
		out = append(out, k)
	}
	return out
}

// FindWindowTitle finds a top-level window by exact title.
func FindWindowTitle(title string) HWND {
	r, _, _ := procFindWindowW.Call(0, UPtr(title))
	return r
}

var procShowWindowAsync = user32.NewProc("ShowWindowAsync")

// Restore un-minimises and focuses a window.
func Restore(h HWND) {
	if IsIconic(h) {
		procShowWindowAsync.Call(h, 9) // SW_RESTORE
	}
	SetForegroundWindow(h)
}

var (
	procGetProcessTimes        = kernel32.NewProc("GetProcessTimes")
	procK32GetProcessMemoryInfo = kernel32.NewProc("K32GetProcessMemoryInfo")
)

// SelfUsage returns this process's total CPU time (100ns units) and its
// private memory / working set in bytes.
func SelfUsage() (cpu100ns uint64, private, workingSet uint64) {
	cur, _ := syscall.GetCurrentProcess()
	var c, e, k, u FILETIME
	procGetProcessTimes.Call(uintptr(cur), uintptr(unsafe.Pointer(&c)), uintptr(unsafe.Pointer(&e)), uintptr(unsafe.Pointer(&k)), uintptr(unsafe.Pointer(&u)))
	var pmc struct {
		Cb                         uint32
		PageFaultCount             uint32
		PeakWorkingSetSize         uintptr
		WorkingSetSize             uintptr
		QuotaPeakPagedPoolUsage    uintptr
		QuotaPagedPoolUsage        uintptr
		QuotaPeakNonPagedPoolUsage uintptr
		QuotaNonPagedPoolUsage     uintptr
		PagefileUsage              uintptr
		PeakPagefileUsage          uintptr
		PrivateUsage               uintptr
	}
	pmc.Cb = uint32(unsafe.Sizeof(pmc))
	procK32GetProcessMemoryInfo.Call(uintptr(cur), uintptr(unsafe.Pointer(&pmc)), uintptr(pmc.Cb))
	return k.U64() + u.U64(), uint64(pmc.PrivateUsage), uint64(pmc.WorkingSetSize)
}

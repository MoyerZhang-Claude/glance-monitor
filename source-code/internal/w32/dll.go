//go:build windows

// Package w32 contains the small subset of Win32 bindings Glance needs,
// written against the standard library only (no x/sys dependency).
package w32

import (
	"syscall"
	"unsafe"
)

const loadLibrarySearchSystem32 = 0x00000800

func init() {
	// Restrict implicit DLL search to System32 before any lazy DLL is
	// loaded, so a planted pdh.dll / gdiplus.dll next to the exe can never
	// be picked up.
	k := syscall.NewLazyDLL("kernel32.dll")
	if p := k.NewProc("SetDefaultDllDirectories"); p.Find() == nil {
		p.Call(loadLibrarySearchSystem32)
	}
}

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
	shcore   = syscall.NewLazyDLL("shcore.dll")
	dwmapi   = syscall.NewLazyDLL("dwmapi.dll")

	procGetModuleHandleW               = kernel32.NewProc("GetModuleHandleW")
	procCreateMutexW                   = kernel32.NewProc("CreateMutexW")
	procQueryPerformanceCounter        = kernel32.NewProc("QueryPerformanceCounter")
	procQueryPerformanceFrequency      = kernel32.NewProc("QueryPerformanceFrequency")
	procGlobalMemoryStatusEx           = kernel32.NewProc("GlobalMemoryStatusEx")
	procGetSystemTimes                 = kernel32.NewProc("GetSystemTimes")
	procCreateFileW                    = kernel32.NewProc("CreateFileW")
	procDeviceIoControl                = kernel32.NewProc("DeviceIoControl")
	procSetThreadAffinityMask          = kernel32.NewProc("SetThreadAffinityMask")
	procGetCurrentThread               = kernel32.NewProc("GetCurrentThread")
	procOpenProcess                    = kernel32.NewProc("OpenProcess")
	procQueryFullProcessImageNameW     = kernel32.NewProc("QueryFullProcessImageNameW")
	procCreateToolhelp32Snapshot       = kernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstW                = kernel32.NewProc("Process32FirstW")
	procProcess32NextW                 = kernel32.NewProc("Process32NextW")
	procGetLogicalProcessorInformationEx = kernel32.NewProc("GetLogicalProcessorInformationEx")
	procLoadLibraryExW                 = kernel32.NewProc("LoadLibraryExW")
	procGetSystemPowerStatus           = kernel32.NewProc("GetSystemPowerStatus")
	procGetTickCount64                 = kernel32.NewProc("GetTickCount64")
	procCreateProcessW                 = kernel32.NewProc("CreateProcessW")
	procGetActiveProcessorCount        = kernel32.NewProc("GetActiveProcessorCount")
	procWaitForSingleObject            = kernel32.NewProc("WaitForSingleObject")

	procRegisterClassExW            = user32.NewProc("RegisterClassExW")
	procCreateWindowExW             = user32.NewProc("CreateWindowExW")
	procDestroyWindow               = user32.NewProc("DestroyWindow")
	procDefWindowProcW              = user32.NewProc("DefWindowProcW")
	procGetMessageW                 = user32.NewProc("GetMessageW")
	procTranslateMessage            = user32.NewProc("TranslateMessage")
	procDispatchMessageW            = user32.NewProc("DispatchMessageW")
	procPostMessageW                = user32.NewProc("PostMessageW")
	procPostQuitMessage             = user32.NewProc("PostQuitMessage")
	procShowWindow                  = user32.NewProc("ShowWindow")
	procSetWindowPos                = user32.NewProc("SetWindowPos")
	procGetWindowRect               = user32.NewProc("GetWindowRect")
	procGetCursorPos                = user32.NewProc("GetCursorPos")
	procUpdateLayeredWindow         = user32.NewProc("UpdateLayeredWindow")
	procSetWindowLongPtrW           = user32.NewProc("SetWindowLongPtrW")
	procGetWindowLongPtrW           = user32.NewProc("GetWindowLongPtrW")
	procSetTimer                    = user32.NewProc("SetTimer")
	procKillTimer                   = user32.NewProc("KillTimer")
	procRegisterHotKey              = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey            = user32.NewProc("UnregisterHotKey")
	procGetForegroundWindow         = user32.NewProc("GetForegroundWindow")
	procSetForegroundWindow         = user32.NewProc("SetForegroundWindow")
	procGetWindowThreadProcessId    = user32.NewProc("GetWindowThreadProcessId")
	procMonitorFromWindow           = user32.NewProc("MonitorFromWindow")
	procMonitorFromPoint            = user32.NewProc("MonitorFromPoint")
	procMonitorFromRect             = user32.NewProc("MonitorFromRect")
	procGetMonitorInfoW             = user32.NewProc("GetMonitorInfoW")
	procEnumDisplayMonitors         = user32.NewProc("EnumDisplayMonitors")
	procGetDC                       = user32.NewProc("GetDC")
	procReleaseDC                   = user32.NewProc("ReleaseDC")
	procCreatePopupMenu             = user32.NewProc("CreatePopupMenu")
	procAppendMenuW                 = user32.NewProc("AppendMenuW")
	procTrackPopupMenu              = user32.NewProc("TrackPopupMenu")
	procDestroyMenu                 = user32.NewProc("DestroyMenu")
	procLoadImageW                  = user32.NewProc("LoadImageW")
	procDestroyIcon                 = user32.NewProc("DestroyIcon")
	procLoadCursorW                 = user32.NewProc("LoadCursorW")
	procRegisterWindowMessageW      = user32.NewProc("RegisterWindowMessageW")
	procChangeWindowMessageFilterEx = user32.NewProc("ChangeWindowMessageFilterEx")
	procSetWinEventHook             = user32.NewProc("SetWinEventHook")
	procUnhookWinEvent              = user32.NewProc("UnhookWinEvent")
	procGetShellWindow              = user32.NewProc("GetShellWindow")
	procGetClassNameW               = user32.NewProc("GetClassNameW")
	procEnumChildWindows            = user32.NewProc("EnumChildWindows")
	procIsWindowVisible             = user32.NewProc("IsWindowVisible")
	procIsIconic                    = user32.NewProc("IsIconic")
	procGetSystemMetrics            = user32.NewProc("GetSystemMetrics")
	procGetSystemMetricsForDpi      = user32.NewProc("GetSystemMetricsForDpi")
	procFindWindowW                 = user32.NewProc("FindWindowW")
	procSetWindowDisplayAffinity    = user32.NewProc("SetWindowDisplayAffinity")
	procMessageBoxW                 = user32.NewProc("MessageBoxW")
	procGetAncestor                 = user32.NewProc("GetAncestor")
	procGetDpiForWindow             = user32.NewProc("GetDpiForWindow")
	procInvalidateRect              = user32.NewProc("InvalidateRect")
	procGetKeyState                 = user32.NewProc("GetKeyState")
	procSetCursor                   = user32.NewProc("SetCursor")
	procTrackMouseEvent             = user32.NewProc("TrackMouseEvent")

	procCreateCompatibleDC  = gdi32.NewProc("CreateCompatibleDC")
	procDeleteDC            = gdi32.NewProc("DeleteDC")
	procCreateDIBSection    = gdi32.NewProc("CreateDIBSection")
	procSelectObject        = gdi32.NewProc("SelectObject")
	procDeleteObject        = gdi32.NewProc("DeleteObject")
	procEnumFontFamiliesExW = gdi32.NewProc("EnumFontFamiliesExW")

	procShellNotifyIconW        = shell32.NewProc("Shell_NotifyIconW")
	procShellNotifyIconGetRect  = shell32.NewProc("Shell_NotifyIconGetRect")
	procShellExecuteExW         = shell32.NewProc("ShellExecuteExW")

	procOpenProcessToken        = advapi32.NewProc("OpenProcessToken")
	procGetTokenInformation     = advapi32.NewProc("GetTokenInformation")
	procDuplicateTokenEx        = advapi32.NewProc("DuplicateTokenEx")
	procCreateProcessWithTokenW = advapi32.NewProc("CreateProcessWithTokenW")

	procGetDpiForMonitor = shcore.NewProc("GetDpiForMonitor")

	procDwmGetWindowAttribute = dwmapi.NewProc("DwmGetWindowAttribute")
)

// Ptr converts a Go string to a NUL-terminated UTF-16 pointer.
func Ptr(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		p, _ = syscall.UTF16PtrFromString("")
	}
	return p
}

// UPtr is Ptr as a uintptr for passing to procs.
func UPtr(s string) uintptr { return uintptr(unsafe.Pointer(Ptr(s))) }

// Str converts a NUL-terminated UTF-16 buffer to a Go string.
func Str(b []uint16) string { return syscall.UTF16ToString(b) }

// StrPtr converts a NUL-terminated UTF-16 pointer to a Go string.
func StrPtr(p *uint16) string {
	if p == nil {
		return ""
	}
	n := 0
	for ptr := unsafe.Pointer(p); *(*uint16)(ptr) != 0; n++ {
		ptr = unsafe.Pointer(uintptr(ptr) + 2)
	}
	return syscall.UTF16ToString(unsafe.Slice(p, n))
}

func boolToUintptr(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

//go:build windows

package ui

import (
	"strconv"
	"syscall"
	"unsafe"

	"glance/internal/w32"
)

func ftoa(f float32) string { return strconv.FormatFloat(float64(f), 'f', 2, 32) }

func btoa(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// monitorFor returns the monitor handle for a device name ("" = primary).
func monitorFor(device string) w32.HMONITOR {
	if device != "" {
		for _, hm := range w32.EnumMonitors() {
			if mi, ok := w32.GetMonitorInfo(hm); ok && syscall.UTF16ToString(mi.Device[:]) == device {
				return hm
			}
		}
	}
	return w32.MonitorFromPoint(w32.POINT{}, w32.MONITOR_DEFAULTTOPRIMARY)
}

func isPrimary(hm w32.HMONITOR) bool {
	mi, ok := w32.GetMonitorInfo(hm)
	return ok && mi.Flags&1 != 0 // MONITORINFOF_PRIMARY
}

func monitorDevice(hm w32.HMONITOR) string {
	mi, ok := w32.GetMonitorInfo(hm)
	if !ok {
		return ""
	}
	return syscall.UTF16ToString(mi.Device[:])
}

// MonitorInfo is exposed to the settings page.
type MonitorInfo struct {
	Device  string `json:"device"`
	Primary bool   `json:"primary"`
	Width   int32  `json:"width"`
	Height  int32  `json:"height"`
	Scale   int    `json:"scale"`
}

func listMonitors() []MonitorInfo {
	var out []MonitorInfo
	for _, hm := range w32.EnumMonitors() {
		mi, ok := w32.GetMonitorInfo(hm)
		if !ok {
			continue
		}
		out = append(out, MonitorInfo{
			Device:  syscall.UTF16ToString(mi.Device[:]),
			Primary: mi.Flags&1 != 0,
			Width:   mi.Monitor.W(),
			Height:  mi.Monitor.H(),
			Scale:   int(w32.GetDpiForMonitor(hm)) * 100 / 96,
		})
	}
	return out
}

// lightTaskbar reports whether the Windows (system) theme is light.
func lightTaskbar() bool {
	var k syscall.Handle
	p, _ := syscall.UTF16PtrFromString(`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`)
	if syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, p, 0, syscall.KEY_READ, &k) != nil {
		return false
	}
	defer syscall.RegCloseKey(k)
	n, _ := syscall.UTF16PtrFromString("SystemUsesLightTheme")
	var typ, v uint32
	size := uint32(4)
	if syscall.RegQueryValueEx(k, n, nil, &typ, (*byte)(unsafe.Pointer(&v)), &size) != nil {
		return false
	}
	return v != 0
}

// isFullscreen reports whether hwnd covers its whole monitor (games in
// exclusive or borderless fullscreen), ignoring the desktop and shell.
func isFullscreen(h w32.HWND) bool {
	if h == 0 {
		return false
	}
	switch w32.GetClassName(h) {
	case "Progman", "WorkerW", "Shell_TrayWnd", "Shell_SecondaryTrayWnd":
		return false
	}
	if w32.IsIconic(h) || !w32.IsWindowVisible(h) {
		return false
	}
	rc := w32.GetWindowRect(h)
	mi, ok := w32.GetMonitorInfo(w32.MonitorFromWindow(h, w32.MONITOR_DEFAULTTONEAREST))
	if !ok {
		return false
	}
	m := mi.Monitor
	return rc.Left <= m.Left && rc.Top <= m.Top && rc.Right >= m.Right && rc.Bottom >= m.Bottom
}

func unsafePointer(p *uint16) unsafe.Pointer { return unsafe.Pointer(p) }

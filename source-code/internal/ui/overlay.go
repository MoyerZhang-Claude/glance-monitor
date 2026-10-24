//go:build windows

package ui

import (
	"math"
	"syscall"
	"unsafe"

	"glance/internal/config"
	"glance/internal/data"
	"glance/internal/layout"
	"glance/internal/w32"
)

const overlayClass = "GlanceHUD.Overlay"

type overlayWin struct {
	app      *App
	hwnd     w32.HWND
	rend     *overlayRenderer
	shown    bool
	editing  bool
	dragging bool
	pos      w32.POINT
	size     w32.SIZE
	scale    float32
	capture  bool
	empty    bool
}

var theOverlay *overlayWin

var overlayProc = syscall.NewCallback(func(h, msg, w, l uintptr) uintptr {
	if o := theOverlay; o != nil && o.hwnd == h {
		if r, ok := o.wndProc(uint32(msg), w, l); ok {
			return r
		}
	}
	return w32.DefWindowProc(h, uint32(msg), w, l)
})

func newOverlay(app *App) *overlayWin {
	o := &overlayWin{app: app, rend: newOverlayRenderer(), scale: 1}
	theOverlay = o
	wc := w32.WNDCLASSEXW{
		Style:     w32.CS_DBLCLKS,
		WndProc:   overlayProc,
		Instance:  app.inst,
		Cursor:    w32.LoadCursor(w32.IDC_ARROW),
		ClassName: w32.Ptr(overlayClass),
	}
	w32.RegisterClass(&wc)
	o.hwnd = w32.CreateWindow(o.exStyle(), overlayClass, "Glance Overlay", w32.WS_POPUP, 0, 0, 1, 1, 0, app.inst)
	return o
}

func (o *overlayWin) exStyle() uint32 {
	s := uint32(w32.WS_EX_LAYERED | w32.WS_EX_TOPMOST | w32.WS_EX_TOOLWINDOW | w32.WS_EX_NOACTIVATE)
	if !o.editing {
		s |= w32.WS_EX_TRANSPARENT
	}
	return s
}

func (o *overlayWin) setEditing(on bool) {
	if o.editing == on {
		return
	}
	o.editing = on
	w32.SetWindowLongPtr(o.hwnd, w32.GWL_EXSTYLE, uintptr(o.exStyle()))
	w32.SetWindowPos(o.hwnd, w32.HWND_TOPMOST, 0, 0, 0, 0,
		w32.SWP_NOMOVE|w32.SWP_NOSIZE|w32.SWP_NOACTIVATE|w32.SWP_FRAMECHANGED)
}

func (o *overlayWin) setCaptureExcluded(on bool) {
	if o.capture == on {
		return
	}
	o.capture = on
	v := uint32(w32.WDA_NONE)
	if on {
		v = w32.WDA_EXCLUDEFROMCAPTURE
	}
	w32.SetWindowDisplayAffinity(o.hwnd, v)
}

func (o *overlayWin) hide() {
	if o.shown {
		w32.ShowWindow(o.hwnd, w32.SW_HIDE)
		o.shown = false
	}
}

func (o *overlayWin) bringToTop() {
	if o.shown {
		w32.SetWindowPos(o.hwnd, w32.HWND_TOPMOST, 0, 0, 0, 0,
			w32.SWP_NOMOVE|w32.SWP_NOSIZE|w32.SWP_NOACTIVATE|w32.SWP_NOOWNERZORDER|w32.SWP_NOSENDCHANGING)
	}
}

// update re-renders the overlay and positions it from the anchor settings.
func (o *overlayWin) update(cfg *config.Config, snap *data.Snapshot, visible bool) {
	if !visible && !o.editing {
		o.hide()
		return
	}
	ov := &cfg.Overlay
	hm := monitorFor(ov.Monitor)
	mi, _ := w32.GetMonitorInfo(hm)
	o.scale = float32(w32.GetDpiForMonitor(hm)) / 96

	model := layout.Build(cfg, snap, o.editing)
	canvas := o.rend.render(cfg, model, o.scale, o.editing)
	if canvas == nil {
		o.empty = true
		o.hide()
		return
	}
	o.empty = false
	size := w32.SIZE{CX: canvas.W, CY: canvas.H}
	alpha := byte(ov.Opacity * 255 / 100)

	if o.dragging {
		// Keep the window where the user is dragging it.
		w32.UpdateLayeredWindow(o.hwnd, nil, &size, canvas.DC, alpha)
		o.size = size
	} else {
		pos := anchorPos(ov, mi.Monitor, size, o.scale)
		w32.UpdateLayeredWindow(o.hwnd, &pos, &size, canvas.DC, alpha)
		o.pos, o.size = pos, size
	}
	if !o.shown {
		w32.ShowWindow(o.hwnd, w32.SW_SHOWNA)
		o.shown = true
	}
	o.bringToTop()
}

// anchorPos converts anchor + offsets (in 96-DPI px) to screen coordinates.
func anchorPos(ov *config.Overlay, m w32.RECT, size w32.SIZE, scale float32) w32.POINT {
	ox := int32(math.Round(float64(float32(ov.OffsetX) * scale)))
	oy := int32(math.Round(float64(float32(ov.OffsetY) * scale)))
	var p w32.POINT
	switch ov.Anchor[1] {
	case 'l':
		p.X = m.Left + ox
	case 'r':
		p.X = m.Right - size.CX - ox
	default:
		p.X = m.Left + (m.W()-size.CX)/2 + ox
	}
	switch ov.Anchor[0] {
	case 't':
		p.Y = m.Top + oy
	case 'b':
		p.Y = m.Bottom - size.CY - oy
	default:
		p.Y = m.Top + (m.H()-size.CY)/2 + oy
	}
	return p
}

// anchorFromRect derives anchor/offsets from a window rectangle.
func anchorFromRect(rc w32.RECT) (anchor string, ox, oy int, device string) {
	hm := w32.MonitorFromRect(rc, w32.MONITOR_DEFAULTTONEAREST)
	mi, _ := w32.GetMonitorInfo(hm)
	m := mi.Monitor
	scale := float32(w32.GetDpiForMonitor(hm)) / 96
	cx := (rc.Left + rc.Right) / 2
	cy := (rc.Top + rc.Bottom) / 2
	var h, v byte
	switch {
	case cx < m.Left+m.W()/3:
		h = 'l'
		ox = int(float32(rc.Left-m.Left) / scale)
	case cx > m.Left+m.W()*2/3:
		h = 'r'
		ox = int(float32(m.Right-rc.Right) / scale)
	default:
		h = 'c'
		ox = int(float32(cx-(m.Left+m.W()/2)) / scale)
	}
	switch {
	case cy < m.Top+m.H()/3:
		v = 't'
		oy = int(float32(rc.Top-m.Top) / scale)
	case cy > m.Top+m.H()*2/3:
		v = 'b'
		oy = int(float32(m.Bottom-rc.Bottom) / scale)
	default:
		v = 'm'
		oy = int(float32(cy-(m.Top+m.H()/2)) / scale)
	}
	if !isPrimary(hm) {
		device = monitorDevice(hm)
	}
	return string([]byte{v, h}), ox, oy, device
}

func (o *overlayWin) wndProc(msg uint32, w, l uintptr) (uintptr, bool) {
	switch msg {
	case w32.WM_NCHITTEST:
		if o.editing {
			return w32.HTCAPTION, true
		}
		return w32.HTTRANSPARENT, true
	case w32.WM_MOUSEACTIVATE:
		return 3, true // MA_NOACTIVATE
	case w32.WM_SETCURSOR:
		if o.editing {
			w32.SetCursor(w32.LoadCursor(w32.IDC_SIZEALL))
			return 1, true
		}
	case w32.WM_NCLBUTTONDBLCLK:
		o.app.setEditMode(false)
		return 0, true
	case w32.WM_NCRBUTTONUP, w32.WM_CONTEXTMENU:
		if o.editing {
			o.app.showOverlayMenu()
			return 0, true
		}
	case w32.WM_ENTERSIZEMOVE:
		o.dragging = true
	case w32.WM_MOVING:
		rc := (*w32.RECT)(unsafe.Pointer(l))
		snapRect(rc)
		return 1, true
	case w32.WM_EXITSIZEMOVE:
		o.dragging = false
		rc := w32.GetWindowRect(o.hwnd)
		a, ox, oy, dev := anchorFromRect(rc)
		o.app.setOverlayPosition(a, ox, oy, dev)
	case w32.WM_DPICHANGED:
		return 0, true // we compute our own scale from the target monitor
	}
	return 0, false
}

// snapRect snaps a dragged rect to monitor edges and centre lines.
func snapRect(rc *w32.RECT) {
	hm := w32.MonitorFromRect(*rc, w32.MONITOR_DEFAULTTONEAREST)
	mi, ok := w32.GetMonitorInfo(hm)
	if !ok {
		return
	}
	m := mi.Monitor
	d := int32(14 * float32(w32.GetDpiForMonitor(hm)) / 96)
	w, h := rc.W(), rc.H()
	abs := func(v int32) int32 {
		if v < 0 {
			return -v
		}
		return v
	}
	switch {
	case abs(rc.Left-m.Left) < d:
		rc.Left = m.Left
	case abs(rc.Right-m.Right) < d:
		rc.Left = m.Right - w
	case abs((rc.Left+w/2)-(m.Left+m.W()/2)) < d:
		rc.Left = m.Left + (m.W()-w)/2
	}
	switch {
	case abs(rc.Top-m.Top) < d:
		rc.Top = m.Top
	case abs(rc.Bottom-m.Bottom) < d:
		rc.Top = m.Bottom - h
	case abs((rc.Top+h/2)-(m.Top+m.H()/2)) < d:
		rc.Top = m.Top + (m.H()-h)/2
	}
	rc.Right = rc.Left + w
	rc.Bottom = rc.Top + h
}

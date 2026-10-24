//go:build windows

package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"glance/internal/config"
	"glance/internal/data"
	"glance/internal/gdip"
	"glance/internal/w32"
)

const (
	trayID       = 1
	wmTray       = w32.WM_APP + 1
	timerHover   = 2
	timerShowCard = 3
)

type trayIcon struct {
	app       *App
	added     bool
	icon      w32.HICON
	iconKey   string
	tip       string
	hoverPos  w32.POINT
	lastMove  time.Time
	outside   time.Time
	polling   bool
	pending   bool
	logo      w32.HICON
	logoSize  int32
	tipSent   string
	tipValid  bool
}

func newTray(app *App) *trayIcon { return &trayIcon{app: app} }

func (t *trayIcon) nid() *w32.NOTIFYICONDATAW {
	n := &w32.NOTIFYICONDATAW{Wnd: t.app.hwnd, ID: trayID}
	return n
}

func (t *trayIcon) iconSize() (int32, uint32) {
	hm := w32.MonitorFromPoint(w32.POINT{}, w32.MONITOR_DEFAULTTOPRIMARY)
	dpi := w32.GetDpiForMonitor(hm)
	return w32.GetSystemMetricsForDpi(w32.SM_CXSMICON, dpi), dpi
}

// add (re)creates the notification icon; also used after explorer restarts.
func (t *trayIcon) add() {
	n := t.nid()
	n.Flags = w32.NIF_MESSAGE | w32.NIF_ICON
	n.CallbackMessage = wmTray
	n.Icon = t.currentIcon()
	t.setTip(n)
	if t.added {
		w32.ShellNotifyIcon(w32.NIM_DELETE, t.nid())
	}
	t.added = w32.ShellNotifyIcon(w32.NIM_ADD, n)
	v := t.nid()
	v.Version = w32.NOTIFYICON_VERSION_4
	w32.ShellNotifyIcon(w32.NIM_SETVERSION, v)
}

func (t *trayIcon) remove() {
	if t.added {
		w32.ShellNotifyIcon(w32.NIM_DELETE, t.nid())
		t.added = false
	}
	w32.DestroyIcon(t.icon)
	t.icon = 0
}

func (t *trayIcon) currentIcon() w32.HICON {
	if t.icon != 0 {
		return t.icon
	}
	return t.logoIcon()
}

func (t *trayIcon) logoIcon() w32.HICON {
	sz, _ := t.iconSize()
	if t.logo == 0 || t.logoSize != sz {
		t.logo = w32.LoadIconRes(t.app.inst, 1, sz, sz)
		t.logoSize = sz
	}
	return t.logo
}

// setTip adds the tooltip to n when it changed. With the hover card on,
// the tooltip is emptied so Windows 11 does not draw its own text tooltip
// on top of the card.
func (t *trayIcon) setTip(n *w32.NOTIFYICONDATAW) {
	tip := t.tip
	if t.app.cfg.Tray.HoverCard {
		tip = ""
	}
	if t.tipValid && tip == t.tipSent && n.Flags&w32.NIF_MESSAGE == 0 {
		return
	}
	n.Flags |= w32.NIF_TIP
	if tip != "" {
		n.Flags |= w32.NIF_SHOWTIP
	}
	copy(n.Tip[:127], utf16(tip))
	t.tipSent, t.tipValid = tip, true
}

// update refreshes the icon picture and tooltip from a snapshot.
func (t *trayIcon) update(cfg *config.Config, snap *data.Snapshot, light bool) {
	t.tip = tooltipText(snap)
	key := ""
	var hicon w32.HICON
	metric := cfg.Tray.IconMetric
	if metric != "logo" {
		text := "--"
		var col uint32
		v, ok := snap.Get(metric)
		if strings.HasPrefix(metric, "fps.") && !snap.FPSActive {
			ok = false
		}
		if strings.HasPrefix(metric, "gpu.") && snap.GPUAsleep {
			text = "zz"
		}
		if ok {
			text = fmt.Sprintf("%.0f", v)
			if item := cfg.Item(metric); item != nil {
				col = thresholdIconColor(cfg, item, v, light)
			}
		}
		if col == 0 {
			if cfg.Tray.IconColor != "auto" {
				col = config.ParseColor(cfg.Tray.IconColor)
			} else if light {
				col = 0x1B1B1B
			} else {
				col = 0xFFFFFF
			}
		}
		sz, _ := t.iconSize()
		key = fmt.Sprintf("%s|%06X|%d", text, col, sz)
		if key != t.iconKey {
			hicon = renderTrayIcon(text, col, sz)
		}
	}
	n := t.nid()
	n.Flags = 0
	if key != t.iconKey || (metric == "logo" && t.icon != 0) {
		old := t.icon
		if metric == "logo" {
			t.icon = 0
			n.Icon = t.logoIcon()
		} else {
			t.icon = hicon
			n.Icon = hicon
		}
		n.Flags |= w32.NIF_ICON
		t.iconKey = key
		defer w32.DestroyIcon(old)
	}
	t.setTip(n)
	if n.Flags != 0 && t.added {
		w32.ShellNotifyIcon(w32.NIM_MODIFY, n)
	}
}

func thresholdIconColor(cfg *config.Config, it *config.Item, v float64, light bool) uint32 {
	c := layoutColor(cfg, it, v)
	if c == 0 {
		return 0
	}
	if light {
		if c == config.ParseColor(cfg.Overlay.WarnColor) {
			return lightCard.warn
		}
		if c == config.ParseColor(cfg.Overlay.CritColor) {
			return lightCard.crit
		}
	}
	return c
}

func tooltipText(s *data.Snapshot) string {
	var parts []string
	if v, ok := s.Get(data.CPUTemp); ok {
		p := fmt.Sprintf("CPU %.0f°C", v)
		if w, ok := s.Get(data.CPUPower); ok {
			p += fmt.Sprintf(" %.0fW", w)
		}
		parts = append(parts, p)
	} else if v, ok := s.Get(data.CPUUsage); ok {
		parts = append(parts, fmt.Sprintf("CPU %.0f%%", v))
	}
	if s.GPUAsleep {
		parts = append(parts, "GPU 休眠")
	} else if v, ok := s.Get(data.GPUTemp); ok {
		p := fmt.Sprintf("GPU %.0f°C", v)
		if w, ok := s.Get(data.GPUPower); ok {
			p += fmt.Sprintf(" %.0fW", w)
		}
		parts = append(parts, p)
	}
	if s.FPSActive {
		if v, ok := s.Get(data.FPS); ok {
			parts = append(parts, fmt.Sprintf("%.0f FPS", v))
		}
	}
	return "Glance\n" + strings.Join(parts, " · ")
}

// renderTrayIcon draws a number that fills the small icon.
func renderTrayIcon(text string, rgb uint32, size int32) w32.HICON {
	c := gdip.NewCanvas(size, size)
	defer c.Close()
	c.Clear(0)
	f := gdip.NewFont("Segoe UI", float32(size), true)
	defer f.Close()
	p := gdip.NewPath()
	defer p.Close()
	p.AddString(text, f, 0, 0)
	b := p.Bounds()
	if b.W <= 0 || b.H <= 0 {
		return 0
	}
	availW := float32(size) * 0.98
	availH := float32(size) * 0.78
	sy := availH / b.H
	sx := math.Min(float64(sy), float64(availW/b.W))
	// allow horizontal condensing for 3-digit values
	c.Translate(float32(size)/2, float32(size)/2)
	c.Scale(float32(sx), sy)
	c.Translate(-(b.X + b.W/2), -(b.Y + b.H/2))
	br := gdip.NewBrush(gdip.WithAlpha(rgb, 255))
	c.FillPath(br, p)
	br.Close()
	c.ResetTransform()
	return c.HICON()
}

// ---- hover card logic ----

func (t *trayIcon) iconRect() w32.RECT {
	if rc, ok := w32.ShellNotifyIconGetRect(t.app.hwnd, trayID); ok && rc.W() > 0 {
		return rc
	}
	p := t.hoverPos
	return w32.RECT{Left: p.X - 12, Top: p.Y - 12, Right: p.X + 12, Bottom: p.Y + 12}
}

// onMouseMove is called for every mouse move over the icon.
func (t *trayIcon) onMouseMove(p w32.POINT) {
	t.hoverPos = w32.GetCursorPos()
	t.lastMove = time.Now()
	if !t.app.cfg.Tray.HoverCard || t.app.menuOpen {
		return
	}
	if t.app.card.shown {
		return
	}
	if !t.pending {
		t.pending = true
		w32.SetTimer(t.app.hwnd, timerShowCard, 350)
	}
	t.startPolling()
}

func (t *trayIcon) startPolling() {
	if !t.polling {
		t.polling = true
		t.outside = time.Time{}
		w32.SetTimer(t.app.hwnd, timerHover, 120)
	}
}

func (t *trayIcon) stopPolling() {
	if t.polling {
		w32.KillTimer(t.app.hwnd, timerHover)
		t.polling = false
	}
	if t.pending {
		w32.KillTimer(t.app.hwnd, timerShowCard)
		t.pending = false
	}
}

func (t *trayIcon) cursorInside() bool {
	cur := w32.GetCursorPos()
	ir := t.iconRect()
	ir.Left -= 2
	ir.Top -= 2
	ir.Right += 2
	ir.Bottom += 2
	if ir.Contains(cur) {
		return true
	}
	return t.app.card.shown && t.app.card.rect.Contains(cur)
}

func (t *trayIcon) onShowTimer() {
	w32.KillTimer(t.app.hwnd, timerShowCard)
	t.pending = false
	if t.app.menuOpen || !t.cursorInside() {
		return
	}
	t.app.showCard(t.iconRect())
}

func (t *trayIcon) onPollTimer() {
	if t.cursorInside() {
		t.outside = time.Time{}
		return
	}
	if t.outside.IsZero() {
		t.outside = time.Now()
		return
	}
	if time.Since(t.outside) > 250*time.Millisecond {
		t.stopPolling()
		t.app.hideCard()
	}
}

func utf16(s string) []uint16 {
	u := make([]uint16, 0, len(s))
	for _, r := range s {
		if r >= 0x10000 {
			r -= 0x10000
			u = append(u, uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF)))
			continue
		}
		u = append(u, uint16(r))
	}
	return u
}

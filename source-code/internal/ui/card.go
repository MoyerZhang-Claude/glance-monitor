//go:build windows

package ui

import (
	"math"
	"syscall"
	"time"

	"glance/internal/config"
	"glance/internal/data"
	"glance/internal/gdip"
	"glance/internal/layout"
	"glance/internal/w32"
)

const cardClass = "GlanceHUD.Card"

type cardTheme struct {
	bg, border, text, dim, track uint32
	warn, crit                   uint32
}

var (
	darkCard = cardTheme{bg: 0x202020, border: 0x3D3D3D, text: 0xFFFFFF, dim: 0x9DA3AB, track: 0x2E2E2E, warn: 0xFFB020, crit: 0xFF5F5F}
	lightCard = cardTheme{bg: 0xF7F7F7, border: 0xD6D6D6, text: 0x1B1B1B, dim: 0x5E636B, track: 0xE9E9E9, warn: 0xB86E00, crit: 0xC42B1C}
)

type cardWin struct {
	app     *App
	hwnd    w32.HWND
	shown   bool
	measure *gdip.Canvas
	canvas  *gdip.Canvas
	fonts   map[string]*fontSet
	fontKey string
	anchor  w32.RECT // tray icon rect the card belongs to
	rect    w32.RECT // current card window rect
	shownAt time.Time
}

var theCard *cardWin

var cardProc = syscall.NewCallback(func(h, msg, w, l uintptr) uintptr {
	if c := theCard; c != nil && c.hwnd == h {
		switch uint32(msg) {
		case w32.WM_MOUSEACTIVATE:
			return 3 // MA_NOACTIVATE
		case w32.WM_LBUTTONUP:
			c.hide()
			c.app.openSettings("")
			return 0
		case w32.WM_RBUTTONUP:
			c.hide()
			c.app.showTrayMenu()
			return 0
		}
	}
	return w32.DefWindowProc(h, uint32(msg), w, l)
})

func newCard(app *App) *cardWin {
	c := &cardWin{app: app, measure: gdip.NewCanvas(4, 4), fonts: map[string]*fontSet{}}
	theCard = c
	wc := w32.WNDCLASSEXW{WndProc: cardProc, Instance: app.inst, Cursor: w32.LoadCursor(w32.IDC_ARROW), ClassName: w32.Ptr(cardClass)}
	w32.RegisterClass(&wc)
	c.hwnd = w32.CreateWindow(w32.WS_EX_LAYERED|w32.WS_EX_TOPMOST|w32.WS_EX_TOOLWINDOW|w32.WS_EX_NOACTIVATE,
		cardClass, "Glance", w32.WS_POPUP, 0, 0, 1, 1, 0, app.inst)
	return c
}

func (c *cardWin) font(name string, size float32, bold bool) *fontSet {
	key := name + ftoa(size) + btoa(bold)
	if f, ok := c.fonts[key]; ok {
		return f
	}
	f := newFontSet(name, size, bold)
	c.fonts[key] = f
	return f
}

func (c *cardWin) resetFonts() {
	for _, f := range c.fonts {
		f.close()
	}
	c.fonts = map[string]*fontSet{}
}

func (c *cardWin) hide() {
	if c.shown {
		w32.ShowWindow(c.hwnd, w32.SW_HIDE)
		c.shown = false
	}
}

func (c *cardWin) showAt(anchor w32.RECT, cfg *config.Config, snap *data.Snapshot, light bool) {
	c.anchor = anchor
	c.shownAt = time.Now()
	c.update(cfg, snap, light)
}

// update redraws the card (only while shown, or when showAt calls it).
func (c *cardWin) update(cfg *config.Config, snap *data.Snapshot, light bool) {
	hm := w32.MonitorFromRect(c.anchor, w32.MONITOR_DEFAULTTONEAREST)
	mi, _ := w32.GetMonitorInfo(hm)
	scale := float32(w32.GetDpiForMonitor(hm)) / 96
	key := ftoa(scale)
	if key != c.fontKey {
		c.resetFonts()
		c.fontKey = key
	}
	th := darkCard
	if light {
		th = lightCard
	}
	sections := layout.Card(cfg, snap)

	S := func(v float32) float32 { return float32(math.Round(float64(v * scale))) }
	shadow := S(14)
	pad := S(14)
	cardW := S(300)
	inner := cardW - 2*pad
	colGap := S(16)
	colW := (inner - colGap) / 2

	title := c.font("Segoe UI Semibold", 13*scale, false)
	sub := c.font("Segoe UI", 11.5*scale, false)
	name := c.font("Segoe UI", 12*scale, false)
	value := c.font("Segoe UI Semibold", 14*scale, false)
	unit := c.font("Segoe UI", 11*scale, false)
	mc := c.measure

	lineTitle := S(22)
	linePair := S(22)
	gapSection := S(12)

	// height
	h := pad
	for i, s := range sections {
		if i > 0 {
			h += gapSection
		}
		h += lineTitle
		if s.Note != "" {
			h += linePair
		}
		h += float32((len(s.Pairs)+1)/2) * linePair
	}
	footer := "左键打开设置 · 右键菜单"
	h += S(10) + S(16) + pad
	W := int32(cardW + 2*shadow)
	H := int32(h + 2*shadow)

	if c.canvas == nil || c.canvas.W != W || c.canvas.H != H {
		c.canvas.Close()
		c.canvas = gdip.NewCanvas(W, H)
	}
	cv := c.canvas
	cv.Clear(0)
	cv.SetGridFitText(true)

	// soft shadow
	for i := float32(0); i < shadow; i += 1 {
		a := uint8(math.Max(0, 26*math.Pow(float64(1-i/shadow), 2.2)))
		if a == 0 {
			continue
		}
		p := gdip.NewPath()
		p.AddRoundRect(shadow-i, shadow-i+S(3), cardW+2*i, h+2*i, S(8)+i)
		b := gdip.NewBrush(gdip.ARGB(a, 0, 0, 0))
		cv.FillPath(b, p)
		b.Close()
		p.Close()
	}
	// card body
	body := gdip.NewPath()
	body.AddRoundRect(shadow, shadow, cardW, h, S(8))
	bb := gdip.NewBrush(gdip.WithAlpha(th.bg, 250))
	cv.FillPath(bb, body)
	bb.Close()
	bp := gdip.NewPen(gdip.WithAlpha(th.border, 255), 1)
	cv.DrawPath(bp, body)
	bp.Close()
	body.Close()

	brush := gdip.NewBrush(0)
	defer brush.Close()
	text := func(fs *fontSet, s string, x, y float32, col uint32) {
		f, dy := fs.pick(s)
		brush.Set(col)
		cv.DrawText(s, f, brush, x, y+dy)
	}
	fit := func(fs *fontSet, s string, maxW float32) string {
		if fs.measure(mc, s) <= maxW {
			return s
		}
		r := []rune(s)
		for len(r) > 1 {
			r = r[:len(r)-1]
			if t := string(r) + "…"; fs.measure(mc, t) <= maxW {
				return t
			}
		}
		return s
	}

	x0 := shadow + pad
	y := shadow + pad
	for i, s := range sections {
		if i > 0 {
			y += gapSection
		}
		// accent bar + title + subtitle
		bar := gdip.NewBrush(gdip.WithAlpha(s.Color, 255))
		bpath := gdip.NewPath()
		bpath.AddRoundRect(x0, y+S(4), S(3), S(14), S(1.5))
		cv.FillPath(bar, bpath)
		bpath.Close()
		bar.Close()
		text(title, s.Title, x0+S(9), y+S(1), gdip.WithAlpha(th.text, 255))
		tw := title.measure(mc, s.Title)
		if s.Subtitle != "" {
			avail := inner - S(9) - tw - S(10)
			st := fit(sub, s.Subtitle, avail)
			sw := sub.measure(mc, st)
			text(sub, st, x0+inner-sw, y+S(3), gdip.WithAlpha(th.dim, 255))
		}
		y += lineTitle
		if s.Note != "" {
			text(name, fit(name, s.Note, inner), x0+S(9), y+S(2), gdip.WithAlpha(th.dim, 255))
			y += linePair
		}
		for pi, p := range s.Pairs {
			col := pi % 2
			px := x0 + S(9) + float32(col)*(colW+colGap)
			cw := colW - S(9)
			if col == 1 {
				cw = colW
				px = x0 + colW + colGap
			}
			text(name, p.Name, px, y+S(3), gdip.WithAlpha(th.dim, 255))
			vcol := th.text
			if p.Color != 0 {
				vcol = p.Color
				if p.Color == config.ParseColor(cfg.Overlay.WarnColor) {
					vcol = th.warn
				} else if p.Color == config.ParseColor(cfg.Overlay.CritColor) {
					vcol = th.crit
				}
			}
			uw := float32(0)
			if p.Unit != "" {
				uw = unit.measure(mc, p.Unit) + S(2)
			}
			vw := value.measure(mc, p.Value)
			right := px + cw
			if p.Unit != "" {
				text(unit, p.Unit, right-uw+S(2), y+S(4.5), gdip.WithAlpha(th.dim, 255))
			}
			text(value, p.Value, right-uw-vw, y+S(1), gdip.WithAlpha(vcol, 255))
			if col == 1 || pi == len(s.Pairs)-1 {
				y += linePair
			}
		}
	}
	y += S(10)
	sep := gdip.NewPen(gdip.WithAlpha(th.border, 255), 1)
	cv.Line(sep, x0, y-S(4), x0+inner, y-S(4))
	sep.Close()
	text(sub, footer, x0, y+S(1), gdip.WithAlpha(th.dim, 200))

	// position next to the tray icon, inside the work area
	work := mi.Work
	a := c.anchor
	var px, py int32
	px = (a.Left+a.Right)/2 - W/2
	switch {
	case a.Top >= work.Bottom-2: // taskbar at bottom
		py = work.Bottom - H + int32(shadow) - int32(S(6))
	case a.Bottom <= work.Top+2: // top
		py = work.Top - int32(shadow) + int32(S(6))
	case a.Left >= work.Right-2: // right
		px = work.Right - W + int32(shadow) - int32(S(6))
		py = (a.Top+a.Bottom)/2 - H/2
	case a.Right <= work.Left+2: // left
		px = work.Left - int32(shadow) + int32(S(6))
		py = (a.Top+a.Bottom)/2 - H/2
	default: // icon in the overflow flyout or unknown: above the icon
		py = a.Top - H + int32(shadow) - int32(S(6))
	}
	if px+W-int32(shadow) > work.Right {
		px = work.Right - W + int32(shadow) - int32(S(6))
	}
	if px+int32(shadow) < work.Left {
		px = work.Left - int32(shadow) + int32(S(6))
	}
	if py+int32(shadow) < work.Top {
		py = work.Top
	}
	pos := w32.POINT{X: px, Y: py}
	size := w32.SIZE{CX: W, CY: H}
	w32.UpdateLayeredWindow(c.hwnd, &pos, &size, cv.DC, 255)
	c.rect = w32.RECT{Left: px + int32(shadow), Top: py + int32(shadow), Right: px + W - int32(shadow), Bottom: py + H - int32(shadow)}
	if !c.shown {
		w32.ShowWindow(c.hwnd, w32.SW_SHOWNA)
		w32.SetWindowPos(c.hwnd, w32.HWND_TOPMOST, 0, 0, 0, 0, w32.SWP_NOMOVE|w32.SWP_NOSIZE|w32.SWP_NOACTIVATE)
		c.shown = true
	}
}

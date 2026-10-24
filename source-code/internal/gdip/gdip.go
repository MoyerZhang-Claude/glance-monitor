//go:build windows

// Package gdip is a minimal GDI+ flat-API wrapper used to draw the overlay,
// the hover card and the tray icon into premultiplied ARGB bitmaps.
package gdip

import (
	"math"
	"syscall"
	"unsafe"

	"glance/internal/w32"
)

var (
	dll = syscall.NewLazyDLL("gdiplus.dll")

	pStartup                  = dll.NewProc("GdiplusStartup")
	pShutdown                 = dll.NewProc("GdiplusShutdown")
	pCreateBitmapFromScan0    = dll.NewProc("GdipCreateBitmapFromScan0")
	pGetImageGraphicsContext  = dll.NewProc("GdipGetImageGraphicsContext")
	pDeleteGraphics           = dll.NewProc("GdipDeleteGraphics")
	pDisposeImage             = dll.NewProc("GdipDisposeImage")
	pSetSmoothingMode         = dll.NewProc("GdipSetSmoothingMode")
	pSetTextRenderingHint     = dll.NewProc("GdipSetTextRenderingHint")
	pSetPixelOffsetMode       = dll.NewProc("GdipSetPixelOffsetMode")
	pSetCompositingQuality    = dll.NewProc("GdipSetCompositingQuality")
	pGraphicsClear            = dll.NewProc("GdipGraphicsClear")
	pCreateFontFamilyFromName = dll.NewProc("GdipCreateFontFamilyFromName")
	pDeleteFontFamily         = dll.NewProc("GdipDeleteFontFamily")
	pGetGenericSansSerif      = dll.NewProc("GdipGetGenericFontFamilySansSerif")
	pIsStyleAvailable         = dll.NewProc("GdipIsStyleAvailable")
	pCreateFont               = dll.NewProc("GdipCreateFont")
	pDeleteFont               = dll.NewProc("GdipDeleteFont")
	pStringFormatTypographic  = dll.NewProc("GdipStringFormatGetGenericTypographic")
	pCloneStringFormat        = dll.NewProc("GdipCloneStringFormat")
	pSetStringFormatFlags     = dll.NewProc("GdipSetStringFormatFlags")
	pDeleteStringFormat       = dll.NewProc("GdipDeleteStringFormat")
	pMeasureString            = dll.NewProc("GdipMeasureString")
	pDrawString               = dll.NewProc("GdipDrawString")
	pCreateSolidFill          = dll.NewProc("GdipCreateSolidFill")
	pSetSolidFillColor        = dll.NewProc("GdipSetSolidFillColor")
	pDeleteBrush              = dll.NewProc("GdipDeleteBrush")
	pCreatePen1               = dll.NewProc("GdipCreatePen1")
	pDeletePen                = dll.NewProc("GdipDeletePen")
	pSetPenLineJoin           = dll.NewProc("GdipSetPenLineJoin")
	pSetPenDashStyle          = dll.NewProc("GdipSetPenDashStyle")
	pCreatePath               = dll.NewProc("GdipCreatePath")
	pDeletePath               = dll.NewProc("GdipDeletePath")
	pResetPath                = dll.NewProc("GdipResetPath")
	pAddPathString            = dll.NewProc("GdipAddPathString")
	pAddPathArc               = dll.NewProc("GdipAddPathArc")
	pAddPathLine              = dll.NewProc("GdipAddPathLine")
	pClosePathFigure          = dll.NewProc("GdipClosePathFigure")
	pFillPath                 = dll.NewProc("GdipFillPath")
	pDrawPath                 = dll.NewProc("GdipDrawPath")
	pFillRectangle            = dll.NewProc("GdipFillRectangle")
	pDrawLine                 = dll.NewProc("GdipDrawLine")
	pDrawLines                = dll.NewProc("GdipDrawLines")
	pFillPolygon              = dll.NewProc("GdipFillPolygon")
	pFillEllipse              = dll.NewProc("GdipFillEllipse")
	pCreateHICONFromBitmap    = dll.NewProc("GdipCreateHICONFromBitmap")
	pTranslateTransform       = dll.NewProc("GdipTranslateWorldTransform")
	pResetTransform           = dll.NewProc("GdipResetWorldTransform")
	pGetPathWorldBounds       = dll.NewProc("GdipGetPathWorldBounds")
	pScaleTransform           = dll.NewProc("GdipScaleWorldTransform")
	pGetCellAscent            = dll.NewProc("GdipGetCellAscent")
	pGetCellDescent           = dll.NewProc("GdipGetCellDescent")
	pGetEmHeight              = dll.NewProc("GdipGetEmHeight")
	pDrawRectangle            = dll.NewProc("GdipDrawRectangle")
)

const (
	PixelFormat32bppPARGB = 0x000E200B
	PixelFormat32bppARGB  = 0x0026200A

	FontStyleRegular = 0
	FontStyleBold    = 1

	UnitPixel = 2

	SmoothingModeAntiAlias   = 4
	TextRenderingAntiAlias   = 4
	TextRenderingAAGridFit   = 3
	PixelOffsetModeHalf      = 4
	CompositingQualityHigh   = 2
	LineJoinRound            = 2
	DashStyleDash            = 1
	FillModeAlternate        = 0
	FillModeWinding          = 1

	StringFormatFlagsNoWrap               = 0x1000
	StringFormatFlagsMeasureTrailingSpaces = 0x0800
	StringFormatFlagsNoClip               = 0x4000
)

type RectF struct{ X, Y, W, H float32 }
type PointF struct{ X, Y float32 }

var token uintptr

// Startup initialises GDI+. Must be called once before use.
func Startup() bool {
	input := struct {
		Version                  uint32
		DebugEventCallback       uintptr
		SuppressBackgroundThread int32
		SuppressExternalCodecs   int32
	}{Version: 1}
	r, _, _ := pStartup.Call(uintptr(unsafe.Pointer(&token)), uintptr(unsafe.Pointer(&input)), 0)
	return r == 0
}

func Shutdown() {
	if token != 0 {
		pShutdown.Call(token)
		token = 0
	}
}

func f(v float32) uintptr { return uintptr(math.Float32bits(v)) }

// ---------------- Canvas ----------------

// Canvas is a premultiplied ARGB DIB section usable both by GDI+ (for
// drawing) and by UpdateLayeredWindow / icon creation.
type Canvas struct {
	W, H   int32
	DC     w32.HDC
	dib    uintptr
	old    uintptr
	Bits   unsafe.Pointer
	bitmap uintptr
	G      uintptr // GpGraphics*
}

func NewCanvas(w, h int32) *Canvas {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	c := &Canvas{W: w, H: h}
	screen := w32.GetDC(0)
	c.DC = w32.CreateCompatibleDC(screen)
	w32.ReleaseDC(0, screen)
	c.dib, c.Bits = w32.CreateDIB(c.DC, w, h)
	c.old = w32.SelectObject(c.DC, c.dib)
	pCreateBitmapFromScan0.Call(uintptr(w), uintptr(h), uintptr(w*4), PixelFormat32bppPARGB, uintptr(c.Bits), uintptr(unsafe.Pointer(&c.bitmap)))
	pGetImageGraphicsContext.Call(c.bitmap, uintptr(unsafe.Pointer(&c.G)))
	pSetSmoothingMode.Call(c.G, SmoothingModeAntiAlias)
	pSetTextRenderingHint.Call(c.G, TextRenderingAntiAlias)
	pSetPixelOffsetMode.Call(c.G, PixelOffsetModeHalf)
	pSetCompositingQuality.Call(c.G, CompositingQualityHigh)
	return c
}

func (c *Canvas) Valid() bool { return c != nil && c.G != 0 && c.Bits != nil }

func (c *Canvas) Close() {
	if c == nil {
		return
	}
	if c.G != 0 {
		pDeleteGraphics.Call(c.G)
		c.G = 0
	}
	if c.bitmap != 0 {
		pDisposeImage.Call(c.bitmap)
		c.bitmap = 0
	}
	if c.DC != 0 {
		w32.SelectObject(c.DC, c.old)
		w32.DeleteObject(c.dib)
		w32.DeleteDC(c.DC)
		c.DC = 0
	}
}

func (c *Canvas) Clear(argb uint32) { pGraphicsClear.Call(c.G, uintptr(argb)) }

func (c *Canvas) SetGridFitText(on bool) {
	if on {
		pSetTextRenderingHint.Call(c.G, TextRenderingAAGridFit)
	} else {
		pSetTextRenderingHint.Call(c.G, TextRenderingAntiAlias)
	}
}

// HICON builds an icon from the canvas bitmap (caller destroys it).
func (c *Canvas) HICON() w32.HICON {
	// GdipCreateHICONFromBitmap expects non-premultiplied data; build an ARGB
	// copy so semi-transparent edge pixels keep the right colour.
	n := int(c.W * c.H)
	src := unsafe.Slice((*uint32)(c.Bits), n)
	dst := make([]uint32, n)
	for i, p := range src {
		a := p >> 24
		if a == 0 {
			continue
		}
		if a == 255 {
			dst[i] = p
			continue
		}
		r := ((p >> 16) & 0xFF) * 255 / a
		g := ((p >> 8) & 0xFF) * 255 / a
		b := (p & 0xFF) * 255 / a
		dst[i] = a<<24 | min32(r, 255)<<16 | min32(g, 255)<<8 | min32(b, 255)
	}
	var bmp uintptr
	pCreateBitmapFromScan0.Call(uintptr(c.W), uintptr(c.H), uintptr(c.W*4), PixelFormat32bppARGB, uintptr(unsafe.Pointer(&dst[0])), uintptr(unsafe.Pointer(&bmp)))
	if bmp == 0 {
		return 0
	}
	defer pDisposeImage.Call(bmp)
	var h uintptr
	pCreateHICONFromBitmap.Call(bmp, uintptr(unsafe.Pointer(&h)))
	return h
}

func min32(a, b uint32) uint32 {
	if a < b {
		return a
	}
	return b
}

func (c *Canvas) Translate(dx, dy float32) { pTranslateTransform.Call(c.G, f(dx), f(dy), 0) }
func (c *Canvas) ResetTransform()          { pResetTransform.Call(c.G) }
func (c *Canvas) Scale(sx, sy float32)     { pScaleTransform.Call(c.G, f(sx), f(sy), 0) }

// ---------------- Fonts ----------------

type Font struct {
	Family uintptr
	Font   uintptr
	Size   float32
	Style  int32
}

// NewFont creates a font from a family name; falls back to Segoe UI and
// then to the generic sans-serif family.
func NewFont(family string, sizePx float32, bold bool) *Font {
	style := int32(FontStyleRegular)
	if bold {
		style = FontStyleBold
	}
	ft := &Font{Size: sizePx, Style: style}
	for _, name := range []string{family, "Segoe UI", "Microsoft YaHei UI"} {
		if name == "" {
			continue
		}
		var fam uintptr
		pCreateFontFamilyFromName.Call(w32.UPtr(name), 0, uintptr(unsafe.Pointer(&fam)))
		if fam != 0 {
			var avail int32
			pIsStyleAvailable.Call(fam, uintptr(style), uintptr(unsafe.Pointer(&avail)))
			if avail == 0 && style != FontStyleRegular {
				ft.Style = FontStyleRegular
			}
			ft.Family = fam
			break
		}
	}
	if ft.Family == 0 {
		var fam uintptr
		pGetGenericSansSerif.Call(uintptr(unsafe.Pointer(&fam)))
		ft.Family = fam
	}
	pCreateFont.Call(ft.Family, f(sizePx), uintptr(ft.Style), UnitPixel, uintptr(unsafe.Pointer(&ft.Font)))
	return ft
}

// Ascent returns the font ascent and descent in pixels.
func (ft *Font) Ascent() (float32, float32) {
	var em, asc, desc uint16
	pGetEmHeight.Call(ft.Family, uintptr(ft.Style), uintptr(unsafe.Pointer(&em)))
	pGetCellAscent.Call(ft.Family, uintptr(ft.Style), uintptr(unsafe.Pointer(&asc)))
	pGetCellDescent.Call(ft.Family, uintptr(ft.Style), uintptr(unsafe.Pointer(&desc)))
	if em == 0 {
		return ft.Size * 0.9, ft.Size * 0.25
	}
	return ft.Size * float32(asc) / float32(em), ft.Size * float32(desc) / float32(em)
}

func (ft *Font) Close() {
	if ft == nil {
		return
	}
	if ft.Font != 0 {
		pDeleteFont.Call(ft.Font)
		ft.Font = 0
	}
	if ft.Family != 0 {
		pDeleteFontFamily.Call(ft.Family)
		ft.Family = 0
	}
}

var typographic uintptr

func format() uintptr {
	if typographic == 0 {
		var base uintptr
		pStringFormatTypographic.Call(uintptr(unsafe.Pointer(&base)))
		pCloneStringFormat.Call(base, uintptr(unsafe.Pointer(&typographic)))
		pSetStringFormatFlags.Call(typographic, StringFormatFlagsNoWrap|StringFormatFlagsMeasureTrailingSpaces|StringFormatFlagsNoClip)
	}
	return typographic
}

// Measure returns the typographic width and height of s.
func (c *Canvas) Measure(s string, ft *Font) (float32, float32) {
	if s == "" {
		return 0, ft.Size
	}
	u, _ := syscall.UTF16FromString(s)
	layout := RectF{0, 0, 100000, 100000}
	var box RectF
	pMeasureString.Call(c.G, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), ft.Font,
		uintptr(unsafe.Pointer(&layout)), format(), uintptr(unsafe.Pointer(&box)), 0, 0)
	return box.W, box.H
}

// ---------------- Brushes / pens / paths ----------------

type Brush struct{ h uintptr }

func NewBrush(argb uint32) *Brush {
	b := &Brush{}
	pCreateSolidFill.Call(uintptr(argb), uintptr(unsafe.Pointer(&b.h)))
	return b
}
func (b *Brush) Set(argb uint32) { pSetSolidFillColor.Call(b.h, uintptr(argb)) }
func (b *Brush) Close() {
	if b != nil && b.h != 0 {
		pDeleteBrush.Call(b.h)
		b.h = 0
	}
}

type Pen struct{ h uintptr }

func NewPen(argb uint32, width float32) *Pen {
	p := &Pen{}
	pCreatePen1.Call(uintptr(argb), f(width), UnitPixel, uintptr(unsafe.Pointer(&p.h)))
	pSetPenLineJoin.Call(p.h, LineJoinRound)
	return p
}
func (p *Pen) Dash() { pSetPenDashStyle.Call(p.h, DashStyleDash) }
func (p *Pen) Close() {
	if p != nil && p.h != 0 {
		pDeletePen.Call(p.h)
		p.h = 0
	}
}

type Path struct{ h uintptr }

func NewPath() *Path {
	p := &Path{}
	pCreatePath.Call(FillModeWinding, uintptr(unsafe.Pointer(&p.h)))
	return p
}
func (p *Path) Reset() { pResetPath.Call(p.h) }
func (p *Path) Close() {
	if p != nil && p.h != 0 {
		pDeletePath.Call(p.h)
		p.h = 0
	}
}

// AddString adds text s with its layout box origin at (x, y).
func (p *Path) AddString(s string, ft *Font, x, y float32) {
	if s == "" {
		return
	}
	u, _ := syscall.UTF16FromString(s)
	rc := RectF{x, y, 100000, 100000}
	pAddPathString.Call(p.h, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), ft.Family, uintptr(ft.Style),
		f(ft.Size), uintptr(unsafe.Pointer(&rc)), format())
}

// AddRoundRect adds a rounded rectangle figure.
func (p *Path) AddRoundRect(x, y, w, h, r float32) {
	if r <= 0.5 {
		pAddPathLine.Call(p.h, f(x), f(y), f(x+w), f(y))
		pAddPathLine.Call(p.h, f(x+w), f(y), f(x+w), f(y+h))
		pAddPathLine.Call(p.h, f(x+w), f(y+h), f(x), f(y+h))
		pClosePathFigure.Call(p.h)
		return
	}
	if r > w/2 {
		r = w / 2
	}
	if r > h/2 {
		r = h / 2
	}
	d := r * 2
	pAddPathArc.Call(p.h, f(x), f(y), f(d), f(d), f(180), f(90))
	pAddPathArc.Call(p.h, f(x+w-d), f(y), f(d), f(d), f(270), f(90))
	pAddPathArc.Call(p.h, f(x+w-d), f(y+h-d), f(d), f(d), f(0), f(90))
	pAddPathArc.Call(p.h, f(x), f(y+h-d), f(d), f(d), f(90), f(90))
	pClosePathFigure.Call(p.h)
}

func (p *Path) Bounds() RectF {
	var r RectF
	pGetPathWorldBounds.Call(p.h, uintptr(unsafe.Pointer(&r)), 0, 0)
	return r
}

func (c *Canvas) FillPath(b *Brush, p *Path) { pFillPath.Call(c.G, b.h, p.h) }
func (c *Canvas) DrawPath(pen *Pen, p *Path) { pDrawPath.Call(c.G, pen.h, p.h) }

func (c *Canvas) FillRect(b *Brush, x, y, w, h float32) {
	pFillRectangle.Call(c.G, b.h, f(x), f(y), f(w), f(h))
}

func (c *Canvas) StrokeRect(pen *Pen, x, y, w, h float32) {
	pDrawRectangle.Call(c.G, pen.h, f(x), f(y), f(w), f(h))
}

func (c *Canvas) FillEllipse(b *Brush, x, y, w, h float32) {
	pFillEllipse.Call(c.G, b.h, f(x), f(y), f(w), f(h))
}

func (c *Canvas) Line(pen *Pen, x1, y1, x2, y2 float32) {
	pDrawLine.Call(c.G, pen.h, f(x1), f(y1), f(x2), f(y2))
}

func (c *Canvas) Lines(pen *Pen, pts []PointF) {
	if len(pts) < 2 {
		return
	}
	pDrawLines.Call(c.G, pen.h, uintptr(unsafe.Pointer(&pts[0])), uintptr(len(pts)))
}

func (c *Canvas) FillPolygon(b *Brush, pts []PointF) {
	if len(pts) < 3 {
		return
	}
	pFillPolygon.Call(c.G, b.h, uintptr(unsafe.Pointer(&pts[0])), uintptr(len(pts)), FillModeWinding)
}

// DrawText draws plain anti-aliased text with the layout origin at (x, y).
func (c *Canvas) DrawText(s string, ft *Font, b *Brush, x, y float32) {
	if s == "" {
		return
	}
	u, _ := syscall.UTF16FromString(s)
	rc := RectF{x, y, 100000, 100000}
	pDrawString.Call(c.G, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), ft.Font, uintptr(unsafe.Pointer(&rc)), format(), b.h)
}

// ARGB packs a colour.
func ARGB(a, r, g, b uint8) uint32 {
	return uint32(a)<<24 | uint32(r)<<16 | uint32(g)<<8 | uint32(b)
}

// WithAlpha replaces the alpha channel of an RGB colour.
func WithAlpha(rgb uint32, a uint8) uint32 { return rgb&0xFFFFFF | uint32(a)<<24 }

//go:build windows

package ui

import (
	"math"

	"glance/internal/gdip"
)

const cjkFont = "Microsoft YaHei UI"

// fontSet caches a Latin font and a CJK companion of the same size.
type fontSet struct {
	key        string
	latin, cjk *gdip.Font
	ascent     float32 // latin ascent (px)
	cjkAscent  float32
}

func newFontSet(family string, size float32, bold bool) *fontSet {
	fs := &fontSet{latin: gdip.NewFont(family, size, bold), cjk: gdip.NewFont(cjkFont, size, bold)}
	fs.ascent, _ = fs.latin.Ascent()
	fs.cjkAscent, _ = fs.cjk.Ascent()
	return fs
}

func (fs *fontSet) close() {
	if fs != nil {
		fs.latin.Close()
		fs.cjk.Close()
	}
}

func hasCJK(s string) bool {
	for _, r := range s {
		if r >= 0x2E80 {
			return true
		}
	}
	return false
}

// pick returns the font to draw s with and the y-shift that keeps its
// baseline aligned with the latin font.
func (fs *fontSet) pick(s string) (*gdip.Font, float32) {
	if hasCJK(s) {
		return fs.cjk, fs.ascent - fs.cjkAscent
	}
	return fs.latin, 0
}

func (fs *fontSet) measure(c *gdip.Canvas, s string) float32 {
	if s == "" {
		return 0
	}
	f, _ := fs.pick(s)
	w, _ := c.Measure(s, f)
	return w
}

// textPainter batches text into paths per colour so effects (shadow,
// outline) are drawn once for all text and fills once per colour.
type textPainter struct {
	all    *gdip.Path
	colors map[uint32]*gdip.Path
	order  []uint32
}

func newTextPainter() *textPainter {
	return &textPainter{all: gdip.NewPath(), colors: map[uint32]*gdip.Path{}}
}

func (tp *textPainter) reset() {
	tp.all.Reset()
	for _, p := range tp.colors {
		p.Reset()
	}
	tp.order = tp.order[:0]
}

func (tp *textPainter) add(fs *fontSet, s string, x, y float32, argb uint32) {
	if s == "" {
		return
	}
	f, dy := fs.pick(s)
	p := tp.colors[argb]
	if p == nil {
		p = gdip.NewPath()
		tp.colors[argb] = p
	}
	seen := false
	for _, c := range tp.order {
		if c == argb {
			seen = true
			break
		}
	}
	if !seen {
		tp.order = append(tp.order, argb)
	}
	p.AddString(s, f, x, y+dy)
	tp.all.AddString(s, f, x, y+dy)
}

// draw renders the batched text with the given effect.
func (tp *textPainter) draw(c *gdip.Canvas, effect string, scale float32) {
	switch effect {
	case "outline":
		pen := gdip.NewPen(gdip.ARGB(215, 0, 0, 0), float32(math.Max(1.6, float64(2.2*scale))))
		c.DrawPath(pen, tp.all)
		pen.Close()
	case "shadow":
		b := gdip.NewBrush(gdip.ARGB(170, 0, 0, 0))
		d := float32(math.Max(1, math.Round(float64(scale))))
		c.Translate(d, d)
		c.FillPath(b, tp.all)
		c.ResetTransform()
		b.Close()
	}
	for _, col := range tp.order {
		b := gdip.NewBrush(col)
		c.FillPath(b, tp.colors[col])
		b.Close()
	}
}

func (tp *textPainter) close() {
	tp.all.Close()
	for _, p := range tp.colors {
		p.Close()
	}
}

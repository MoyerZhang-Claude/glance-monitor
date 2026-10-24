//go:build windows

package ui

import (
	"math"
	"sort"

	"glance/internal/config"
	"glance/internal/gdip"
	"glance/internal/layout"
)

// overlayRenderer draws a layout.Model into a premultiplied canvas.
type overlayRenderer struct {
	key     string
	main    *fontSet
	small   *fontSet
	inkTop  float32 // digit ink top relative to the text box (main font)
	inkH    float32
	measure *gdip.Canvas
	canvas  *gdip.Canvas
	tp      *textPainter
}

func newOverlayRenderer() *overlayRenderer {
	return &overlayRenderer{measure: gdip.NewCanvas(4, 4), tp: newTextPainter()}
}

func (r *overlayRenderer) close() {
	r.main.close()
	r.small.close()
	r.measure.Close()
	r.canvas.Close()
	r.tp.close()
}

func (r *overlayRenderer) ensureFonts(o *config.Overlay, scale float32) {
	size := float32(o.FontSize) * scale
	key := o.Font + "|" + ftoa(size) + "|" + btoa(o.Bold) + "|" + ftoa(float32(o.UnitScale))
	if key == r.key {
		return
	}
	r.main.close()
	r.small.close()
	r.main = newFontSet(o.Font, size, o.Bold)
	r.small = newFontSet(o.Font, size*float32(o.UnitScale), o.Bold)
	r.key = key
	p := gdip.NewPath()
	p.AddString("0123456789", r.main.latin, 0, 0)
	b := p.Bounds()
	p.Close()
	r.inkTop, r.inkH = b.Y, b.H
	if r.inkH <= 0 {
		r.inkTop, r.inkH = size*0.25, size*0.72
	}
}

type cellBox struct {
	prefixW, valueW, unitW, w float32
}

const (
	accentEdit = 0x4CC2FF
)

// render draws the model and returns the canvas (owned by the renderer).
func (r *overlayRenderer) render(cfg *config.Config, m layout.Model, scale float32, editing bool) *gdip.Canvas {
	o := &cfg.Overlay
	r.ensureFonts(o, scale)
	mc := r.measure

	pad := float32(o.Padding) * scale
	colGap := float32(o.ColGap) * scale
	rowGap := float32(o.RowGap) * scale
	gapPrefix := float32(math.Round(float64(3 * scale)))
	gapUnit := float32(math.Round(float64(1.5 * scale)))
	rowH := float32(math.Ceil(float64(r.inkH*1.5 + 1)))

	// ---- measure ----
	boxes := make([][]cellBox, len(m.Rows))
	var colW []float32
	for ri, row := range m.Rows {
		boxes[ri] = make([]cellBox, len(row.Cells))
		for ci, c := range row.Cells {
			var b cellBox
			if c.Label {
				b.valueW = r.main.measure(mc, c.Value)
			} else {
				b.prefixW = r.small.measure(mc, c.Prefix)
				b.valueW = r.main.measure(mc, c.Value)
				if t := r.main.measure(mc, c.Template); t > b.valueW && !c.Dim {
					b.valueW = t
				}
				if c.Dim && c.Template != "" && c.Value == "--" {
					b.valueW = r.main.measure(mc, c.Template)
				}
				b.unitW = r.small.measure(mc, c.Unit)
			}
			b.w = b.valueW
			if b.prefixW > 0 {
				b.w += b.prefixW + gapPrefix
			}
			if b.unitW > 0 {
				b.w += b.unitW + gapUnit
			}
			boxes[ri][ci] = b
			if m.Layout != "line" {
				for len(colW) <= ci {
					colW = append(colW, 0)
				}
				if b.w > colW[ci] {
					colW[ci] = b.w
				}
			}
		}
	}

	// ---- size ----
	contentW := float32(0)
	if m.Layout == "line" && len(m.Rows) == 1 {
		for i, b := range boxes[0] {
			contentW += b.w
			if i > 0 {
				contentW += colGap
			}
		}
	} else {
		for i, w := range colW {
			contentW += w
			if i > 0 {
				contentW += colGap
			}
		}
	}
	hint := ""
	if editing {
		hint = "拖动调整位置 · 双击锁定"
	}
	graphH := float32(0)
	if m.ShowGraph {
		graphH = float32(o.GraphHeight) * scale
		if minW := 150 * scale; contentW < minW {
			contentW = minW
		}
	}
	hintW := float32(0)
	if hint != "" {
		hintW = r.small.measure(mc, hint)
		if contentW < hintW {
			contentW = hintW
		}
	}
	if len(m.Rows) == 0 && !m.ShowGraph {
		// Nothing to show (e.g. everything disabled): keep a tiny handle in edit mode.
		if !editing {
			return nil
		}
		contentW = hintW
	}
	contentH := float32(len(m.Rows))*rowH + float32(max(len(m.Rows)-1, 0))*rowGap
	if m.ShowGraph {
		if contentH > 0 {
			contentH += rowGap + 4*scale
		}
		contentH += graphH
	}
	hintH := float32(0)
	if hint != "" {
		hintH = r.small.ascent + 6*scale
		contentH += hintH
	}
	W := int32(math.Ceil(float64(contentW + 2*pad + 2)))
	H := int32(math.Ceil(float64(contentH + 2*pad + 2)))

	if r.canvas == nil || r.canvas.W != W || r.canvas.H != H {
		r.canvas.Close()
		r.canvas = gdip.NewCanvas(W, H)
	}
	c := r.canvas
	c.Clear(0)

	// ---- background ----
	bgA := o.BgOpacity
	if editing && bgA < 45 {
		bgA = 45
	}
	radius := float32(o.Radius) * scale
	if bgA > 0 {
		p := gdip.NewPath()
		p.AddRoundRect(0.5, 0.5, float32(W)-1, float32(H)-1, radius)
		b := gdip.NewBrush(gdip.WithAlpha(config.ParseColor(o.BgColor), uint8(bgA*255/100)))
		c.FillPath(b, p)
		b.Close()
		p.Close()
	}
	if editing {
		p := gdip.NewPath()
		p.AddRoundRect(1, 1, float32(W)-2, float32(H)-2, radius)
		pen := gdip.NewPen(gdip.WithAlpha(accentEdit, 255), float32(math.Max(1.5, float64(1.5*scale))))
		pen.Dash()
		c.DrawPath(pen, p)
		pen.Close()
		p.Close()
	}

	// ---- text ----
	tp := r.tp
	tp.reset()
	unitA := uint8(255)
	y := pad + 1
	for ri, row := range m.Rows {
		boxTop := y + (rowH-r.inkH)/2 - r.inkTop
		smallTop := boxTop + (r.main.ascent - r.small.ascent)
		x := pad + 1
		for ci, cell := range row.Cells {
			b := boxes[ri][ci]
			cx := x
			if cell.Label {
				tp.add(r.main, cell.Value, cx, boxTop, gdip.WithAlpha(cell.Color, 255))
			} else {
				if cell.Prefix != "" {
					tp.add(r.small, cell.Prefix, cx, smallTop, gdip.WithAlpha(cell.PrefixColor, unitA))
					cx += b.prefixW + gapPrefix
				}
				// right-align the value inside its reserved width
				vw := r.main.measure(mc, cell.Value)
				col := gdip.WithAlpha(cell.Color, 255)
				if cell.Dim {
					col = gdip.WithAlpha(cell.Color, 200)
				}
				tp.add(r.main, cell.Value, cx+(b.valueW-vw), boxTop, col)
				cx += b.valueW
				if cell.Unit != "" {
					tp.add(r.small, cell.Unit, cx+gapUnit, smallTop, gdip.WithAlpha(config.ParseColor(o.UnitColor), unitA))
				}
			}
			if m.Layout == "line" {
				x += b.w + colGap
			} else {
				x += colW[ci] + colGap
			}
		}
		y += rowH + rowGap
	}
	if len(m.Rows) > 0 {
		y -= rowGap
	}
	if hint != "" {
		hy := float32(H) - pad - hintH + 2*scale
		tp.add(r.small, hint, (float32(W)-hintW)/2, hy, gdip.WithAlpha(accentEdit, 255))
	}
	tp.draw(c, o.Effect, scale)

	// ---- frame-time graph ----
	if m.ShowGraph {
		gy := y
		if len(m.Rows) > 0 {
			gy += rowGap + 4*scale
		}
		r.drawGraph(c, m.Graph, pad+1, gy, contentW, graphH, scale, config.ParseColor(o.GraphColor), o.GraphSecs)
	}
	return c
}

func (r *overlayRenderer) drawGraph(c *gdip.Canvas, ft []float32, x, y, w, h, scale float32, color uint32, secs int) {
	bg := gdip.NewBrush(gdip.ARGB(28, 255, 255, 255))
	c.FillRect(bg, x, y, w, h)
	bg.Close()

	// y range: at least 33 ms, otherwise p99 * 1.25 rounded up to 10 ms
	ymax := 33.4
	if len(ft) > 0 {
		sorted := append([]float32(nil), ft...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
		p99 := float64(sorted[int(float64(len(sorted)-1)*0.99)])
		if v := math.Ceil(p99*1.25/10) * 10; v > ymax {
			ymax = math.Min(v, 250)
		}
	}
	ref := gdip.NewPen(gdip.ARGB(60, 255, 255, 255), 1)
	ref.Dash()
	for _, ms := range []float64{16.7, 33.3} {
		if ms < ymax {
			ly := y + h - float32(ms/ymax)*h
			c.Line(ref, x, ly, x+w, ly)
		}
	}
	ref.Close()
	if len(ft) < 2 {
		return
	}
	// Downsample to one point per pixel column, keeping the worst frame so
	// stutters stay visible.
	var total float64
	for _, v := range ft {
		total += float64(v)
	}
	span := math.Max(total, float64(secs)*1000)
	cols := int(w)
	if cols < 2 {
		return
	}
	colMax := make([]float32, cols)
	has := make([]bool, cols)
	t := span - total // right-align data when shorter than the window
	for _, v := range ft {
		t += float64(v)
		ci := int(t / span * float64(cols-1))
		if ci < 0 {
			ci = 0
		}
		if ci >= cols {
			ci = cols - 1
		}
		if !has[ci] || v > colMax[ci] {
			colMax[ci] = v
			has[ci] = true
		}
	}
	pts := make([]gdip.PointF, 0, cols+2)
	for i := 0; i < cols; i++ {
		if !has[i] {
			continue
		}
		v := math.Min(float64(colMax[i]), ymax)
		pts = append(pts, gdip.PointF{X: x + float32(i), Y: y + h - float32(v/ymax)*h})
	}
	if len(pts) < 2 {
		return
	}
	area := append([]gdip.PointF{{X: pts[0].X, Y: y + h}}, pts...)
	area = append(area, gdip.PointF{X: pts[len(pts)-1].X, Y: y + h})
	fill := gdip.NewBrush(gdip.WithAlpha(color, 60))
	c.FillPolygon(fill, area)
	fill.Close()
	pen := gdip.NewPen(gdip.WithAlpha(color, 240), float32(math.Max(1, float64(1.25*scale))))
	c.Lines(pen, pts)
	pen.Close()
}

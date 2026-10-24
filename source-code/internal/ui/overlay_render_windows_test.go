//go:build windows

package ui

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"unsafe"

	"glance/internal/config"
	"glance/internal/data"
	"glance/internal/gdip"
	"glance/internal/layout"
)

// Opt-in native rendering QA: set GLANCE_RENDER_DIR to export PNGs for inspection.
// Uses the same GDI+ renderer as the layered window, with explicit synthetic data.
func TestNativeOverlayRendering(t *testing.T) {
	dir := os.Getenv("GLANCE_RENDER_DIR")
	if dir == "" {
		t.Skip("set GLANCE_RENDER_DIR for native GDI+ rendering QA")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if !gdip.Startup() {
		t.Fatal("GDI+ startup failed")
	}
	defer gdip.Shutdown()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	s := data.NewSnapshot()
	s.FPSActive = true
	s.Unsupported = map[string]bool{data.GPUPower: true, data.CPUVRM: true}
	for k, v := range map[string]float64{
		data.CPUUsage: 34, data.CPUClock: 5300, data.CPUTemp: 72, data.CPUPower: 64,
		data.GPUUsage: 98, data.GPUClock: 2445, data.GPUTemp: 64, data.GPUVRAM: 9.5,
		data.FPS: 160, data.FPSLow1: 123, data.FPSLow01: 98, data.FrameTime: 6.25,
	} {
		s.Set(k, v)
	}
	for i := 0; i < 1600; i++ {
		ms := float32(6.25)
		if i%137 == 0 {
			ms = 22
		}
		s.FrameTimes = append(s.FrameTimes, ms)
	}
	s.FrameSpan = 10
	for _, mode := range []string{"rows", "line", "list"} {
		for _, scale := range []float32{1, 1.5} {
			t.Run(fmt.Sprintf("%s-%.0f", mode, scale*100), func(t *testing.T) {
				c := config.Default()
				c.Overlay.Layout = mode
				c.Overlay.Graph = true
				r := newOverlayRenderer()
				defer r.close()
				canvas := r.render(c, layout.Build(c, s, false), scale, false)
				if !canvas.Valid() || canvas.W < 100 || canvas.H < 50 {
					t.Fatal("invalid render dimensions")
				}
				pixels := unsafe.Slice((*uint32)(canvas.Bits), int(canvas.W*canvas.H))
				im := image.NewRGBA(image.Rect(0, 0, int(canvas.W), int(canvas.H)))
				bright, graph := 0, 0
				for i, p := range pixels {
					r, g, b, a := byte(p>>16), byte(p>>8), byte(p), byte(p>>24)
					im.Pix[4*i], im.Pix[4*i+1], im.Pix[4*i+2], im.Pix[4*i+3] = r, g, b, a
					if r > 200 && g > 200 && b > 200 {
						bright++
					}
					if r > 200 && g > 100 && g < 210 && b < 100 {
						graph++
					}
				}
				if bright < 100 || graph < 20 {
					t.Fatalf("missing text/graph: white=%d graph=%d", bright, graph)
				}
				name := filepath.Join(dir, fmt.Sprintf("%s-%.0f.png", mode, scale*100))
				f, err := os.Create(name)
				if err != nil {
					t.Fatal(err)
				}
				err = png.Encode(f, im)
				closeErr := f.Close()
				if err != nil {
					t.Fatal(err)
				}
				if closeErr != nil {
					t.Fatal(closeErr)
				}
				t.Logf("%dx%d -> %s", canvas.W, canvas.H, name)
			})
		}
	}
}

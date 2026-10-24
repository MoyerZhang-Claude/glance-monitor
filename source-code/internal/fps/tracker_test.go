package fps

import (
	"math"
	"testing"
)

const freq = 10_000_000

func feed(t *Tracker, pid uint32, chain uint64, src Source, start int64, intervalsMs []float64) int64 {
	ts := start
	t.Add(Event{PID: pid, TS: ts, Src: src, Chain: chain})
	for _, ms := range intervalsMs {
		ts += int64(ms * freq / 1000)
		t.Add(Event{PID: pid, TS: ts, Src: src, Chain: chain})
	}
	return ts
}

func repeat(ms float64, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = ms
	}
	return out
}

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestSteady60(t *testing.T) {
	tr := NewTracker(freq, 30)
	end := feed(tr, 100, 1, SrcDXGI, 1_000_000, repeat(1000.0/60, 600))
	st := tr.Query(100, end, Options{LowWindowSec: 20, Method: "avg"})
	if !st.Active || !near(st.FPS, 60, 0.2) {
		t.Fatalf("fps=%v active=%v", st.FPS, st.Active)
	}
	if !near(st.Low1, 60, 0.5) || !near(st.FrameTime, 16.67, 0.05) {
		t.Fatalf("low1=%v ft=%v", st.Low1, st.FrameTime)
	}
	if st.Src != SrcDXGI {
		t.Fatalf("src=%v", st.Src)
	}
}

func TestLowsWithStutter(t *testing.T) {
	tr := NewTracker(freq, 30)
	iv := repeat(10, 990) // 100 fps
	for i := 0; i < 10; i++ {
		iv[i*99] = 50 // 10 stutters of 50 ms (=1% of 1000 frames)
	}
	end := feed(tr, 7, 1, SrcDXGI, 0, iv)
	st := tr.Query(7, end, Options{LowWindowSec: 60, Method: "avg"})
	if !st.HasLows {
		t.Fatal("no lows")
	}
	// slowest 1% = 10 frames of 50ms -> 20 fps
	if !near(st.Low1, 20, 0.01) {
		t.Fatalf("low1=%v", st.Low1)
	}
	if !near(st.Low01, 20, 0.01) {
		t.Fatalf("low01=%v", st.Low01)
	}
	pct := tr.Query(7, end, Options{LowWindowSec: 60, Method: "pct"})
	if !near(pct.Low1, 20, 0.01) {
		t.Fatalf("pct low1=%v", pct.Low1)
	}
}

func TestStaleAndGap(t *testing.T) {
	tr := NewTracker(freq, 30)
	end := feed(tr, 5, 1, SrcDXGI, 0, repeat(5, 400))
	if st := tr.Query(5, end+3*freq, Options{LowWindowSec: 20}); st.Active {
		t.Fatal("should be stale")
	}
	// A 3 s pause followed by frames must not poison the lows.
	end2 := feed(tr, 5, 1, SrcDXGI, end+3*freq, repeat(5, 400))
	st := tr.Query(5, end2, Options{LowWindowSec: 20})
	if !near(st.Low1, 200, 1) || !near(st.FPS, 200, 1) {
		t.Fatalf("after gap low1=%v fps=%v", st.Low1, st.FPS)
	}
}

func TestPicksBusiestChainAndKernelFallback(t *testing.T) {
	tr := NewTracker(freq, 30)
	feed(tr, 9, 0xA, SrcDXGI, 0, repeat(1000.0/144, 300)) // main game swapchain
	end := feed(tr, 9, 0xB, SrcDXGI, 0, repeat(100, 20))   // small secondary window
	st := tr.Query(9, end, Options{LowWindowSec: 10})
	if !near(st.FPS, 144, 1) {
		t.Fatalf("fps=%v", st.FPS)
	}
	// Vulkan style process: kernel events only.
	kend := feed(tr, 11, 0, SrcKernel, 0, repeat(1000.0/90, 300))
	ks := tr.Query(11, kend, Options{LowWindowSec: 10})
	if !near(ks.FPS, 90, 0.5) || ks.Src != SrcKernel {
		t.Fatalf("kernel fps=%v src=%v", ks.FPS, ks.Src)
	}
	// Runtime events take priority over kernel ones for the same process.
	feed(tr, 12, 0, SrcKernel, 0, repeat(1000.0/60, 120))
	rend := feed(tr, 12, 3, SrcDXGI, 0, repeat(1000.0/60, 120))
	if rs := tr.Query(12, rend, Options{}); rs.Src != SrcDXGI {
		t.Fatalf("src=%v", rs.Src)
	}
}

func TestOutOfOrderInsert(t *testing.T) {
	tr := NewTracker(freq, 30)
	base := int64(0)
	order := []int64{0, 2, 1, 3, 5, 4, 6, 7, 9, 8, 10}
	for _, k := range order {
		tr.Add(Event{PID: 1, TS: base + k*int64(freq/100), Src: SrcDXGI, Chain: 1})
	}
	st := tr.Query(1, base+10*int64(freq/100), Options{LowWindowSec: 5, GraphSec: 5})
	if !near(st.FPS, 100, 0.01) {
		t.Fatalf("fps=%v", st.FPS)
	}
	for _, g := range st.Graph {
		if !near(float64(g), 10, 0.001) {
			t.Fatalf("graph=%v", st.Graph)
		}
	}
}

func TestTrimKeepsWindow(t *testing.T) {
	tr := NewTracker(freq, 5)
	end := feed(tr, 2, 1, SrcDXGI, 0, repeat(1, 20000)) // 20 s at 1000 fps
	tr.mu.Lock()
	n := len(tr.procs[2].chains[1].live())
	tr.mu.Unlock()
	if n > 5100 || n < 4900 {
		t.Fatalf("kept %d", n)
	}
	if st := tr.Query(2, end, Options{LowWindowSec: 3}); !near(st.FPS, 1000, 1) {
		t.Fatalf("fps=%v", st.FPS)
	}
}

func TestFrameGenPacing(t *testing.T) {
	tr := NewTracker(freq, 30)
	var iv []float64
	for i := 0; i < 1500; i++ { // MFG 3x at 47 fps base -> 141 fps output, bursty presents
		iv = append(iv, 0.3, 0.3, 20.67)
	}
	end := feed(tr, 3, 1, SrcDXGI, 0, iv)
	st := tr.Query(3, end, Options{LowWindowSec: 20, Method: "avg", GraphSec: 5})
	if !st.FrameGen {
		t.Fatal("frame generation not detected")
	}
	if !near(st.FPS, 141.1, 1) {
		t.Fatalf("fps=%v", st.FPS)
	}
	if !near(st.Low1, 141.1, 3) {
		t.Fatalf("paced low1=%v (raw would be ~48)", st.Low1)
	}
	for _, g := range st.Graph[3:] {
		if !near(float64(g), 7.09, 0.2) {
			t.Fatalf("graph not paced: %v", st.Graph[:12])
		}
	}
	// A normal game with occasional quick frames must not be paced.
	tr2 := NewTracker(freq, 30)
	iv2 := repeat(8, 1000)
	for i := 0; i < 1000; i += 50 {
		iv2[i] = 1
	}
	end2 := feed(tr2, 4, 1, SrcDXGI, 0, iv2)
	if st2 := tr2.Query(4, end2, Options{LowWindowSec: 20}); st2.FrameGen {
		t.Fatal("false frame-gen detection")
	}
}

func TestFrameGenVariants(t *testing.T) {
	cases := []struct {
		name string
		pat  []float64
		fg   bool
	}{
		{"mfg3-tight", []float64{0.3, 0.3, 20.7}, true},
		{"mfg3-loose", []float64{2.5, 2.5, 16.3}, true},
		{"fg2", []float64{0.8, 15.9}, true},
		{"mfg4", []float64{0.2, 0.2, 0.2, 27}, true},
		{"judder-8-25", []float64{8, 25}, false},
		{"steady", []float64{6.9}, false},
	}
	for _, c := range cases {
		var iv []float64
		for len(iv) < 1200 {
			for _, ms := range c.pat {
				iv = append(iv, ms/1000)
			}
		}
		_, got := paceFrameGen(iv)
		if got != c.fg {
			t.Errorf("%s: fg=%v want %v", c.name, got, c.fg)
		}
		if got {
			out, _ := paceFrameGen(iv)
			var period float64
			for _, d := range c.pat {
				period += d
			}
			want := period / float64(len(c.pat)) / 1000
			for _, d := range out[len(c.pat):len(out)-len(c.pat)] {
				if !near(d, want, 1e-5) {
					t.Errorf("%s: paced %v want %v", c.name, d, want)
					break
				}
			}
		}
	}
}

func TestLowsFollowScene(t *testing.T) {
	tr := NewTracker(freq, 30)
	// 10 s of a 60 fps menu, then 6 s of 150 fps gameplay with two stutters.
	iv := repeat(1000.0/60, 600)
	game := repeat(1000.0/150, 900)
	game[300], game[700] = 45, 60
	iv = append(iv, game...)
	end := feed(tr, 8, 1, SrcDXGI, 0, iv)
	st := tr.Query(8, end, Options{LowWindowSec: 20, Method: "avg"})
	if st.Frames > 950 || st.Frames < 850 {
		t.Fatalf("scene frames=%d, want ~900 (menu frames must be dropped)", st.Frames)
	}
	// slowest 1% of ~900 frames = 9 frames: the two stutters + 7 normal ones
	want := 9 / ((45 + 60 + 7*1000.0/150) / 1000)
	if !near(st.Low1, want, 1) {
		t.Fatalf("low1=%v want %v (stutters must still count)", st.Low1, want)
	}
}

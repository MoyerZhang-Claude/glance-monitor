// Package fps turns per-process present events (from ETW on Windows) into
// frame rate statistics: average FPS, 1% / 0.1% lows and frame times.
// This file is platform independent and unit tested.
package fps

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
)

// Source identifies where a present event came from.
type Source uint8

const (
	SrcDXGI Source = iota + 1
	SrcD3D9
	SrcKernel // DxgKrnl present history (Vulkan / OpenGL / anything else)
)

func (s Source) String() string {
	switch s {
	case SrcDXGI:
		return "DXGI"
	case SrcD3D9:
		return "D3D9"
	case SrcKernel:
		return "Vulkan/GL"
	}
	return ""
}

// Event is one present call.
type Event struct {
	PID   uint32
	TS    int64 // QPC ticks
	Src   Source
	Chain uint64 // swap chain address for runtime events, 0 for kernel
}

type stream struct {
	ts   []int64 // ascending
	head int
	src  Source
}

func (s *stream) live() []int64 { return s.ts[s.head:] }

func (s *stream) last() int64 {
	if len(s.ts) == s.head {
		return 0
	}
	return s.ts[len(s.ts)-1]
}

func (s *stream) add(t int64) {
	s.ts = append(s.ts, t)
	// Real-time ETW buffers from different CPUs can arrive slightly out of
	// order; keep the slice sorted with a short insertion step.
	for i := len(s.ts) - 1; i > s.head && s.ts[i-1] > s.ts[i]; i-- {
		s.ts[i-1], s.ts[i] = s.ts[i], s.ts[i-1]
	}
}

func (s *stream) trim(before int64) {
	for s.head < len(s.ts) && s.ts[s.head] < before {
		s.head++
	}
	if s.head > 4096 && s.head > len(s.ts)/2 {
		n := copy(s.ts, s.ts[s.head:])
		s.ts = s.ts[:n]
		s.head = 0
	}
}

// countSince returns how many timestamps are >= t.
func (s *stream) countSince(t int64) int {
	l := s.live()
	i := sort.Search(len(l), func(i int) bool { return l[i] >= t })
	return len(l) - i
}

type proc struct {
	chains   map[uint64]*stream
	kernel   *stream
	lastSeen int64
}

// Tracker accumulates events. It is safe for concurrent use.
type Tracker struct {
	mu     sync.Mutex
	freq   float64
	keep   int64 // ticks of history kept per stream
	procs  map[uint32]*proc
	adds   int
}

// NewTracker creates a tracker for a QPC frequency, keeping keepSec seconds of history.
func NewTracker(qpcFreq int64, keepSec float64) *Tracker {
	if qpcFreq <= 0 {
		qpcFreq = 10_000_000
	}
	return &Tracker{freq: float64(qpcFreq), keep: int64(keepSec * float64(qpcFreq)), procs: map[uint32]*proc{}}
}

// SetKeep changes how many seconds of history are retained.
func (t *Tracker) SetKeep(sec float64) {
	t.mu.Lock()
	t.keep = int64(sec * t.freq)
	t.mu.Unlock()
}

// Add records a present event.
func (t *Tracker) Add(e Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	p := t.procs[e.PID]
	if p == nil {
		p = &proc{chains: map[uint64]*stream{}}
		t.procs[e.PID] = p
	}
	var s *stream
	if e.Src == SrcKernel {
		if p.kernel == nil {
			p.kernel = &stream{src: SrcKernel}
		}
		s = p.kernel
	} else {
		s = p.chains[e.Chain]
		if s == nil {
			s = &stream{src: e.Src}
			p.chains[e.Chain] = s
		}
	}
	s.add(e.TS)
	if e.TS > p.lastSeen {
		p.lastSeen = e.TS
	}
	s.trim(e.TS - t.keep)
	t.adds++
	if t.adds%4096 == 0 {
		t.pruneLocked(e.TS)
	}
}

func (t *Tracker) pruneLocked(now int64) {
	idle := int64(15 * t.freq)
	for pid, p := range t.procs {
		if now-p.lastSeen > idle {
			delete(t.procs, pid)
			continue
		}
		for id, s := range p.chains {
			if now-s.last() > idle {
				delete(p.chains, id)
			}
		}
	}
}

// Options control a stats query.
type Options struct {
	LowWindowSec float64 // window for 1% / 0.1% lows
	Method       string  // "avg" (mean of slowest N%) or "pct" (percentile)
	GraphSec     float64 // seconds of frame times to return (0 = none)
	StaleSec     float64 // no events for this long => inactive
}

// Stats is the result for one process.
type Stats struct {
	Active     bool
	FPS        float64
	FrameTime  float64 // ms, mean over the last second
	Low1       float64
	Low01      float64
	HasLows    bool
	Src        Source
	Graph      []float32 // ms, oldest first
	GraphSpan  float64
	Frames     int  // frames in the low window
	FrameGen   bool // bursty presents from frame generation were detected and paced
}

// PresentRate returns the number of presents in the last second for pid
// (best stream), its source, and whether it is presenting at all.
func (t *Tracker) PresentRate(pid uint32, now int64, staleSec float64) (int, Source, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	p := t.procs[pid]
	if p == nil {
		return 0, 0, false
	}
	s := t.pick(p, now, staleSec)
	if s == nil {
		return 0, 0, false
	}
	return s.countSince(s.last() - int64(t.freq)), s.src, true
}

func (t *Tracker) pick(p *proc, now int64, staleSec float64) *stream {
	stale := int64(staleSec * t.freq)
	var best *stream
	bestN := -1
	for _, s := range p.chains {
		last := s.last()
		if last == 0 || now-last > stale {
			continue
		}
		n := s.countSince(last - int64(t.freq))
		if n > bestN {
			best, bestN = s, n
		}
	}
	if best != nil {
		return best
	}
	if p.kernel != nil {
		last := p.kernel.last()
		if last != 0 && now-last <= stale {
			return p.kernel
		}
	}
	return nil
}

// Query computes statistics for pid at QPC time now.
func (t *Tracker) Query(pid uint32, now int64, o Options) Stats {
	if o.StaleSec <= 0 {
		o.StaleSec = 2.5
	}
	t.mu.Lock()
	p := t.procs[pid]
	var ts []int64
	var src Source
	if p != nil {
		if s := t.pick(p, now, o.StaleSec); s != nil {
			l := s.live()
			ts = make([]int64, len(l))
			copy(ts, l)
			src = s.src
		}
	}
	freq := t.freq
	t.mu.Unlock()
	return compute(ts, src, freq, o)
}

// maxGap: intervals longer than this are pauses (loading, alt-tab), not frames.
const maxGapSec = 1.0

// intervalsSince returns present-to-present intervals (seconds, in time
// order) for presents at or after t.
func intervalsSince(ts []int64, t int64, freq float64) []float64 {
	start := sort.Search(len(ts), func(i int) bool { return ts[i] >= t })
	if start < 1 {
		start = 1
	}
	out := make([]float64, 0, len(ts)-start)
	for i := start; i < len(ts); i++ {
		out = append(out, float64(ts[i]-ts[i-1])/freq)
	}
	return out
}

// paceFrameGen detects frame generation that submits several frames in a
// burst (e.g. DLSS multi frame generation with hardware flip metering on
// RTX 50: presents arrive as ~0.3 ms, 0.3 ms, 20 ms...). The display shows
// those frames evenly, so each burst's span is split evenly across its
// frames. Normal games are returned unchanged.
//
// Detection: intervals form two clearly separated clusters — many short
// ones (>= 30%) that are less than a quarter of the long ones. Uneven
// pacing without frame generation (e.g. 8 ms / 25 ms judder) is not
// separated enough and stays untouched, so real stutter remains visible.
func paceFrameGen(iv []float64) ([]float64, bool) {
	thr, ok := frameGenThreshold(iv)
	if !ok {
		return iv, false
	}
	out := make([]float64, 0, len(iv))
	// iv[i] is the gap before present i+1. A burst = presents joined by
	// short gaps; the long gap after the burst ends it. The window may start
	// mid-burst, so skip up to and including the first long gap.
	first := 0
	for first < len(iv) && iv[first] < thr {
		first++
	}
	if first < len(iv) {
		iv = iv[first+1:]
	}
	burstSpan, k := 0.0, 0
	for _, d := range iv {
		burstSpan += d
		k++
		if d >= thr {
			if d > maxGapSec {
				out = append(out, d)
			} else {
				per := burstSpan / float64(k)
				for j := 0; j < k; j++ {
					out = append(out, per)
				}
			}
			burstSpan, k = 0, 0
		}
	}
	// trailing incomplete burst: keep raw so no time is invented
	if k > 0 {
		out = append(out, iv[len(iv)-k:]...)
	}
	return out, true
}

func frameGenThreshold(iv []float64) (float64, bool) {
	if len(iv) < 30 {
		return 0, false
	}
	var sum float64
	n := 0
	for _, d := range iv {
		if d <= maxGapSec {
			sum += d
			n++
		}
	}
	if n < 30 {
		return 0, false
	}
	mean := sum / float64(n)
	var short, long []float64
	for _, d := range iv {
		if d > maxGapSec {
			continue
		}
		if d < 0.5*mean {
			short = append(short, d)
		} else {
			long = append(long, d)
		}
	}
	if float64(len(short)) < 0.3*float64(n) || float64(len(long)) < 0.1*float64(n) {
		return 0, false
	}
	medS, medL := median(short), median(long)
	if medL <= 0 || medS >= 0.25*medL {
		return 0, false
	}
	return math.Sqrt(math.Max(medS, 1e-5) * medL), true
}

// sceneFrames returns how many of the newest intervals belong to the
// current scene, within at most windowSec seconds.
func sceneFrames(all []float64, windowSec float64) int {
	type bucket struct{ n int; med float64 }
	var buckets []bucket
	i := len(all)
	total := 0.0
	for i > 0 && total < windowSec {
		start := i
		w := 0.0
		for i > 0 && w < 1.0 && total+w < windowSec {
			i--
			w += all[i]
		}
		total += w
		buckets = append(buckets, bucket{n: start - i, med: median(all[i:start])})
	}
	if len(buckets) == 0 {
		return 0
	}
	cur := buckets[0].med
	n := buckets[0].n
	for _, b := range buckets[1:] {
		if cur <= 0 || math.Abs(b.med-cur)/cur > 0.35 {
			break
		}
		n += b.n
	}
	return n
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	c := append([]float64(nil), v...)
	sort.Float64s(c)
	return c[len(c)/2]
}

// Debug describes the recent present cadence of pid (for the log).
func (t *Tracker) Debug(pid uint32, now int64) string {
	t.mu.Lock()
	p := t.procs[pid]
	var ts []int64
	chains := 0
	if p != nil {
		chains = len(p.chains)
		if s := t.pick(p, now, 2.5); s != nil {
			l := s.live()
			ts = append(ts, l...)
		}
	}
	freq := t.freq
	t.mu.Unlock()
	if len(ts) < 3 {
		return "no data"
	}
	iv := intervalsSince(ts, ts[len(ts)-1]-int64(5*freq), freq)
	sorted := append([]float64(nil), iv...)
	sort.Float64s(sorted)
	q := func(f float64) float64 { return sorted[int(f*float64(len(sorted)-1))] * 1000 }
	thr, fg := frameGenThreshold(iv)
	var b strings.Builder
	fmt.Fprintf(&b, "chains=%d n=%d p1=%.2f p10=%.2f p50=%.2f p90=%.2f p99=%.2f max=%.2f fg=%v thr=%.2fms seq:",
		chains, len(iv), q(0.01), q(0.10), q(0.5), q(0.9), q(0.99), sorted[len(sorted)-1]*1000, fg, thr*1000)
	from := len(iv) - 40
	if from < 0 {
		from = 0
	}
	for _, d := range iv[from:] {
		fmt.Fprintf(&b, " %.1f", d*1000)
	}
	return b.String()
}

func compute(ts []int64, src Source, freq float64, o Options) Stats {
	st := Stats{Src: src}
	if len(ts) < 2 {
		return st
	}
	last := ts[len(ts)-1]
	st.Active = true

	// Average FPS over the last second (frame count / time; pacing-independent).
	oneSec := last - int64(freq)
	var n int
	var sum float64
	for i := len(ts) - 1; i > 0 && ts[i-1] >= oneSec; i-- {
		d := float64(ts[i]-ts[i-1]) / freq
		if d > maxGapSec {
			break
		}
		sum += d
		n++
	}
	if n == 0 {
		// Fewer than two frames in the last second: use the last interval.
		d := float64(ts[len(ts)-1]-ts[len(ts)-2]) / freq
		if d > 0 && d <= 5 {
			sum, n = d, 1
		}
	}
	if n > 0 && sum > 0 {
		st.FPS = float64(n) / sum
		st.FrameTime = sum / float64(n) * 1000
	}

	// Frame intervals over the longest window we need, paced if bursty.
	span := o.LowWindowSec
	if o.GraphSec > span {
		span = o.GraphSec
	}
	all, fg := paceFrameGen(intervalsSince(ts, last-int64(span*freq), freq))
	st.FrameGen = fg

	// Lows over the configured window (newest intervals at the end),
	// restricted to the current "scene": when the typical frame time changes
	// a lot (menu, loading, alt-tab, a capped cut-scene), older frames from
	// the previous scene are dropped so the 1% low describes what is on
	// screen now. Isolated stutters do not move the median and stay counted.
	lowN := sceneFrames(all, o.LowWindowSec)
	var iv []float64
	for _, d := range all[len(all)-lowN:] {
		if d > 0 && d <= maxGapSec {
			iv = append(iv, d)
		}
	}
	st.Frames = len(iv)
	if len(iv) >= 10 {
		sort.Sort(sort.Reverse(sort.Float64Slice(iv))) // slowest first
		st.Low1 = low(iv, 0.01, o.Method)
		st.Low01 = low(iv, 0.001, o.Method)
		st.HasLows = true
	}

	// Frame time graph.
	if o.GraphSec > 0 {
		w := 0.0
		start := len(all)
		for start > 0 && w+all[start-1] <= o.GraphSec {
			w += all[start-1]
			start--
		}
		for _, d := range all[start:] {
			st.Graph = append(st.Graph, float32(math.Min(d, maxGapSec)*1000))
		}
		st.GraphSpan = w
	}
	return st
}

// low computes the "N% low" frame rate from intervals sorted slowest first.
func low(iv []float64, frac float64, method string) float64 {
	k := int(math.Ceil(float64(len(iv)) * frac))
	if k < 1 {
		k = 1
	}
	if k > len(iv) {
		k = len(iv)
	}
	if method == "pct" {
		return 1 / iv[k-1]
	}
	var s float64
	for _, d := range iv[:k] {
		s += d
	}
	return float64(k) / s
}

// ProcInfo summarises a presenting process for diagnostics.
type ProcInfo struct {
	PID  uint32
	Rate int
	Src  Source
}

// Presenting lists processes that presented within staleSec of now.
func (t *Tracker) Presenting(now int64, staleSec float64) []ProcInfo {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []ProcInfo
	for pid, p := range t.procs {
		if s := t.pick(p, now, staleSec); s != nil {
			out = append(out, ProcInfo{PID: pid, Rate: s.countSince(s.last() - int64(t.freq)), Src: s.src})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rate > out[j].Rate })
	return out
}

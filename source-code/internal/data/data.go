// Package data holds the platform-independent sensor snapshot model and the
// metric catalog shared by the overlay, the tray card and the settings UI.
package data

import (
	"math"
	"strconv"
	"time"
)

// Metric IDs.
const (
	CPUUsage   = "cpu.usage"
	CPUClock   = "cpu.clock"
	CPUTemp    = "cpu.temp"
	CPUTempMax = "cpu.tempmax"
	CPUPower   = "cpu.power"
	CPUVRM     = "cpu.vrm"

	GPUUsage   = "gpu.usage"
	GPUClock   = "gpu.clock"
	GPUTemp    = "gpu.temp"
	GPUHotspot = "gpu.hotspot"
	GPUMemTemp = "gpu.memtemp"
	GPUPower   = "gpu.power"
	GPUVRAM    = "gpu.vram"
	GPUMemClk  = "gpu.memclock"
	GPUFan     = "gpu.fan"

	FPS       = "fps.fps"
	FPSLow1   = "fps.low1"
	FPSLow01  = "fps.low01"
	FrameTime = "fps.frametime"

	RAMUsage = "ram.usage"
	RAMUsed  = "ram.used"

	Clock = "time.clock"
)

// Kind decides number formatting and which threshold semantics apply.
type Kind int

const (
	KindPercent Kind = iota
	KindTemp
	KindPower
	KindClock
	KindFPS
	KindMs
	KindGB
	KindTime
)

// MetricInfo describes one displayable metric.
type MetricInfo struct {
	ID       string `json:"id"`
	Group    string `json:"group"`
	Name     string `json:"name"`  // full Chinese name (settings, hover card)
	Short    string `json:"short"` // compact name for list layout
	Unit     string `json:"unit"`  // default unit
	Units    []string `json:"units,omitempty"` // selectable units
	Prefix   string `json:"prefix,omitempty"` // default inline prefix
	Kind     Kind   `json:"kind"`
	Hint     string `json:"hint,omitempty"`
	HighBad  bool   `json:"highBad"` // thresholds trigger when value >= limit (else <=)
}

// Catalog lists every metric in default display order.
var Catalog = []MetricInfo{
	{ID: CPUUsage, Group: "cpu", Name: "CPU 占用", Short: "占用", Unit: "%", Kind: KindPercent, HighBad: true},
	{ID: CPUClock, Group: "cpu", Name: "CPU 频率", Short: "频率", Unit: "GHz", Units: []string{"GHz", "MHz"}, Kind: KindClock, HighBad: true},
	{ID: CPUTemp, Group: "cpu", Name: "CPU 温度", Short: "温度", Unit: "°C", Kind: KindTemp, HighBad: true, Hint: "CPU 封装温度（Package）"},
	{ID: CPUTempMax, Group: "cpu", Name: "CPU 最热核心", Short: "最热核心", Unit: "°C", Kind: KindTemp, HighBad: true, Hint: "所有核心中最高的温度"},
	{ID: CPUPower, Group: "cpu", Name: "CPU 功耗", Short: "功耗", Unit: "W", Kind: KindPower, HighBad: true, Hint: "CPU 封装功耗（Package Power）"},
	{ID: CPUVRM, Group: "cpu", Name: "CPU 供电温度", Short: "供电", Unit: "°C", Kind: KindTemp, HighBad: true, Hint: "主板 VRM 温度，仅部分台式机主板可读取；读不到时自动隐藏"},

	{ID: GPUUsage, Group: "gpu", Name: "GPU 占用", Short: "占用", Unit: "%", Kind: KindPercent, HighBad: true},
	{ID: GPUClock, Group: "gpu", Name: "GPU 频率", Short: "频率", Unit: "MHz", Units: []string{"MHz", "GHz"}, Kind: KindClock, HighBad: true},
	{ID: GPUTemp, Group: "gpu", Name: "GPU 温度", Short: "温度", Unit: "°C", Kind: KindTemp, HighBad: true},
	{ID: GPUHotspot, Group: "gpu", Name: "GPU 热点温度", Short: "热点", Unit: "°C", Kind: KindTemp, HighBad: true, Hint: "核心内部最热点，需要 PawnIO"},
	{ID: GPUMemTemp, Group: "gpu", Name: "显存温度", Short: "显存温度", Unit: "°C", Kind: KindTemp, HighBad: true, Hint: "部分显卡支持，需要 PawnIO"},
	{ID: GPUPower, Group: "gpu", Name: "GPU 功耗", Short: "功耗", Unit: "W", Kind: KindPower, HighBad: true},
	{ID: GPUVRAM, Group: "gpu", Name: "显存占用", Short: "显存", Unit: "GB", Kind: KindGB, HighBad: true},
	{ID: GPUMemClk, Group: "gpu", Name: "显存频率", Short: "显存频", Unit: "MHz", Kind: KindClock, HighBad: true},
	{ID: GPUFan, Group: "gpu", Name: "GPU 风扇", Short: "风扇", Unit: "%", Kind: KindPercent, HighBad: true, Hint: "笔记本显卡通常不提供"},

	{ID: FPS, Group: "fps", Name: "帧率", Short: "帧率", Unit: "", Kind: KindFPS},
	{ID: FPSLow1, Group: "fps", Name: "1% Low", Short: "1% Low", Unit: "", Prefix: "1%", Kind: KindFPS, Hint: "最慢 1% 帧的平均帧率"},
	{ID: FPSLow01, Group: "fps", Name: "0.1% Low", Short: "0.1% Low", Unit: "", Prefix: "0.1%", Kind: KindFPS, Hint: "最慢 0.1% 帧的平均帧率"},
	{ID: FrameTime, Group: "fps", Name: "帧时间", Short: "帧时间", Unit: "ms", Kind: KindMs, HighBad: true},

	{ID: RAMUsage, Group: "ram", Name: "内存占用", Short: "占用", Unit: "%", Kind: KindPercent, HighBad: true},
	{ID: RAMUsed, Group: "ram", Name: "内存已用", Short: "已用", Unit: "GB", Kind: KindGB, HighBad: true},

	{ID: Clock, Group: "time", Name: "当前时间", Short: "时间", Unit: "", Kind: KindTime},
}

var catalogIndex = func() map[string]*MetricInfo {
	m := map[string]*MetricInfo{}
	for i := range Catalog {
		m[Catalog[i].ID] = &Catalog[i]
	}
	return m
}()

// Info returns metric metadata (nil when unknown).
func Info(id string) *MetricInfo { return catalogIndex[id] }

// GroupInfo describes a metric group.
type GroupInfo struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Label string `json:"label"` // default overlay label
	Color string `json:"color"` // default label colour
}

var Groups = []GroupInfo{
	{ID: "cpu", Name: "处理器", Label: "CPU", Color: "#5AC8FA"},
	{ID: "gpu", Name: "显卡", Label: "GPU", Color: "#7CD992"},
	{ID: "fps", Name: "帧率", Label: "FPS", Color: "#FFB547"},
	{ID: "ram", Name: "内存", Label: "RAM", Color: "#C9A7FF"},
	{ID: "time", Name: "时间", Label: "TIME", Color: "#9AA4B2"},
}

// Value is one sensor reading.
type Value struct {
	V  float64
	OK bool
}

// Reading is a raw sensor entry for the diagnostics list.
type Reading struct {
	Group string  `json:"group"`
	Name  string  `json:"name"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

// Status reports the state of each data source.
type Status struct {
	Elevated      bool   `json:"elevated"`
	PawnIO        string `json:"pawnio"` // ok | missing | denied | failed | unsupported
	PawnIOVersion string `json:"pawnioVersion"`
	NVML          string `json:"nvml"` // ok | missing | failed | none
	GPUSource     string `json:"gpuSource"`
	ETW           string `json:"etw"` // ok | denied | failed | off
	CPUTempSource string `json:"cpuTempSource"`
	VRM           string `json:"vrm"` // ok | none
	Notes         []string `json:"notes"`
}

// Snapshot is a full set of readings taken at one instant.
type Snapshot struct {
	Time       time.Time
	Values     map[string]Value
	CPUName    string
	GPUName    string
	GPUAsleep  bool
	FPSActive  bool
	FPSProcess string
	FPSPid     uint32
	FPSAPI     string
	FrameGen   bool
	FrameTimes []float32 // ms, oldest first, for the graph
	FrameSpan  float64   // seconds covered by FrameTimes
	Readings   []Reading
	Status     Status
	// Unsupported marks metrics this machine cannot provide at all (hidden
	// instead of showing "--").
	Unsupported map[string]bool
}

func NewSnapshot() *Snapshot {
	return &Snapshot{Time: time.Now(), Values: map[string]Value{}}
}

func (s *Snapshot) Set(id string, v float64) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return
	}
	s.Values[id] = Value{V: v, OK: true}
}

func (s *Snapshot) Get(id string) (float64, bool) {
	if s == nil {
		return 0, false
	}
	v, ok := s.Values[id]
	return v.V, ok && v.OK
}

// Format renders a metric value (without unit) for display.
func Format(id string, v float64, unit string) string {
	mi := Info(id)
	if mi == nil {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	switch mi.Kind {
	case KindClock:
		if unit == "GHz" {
			return strconv.FormatFloat(v/1000, 'f', 2, 64)
		}
		return strconv.FormatFloat(math.Round(v), 'f', 0, 64)
	case KindPower:
		if v < 9.95 {
			return strconv.FormatFloat(v, 'f', 1, 64)
		}
		return strconv.FormatFloat(math.Round(v), 'f', 0, 64)
	case KindMs:
		if v >= 99.5 {
			return strconv.FormatFloat(math.Round(v), 'f', 0, 64)
		}
		return strconv.FormatFloat(v, 'f', 1, 64)
	case KindGB:
		return strconv.FormatFloat(v, 'f', 1, 64)
	case KindFPS:
		return strconv.FormatFloat(math.Round(v), 'f', 0, 64)
	case KindTime:
		return time.Now().Format("15:04")
	default:
		return strconv.FormatFloat(math.Round(v), 'f', 0, 64)
	}
}

// Template returns a string whose rendered width is the widest value the
// metric normally shows; used to keep overlay columns from jittering.
func Template(id string, unit string) string {
	mi := Info(id)
	if mi == nil {
		return "888"
	}
	switch mi.Kind {
	case KindClock:
		if unit == "GHz" {
			return "8.88"
		}
		return "8888"
	case KindPower:
		return "888"
	case KindMs:
		return "88.8"
	case KindGB:
		return "88.8"
	case KindFPS:
		return "888"
	case KindTime:
		return "88:88"
	default:
		return "100"
	}
}

// UnitFor returns the unit string to show for a metric given a configured unit.
func UnitFor(id, configured string) string {
	mi := Info(id)
	if mi == nil {
		return ""
	}
	if configured != "" {
		for _, u := range mi.Units {
			if u == configured {
				return u
			}
		}
	}
	return mi.Unit
}

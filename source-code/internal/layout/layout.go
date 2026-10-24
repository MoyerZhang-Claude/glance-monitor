// Package layout turns a sensor snapshot plus the user's configuration into
// a list of text rows for the overlay and sections for the tray hover card.
// It contains no drawing code and is unit tested on any platform.
package layout

import (
	"fmt"
	"strings"

	"glance/internal/config"
	"glance/internal/data"
)

// Cell is one piece of overlay text.
type Cell struct {
	Label       bool   // group label cell
	Prefix      string // small text before the value, e.g. "1%"
	Value       string
	Unit        string
	Template    string // widest expected value, reserves width
	Color       uint32 // value / label colour (RGB)
	PrefixColor uint32
	Dim         bool
	Metric      string
}

type Row struct {
	Group string
	Cells []Cell
}

type Model struct {
	Layout    string
	Rows      []Row
	Graph     []float32
	GraphSpan float64
	ShowGraph bool
}

// optional metrics disappear when their sensor is not available instead of
// showing "--" (e.g. VRM temperature on laptops).
var optional = map[string]bool{
	data.CPUVRM: true, data.CPUTempMax: true, data.GPUHotspot: true, data.GPUMemTemp: true,
	data.GPUFan: true, data.GPUMemClk: true,
}

// ValueColor applies warn/critical thresholds.
func ValueColor(cfg *config.Config, it *config.Item, v float64, base uint32) uint32 {
	if it == nil {
		return base
	}
	mi := data.Info(it.Metric)
	if mi == nil {
		return base
	}
	o := cfg.Overlay
	if mi.HighBad {
		if it.Crit > 0 && v >= it.Crit {
			return config.ParseColor(o.CritColor)
		}
		if it.Warn > 0 && v >= it.Warn {
			return config.ParseColor(o.WarnColor)
		}
	} else {
		if it.Crit > 0 && v <= it.Crit {
			return config.ParseColor(o.CritColor)
		}
		if it.Warn > 0 && v <= it.Warn {
			return config.ParseColor(o.WarnColor)
		}
	}
	return base
}

func fpsValue(snap *data.Snapshot, id string) (float64, bool) {
	if !snap.FPSActive {
		return 0, false
	}
	return snap.Get(id)
}

// Build creates the overlay model. editing=true shows placeholders for
// groups that would otherwise be hidden, so the user can see the full size.
func Build(cfg *config.Config, snap *data.Snapshot, editing bool) Model {
	o := cfg.Overlay
	m := Model{Layout: o.Layout}
	valueCol := config.ParseColor(o.ValueColor)
	unitCol := config.ParseColor(o.UnitColor)

	for gi := range cfg.Groups {
		g := &cfg.Groups[gi]
		if !g.Enabled {
			continue
		}
		groupCol := config.ParseColor(g.Color)
		if g.ID == "fps" && !snap.FPSActive && o.HideIdleFps && !editing {
			continue
		}
		var cells []Cell
		sleeping := g.ID == "gpu" && snap.GPUAsleep
		if sleeping {
			cells = append(cells, Cell{Value: "休眠", Dim: true, Color: unitCol})
		} else {
			for ii := range g.Items {
				it := &g.Items[ii]
				if !it.Enabled {
					continue
				}
				var v float64
				var ok bool
				if g.ID == "fps" {
					v, ok = fpsValue(snap, it.Metric)
				} else {
					v, ok = snap.Get(it.Metric)
				}
				if it.Metric == data.Clock {
					ok = true
				}
				if !ok && (optional[it.Metric] || snap.Unsupported[it.Metric]) {
					continue
				}
				unit := data.UnitFor(it.Metric, it.Unit)
				c := Cell{
					Prefix:      it.Prefix,
					Template:    data.Template(it.Metric, unit),
					Color:       valueCol,
					PrefixColor: unitCol,
					Metric:      it.Metric,
				}
				if o.ShowUnits {
					c.Unit = unit
				}
				if ok {
					c.Value = data.Format(it.Metric, v, unit)
					c.Color = ValueColor(cfg, it, v, valueCol)
				} else {
					c.Value = "--"
					c.Dim = true
					c.Color = unitCol
				}
				cells = append(cells, c)
			}
			if g.ID == "fps" && o.ShowProcess && snap.FPSActive && snap.FPSProcess != "" {
				name := strings.TrimSuffix(snap.FPSProcess, ".exe")
				if r := []rune(name); len(r) > 18 {
					name = string(r[:17]) + "…"
				}
				cells = append(cells, Cell{Value: name, Dim: true, Color: unitCol})
			}
		}
		if len(cells) == 0 {
			continue
		}
		switch o.Layout {
		case "list":
			first := true
			for _, c := range cells {
				name := ""
				if mi := data.Info(c.Metric); mi != nil {
					name = mi.Short
				}
				label := g.Label
				if !first && label != "" {
					label = ""
				}
				first = false
				row := Row{Group: g.ID}
				if o.ShowLabels {
					row.Cells = append(row.Cells, Cell{Label: true, Value: label, Color: groupCol})
				}
				c.Prefix = ""
				if name != "" && c.Metric != data.FPS {
					c.Prefix = name
				} else if c.Metric == data.FPS {
					c.Prefix = "帧率"
				}
				row.Cells = append(row.Cells, c)
				m.Rows = append(m.Rows, row)
			}
		default:
			row := Row{Group: g.ID}
			if o.ShowLabels && g.Label != "" {
				row.Cells = append(row.Cells, Cell{Label: true, Value: g.Label, Color: groupCol})
			}
			row.Cells = append(row.Cells, cells...)
			m.Rows = append(m.Rows, row)
		}
	}
	if o.Layout == "line" && len(m.Rows) > 1 {
		merged := Row{Group: "line"}
		for _, r := range m.Rows {
			merged.Cells = append(merged.Cells, r.Cells...)
		}
		m.Rows = []Row{merged}
	}
	if o.Graph && cfg.Enabled(data.FPS) && (snap.FPSActive || editing) {
		m.ShowGraph = true
		m.Graph = snap.FrameTimes
		m.GraphSpan = snap.FrameSpan
	}
	return m
}

// ---------------- hover card ----------------

type Pair struct {
	Name  string
	Value string
	Unit  string
	Color uint32 // 0 = default text colour
}

type Section struct {
	Title    string
	Subtitle string
	Color    uint32
	Pairs    []Pair
	Note     string
}

// Card builds the tray hover card content: everything we can read, not
// only what is enabled on the overlay.
func Card(cfg *config.Config, snap *data.Snapshot) []Section {
	var out []Section
	add := func(s *Section, id string) {
		v, ok := snap.Get(id)
		if id == data.FPS || id == data.FPSLow1 || id == data.FPSLow01 || id == data.FrameTime {
			v, ok = fpsValue(snap, id)
		}
		mi := data.Info(id)
		if !ok {
			if optional[id] || snap.Unsupported[id] {
				return
			}
			s.Pairs = append(s.Pairs, Pair{Name: mi.Short, Value: "--"})
			return
		}
		unit := mi.Unit
		if it := cfg.Item(id); it != nil {
			unit = data.UnitFor(id, it.Unit)
		}
		var col uint32
		if it := cfg.Item(id); it != nil {
			if c := ValueColor(cfg, it, v, 0); c != 0 {
				col = c
			}
		}
		s.Pairs = append(s.Pairs, Pair{Name: mi.Short, Value: data.Format(id, v, unit), Unit: unit, Color: col})
	}
	gcol := func(id string) uint32 {
		if g := cfg.Group(id); g != nil {
			return config.ParseColor(g.Color)
		}
		return 0xFFFFFF
	}

	cpu := Section{Title: "处理器", Subtitle: snap.CPUName, Color: gcol("cpu")}
	for _, id := range []string{data.CPUUsage, data.CPUClock, data.CPUTemp, data.CPUPower, data.CPUTempMax, data.CPUVRM} {
		add(&cpu, id)
	}
	out = append(out, cpu)

	gpu := Section{Title: "显卡", Subtitle: snap.GPUName, Color: gcol("gpu")}
	if snap.GPUAsleep {
		gpu.Note = "独显休眠中（节能），开始游戏后自动显示"
	} else {
		for _, id := range []string{data.GPUUsage, data.GPUClock, data.GPUTemp, data.GPUPower, data.GPUHotspot, data.GPUVRAM, data.GPUMemTemp, data.GPUFan} {
			add(&gpu, id)
		}
	}
	if snap.GPUName != "" || len(gpu.Pairs) > 0 || gpu.Note != "" {
		out = append(out, gpu)
	}

	fpsSec := Section{Title: "帧率", Color: gcol("fps")}
	if snap.FPSActive {
		fpsSec.Subtitle = strings.TrimSuffix(snap.FPSProcess, ".exe")
		if snap.FPSAPI != "" {
			fpsSec.Subtitle += " · " + snap.FPSAPI
		}
		if snap.FrameGen {
			fpsSec.Subtitle += " · 帧生成"
		}
		for _, id := range []string{data.FPS, data.FrameTime, data.FPSLow1, data.FPSLow01} {
			add(&fpsSec, id)
		}
	} else if snap.Status.ETW == "ok" {
		fpsSec.Note = "当前前台程序没有在渲染画面"
	} else {
		fpsSec.Note = "帧率统计需要管理员权限"
	}
	out = append(out, fpsSec)

	ram := Section{Title: "内存", Color: gcol("ram")}
	if used, ok := snap.Get(data.RAMUsed); ok {
		total, _ := snap.Get("ram.total")
		pct, _ := snap.Get(data.RAMUsage)
		ram.Subtitle = fmt.Sprintf("%.1f / %.0f GB", used, total)
		col := uint32(0)
		if it := cfg.Item(data.RAMUsage); it != nil {
			col = ValueColor(cfg, it, pct, 0)
		}
		ram.Pairs = append(ram.Pairs, Pair{Name: "占用", Value: fmt.Sprintf("%.0f", pct), Unit: "%", Color: col})
	}
	out = append(out, ram)
	return out
}

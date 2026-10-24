package layout

import (
	"testing"

	"glance/internal/config"
	"glance/internal/data"
)

func snap() *data.Snapshot {
	s := data.NewSnapshot()
	s.Set(data.CPUUsage, 23.4)
	s.Set(data.CPUClock, 4850)
	s.Set(data.CPUTemp, 91)
	s.Set(data.CPUPower, 45.2)
	s.Set(data.GPUUsage, 97)
	s.Set(data.GPUClock, 1890)
	s.Set(data.GPUTemp, 70)
	s.Set(data.GPUPower, 110)
	s.Set(data.FPS, 143.6)
	s.Set(data.FPSLow1, 98.2)
	s.FPSActive = true
	s.FPSProcess = "Cyberpunk2077.exe"
	return s
}

func TestRowsDefault(t *testing.T) {
	cfg := config.Default()
	m := Build(cfg, snap(), false)
	if len(m.Rows) != 3 {
		t.Fatalf("rows=%d", len(m.Rows))
	}
	cpu := m.Rows[0]
	if cpu.Cells[0].Value != "CPU" || !cpu.Cells[0].Label {
		t.Fatalf("label %+v", cpu.Cells[0])
	}
	// VRM is optional and missing -> hidden: label + usage, clock, temp, power
	if len(cpu.Cells) != 5 {
		t.Fatalf("cpu cells %d: %+v", len(cpu.Cells), cpu.Cells)
	}
	if cpu.Cells[2].Value != "4.85" || cpu.Cells[2].Unit != "GHz" {
		t.Fatalf("clock %+v", cpu.Cells[2])
	}
	temp := cpu.Cells[3]
	if temp.Value != "91" || temp.Color != config.ParseColor(cfg.Overlay.WarnColor) {
		t.Fatalf("temp %+v", temp)
	}
	fps := m.Rows[2]
	if fps.Cells[1].Value != "144" || fps.Cells[2].Prefix != "1%" || fps.Cells[2].Value != "98" {
		t.Fatalf("fps %+v", fps.Cells)
	}
}

func TestHideIdleFpsAndSleepingGPU(t *testing.T) {
	cfg := config.Default()
	s := snap()
	s.FPSActive = false
	s.GPUAsleep = true
	m := Build(cfg, s, false)
	if len(m.Rows) != 2 {
		t.Fatalf("rows=%d", len(m.Rows))
	}
	if m.Rows[1].Cells[1].Value != "休眠" {
		t.Fatalf("gpu row %+v", m.Rows[1].Cells)
	}
	if e := Build(cfg, s, true); len(e.Rows) != 3 {
		t.Fatalf("edit mode should show fps placeholder row, got %d", len(e.Rows))
	}
}

func TestLineAndList(t *testing.T) {
	cfg := config.Default()
	cfg.Overlay.Layout = "line"
	if m := Build(cfg, snap(), false); len(m.Rows) != 1 || len(m.Rows[0].Cells) < 10 {
		t.Fatalf("line rows=%d", len(m.Rows))
	}
	cfg.Overlay.Layout = "list"
	m := Build(cfg, snap(), false)
	if len(m.Rows) != 10 {
		t.Fatalf("list rows=%d", len(m.Rows))
	}
	if m.Rows[0].Cells[0].Value != "CPU" || m.Rows[1].Cells[0].Value != "" || m.Rows[0].Cells[1].Prefix != "占用" {
		t.Fatalf("list cells %+v / %+v", m.Rows[0].Cells, m.Rows[1].Cells)
	}
}

func TestCard(t *testing.T) {
	cfg := config.Default()
	s := snap()
	s.CPUName = "Intel Core Ultra 9 290HX Plus"
	s.Set(data.RAMUsed, 12.3)
	s.Set("ram.total", 31.6)
	s.Set(data.RAMUsage, 39)
	secs := Card(cfg, s)
	if len(secs) != 4 || secs[0].Subtitle != s.CPUName {
		t.Fatalf("sections %+v", secs)
	}
	if secs[2].Subtitle != "Cyberpunk2077" {
		t.Fatalf("fps subtitle %q", secs[2].Subtitle)
	}
}

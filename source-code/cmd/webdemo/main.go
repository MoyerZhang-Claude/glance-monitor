// Command webdemo serves the settings UI with simulated data so the page
// can be developed and screenshotted without Windows.
package main

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"sync"
	"time"

	"glance/internal/config"
	"glance/internal/data"
	"glance/internal/web"
)

type fake struct {
	mu      sync.Mutex
	cfg     *config.Config
	editing bool
	auto    bool
	t0      time.Time
	game    bool
	asleep  bool
}

func (f *fake) Config() *config.Config { f.mu.Lock(); defer f.mu.Unlock(); return f.cfg.Clone() }
func (f *fake) SetConfig(c *config.Config) (*config.Config, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := c.Clone()
	n.Normalize()
	f.cfg = n
	return n.Clone(), nil
}

func (f *fake) Snapshot() *data.Snapshot {
	t := time.Since(f.t0).Seconds()
	s := data.NewSnapshot()
	w := func(base, amp, freq float64) float64 { return base + amp*math.Sin(t*freq) + rand.Float64()*amp*0.2 }
	s.CPUName = "Intel Core Ultra 9 290HX Plus"
	s.GPUName = "RTX 5070 Ti Laptop GPU"
	s.Set(data.CPUUsage, w(34, 12, 0.7))
	s.Set(data.CPUClock, w(5100, 250, 0.5))
	s.Set(data.CPUTemp, w(82, 6, 0.3))
	s.Set(data.CPUTempMax, w(91, 4, 0.3))
	s.Set(data.CPUPower, w(68, 12, 0.4))
	s.Set(data.RAMUsage, 47)
	s.Set(data.RAMUsed, 15.1)
	s.Set("ram.total", 31.6)
	s.GPUAsleep = f.asleep
	if !f.asleep {
		s.Set(data.GPUUsage, w(96, 3, 0.9))
		s.Set(data.GPUClock, w(2280, 60, 0.6))
		s.Set(data.GPUTemp, w(74, 3, 0.2))
		s.Set(data.GPUHotspot, w(86, 3, 0.2))
		s.Set(data.GPUPower, w(138, 6, 0.8))
		s.Set(data.GPUVRAM, 9.4)
		s.Set(data.GPUMemClk, 1750)
	}
	if f.game {
		s.FPSActive = true
		s.FPSProcess = "Cyberpunk2077.exe"
		s.FPSAPI = "DXGI"
		fps := w(118, 9, 1.3)
		s.Set(data.FPS, fps)
		s.Set(data.FrameTime, 1000/fps)
		s.Set(data.FPSLow1, w(86, 6, 0.4))
		s.Set(data.FPSLow01, w(64, 5, 0.4))
		for i := 0; i < 900; i++ {
			v := 1000/118 + rand.Float64()*1.2
			if i%173 == 0 {
				v += 18
			}
			s.FrameTimes = append(s.FrameTimes, float32(v))
		}
		s.Readings = append(s.Readings, data.Reading{Group: "帧率", Name: "Cyberpunk2077.exe (DXGI)", Value: fps, Unit: "FPS"})
	}
	for i := 0; i < 8; i++ {
		s.Readings = append(s.Readings, data.Reading{Group: "CPU", Name: fmt.Sprintf("核心 #%d (P)", i+1), Value: w(80, 6, 0.3+float64(i)/10), Unit: "°C"})
	}
	s.Readings = append(s.Readings, data.Reading{Group: "CPU", Name: "封装温度 (Package)", Value: w(82, 6, 0.3), Unit: "°C"})
	s.Readings = append(s.Readings, data.Reading{Group: "GPU", Name: "核心温度", Value: w(74, 3, 0.2), Unit: "°C"})
	s.Status = data.Status{Elevated: true, PawnIO: "ok", PawnIOVersion: "2.0.1.0", NVML: "ok", GPUSource: "nvml", ETW: "ok", CPUTempSource: "msr", VRM: "none"}
	return s
}

func (f *fake) Action(name, arg string) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch name {
	case "edit_mode":
		f.editing = arg == "on"
	case "autostart":
		f.auto = arg == "on"
	case "reset_config":
		f.cfg = config.Default()
		return f.cfg.Clone(), nil
	case "diagnostics":
		return "demo diagnostics", nil
	}
	return map[string]bool{"ok": true}, nil
}

func (f *fake) Meta() map[string]any {
	return map[string]any{
		"version":    "1.0.0",
		"fonts":      []string{"Arial", "Bahnschrift", "Bahnschrift SemiBold", "Consolas", "Microsoft YaHei UI", "Segoe UI", "Segoe UI Semibold", "Verdana"},
		"monitors":   []map[string]any{{"device": `\\.\DISPLAY1`, "primary": true, "width": 2560, "height": 1600, "scale": 150}},
		"exe":        `D:\Claude\monitor\Glance.exe`,
		"configPath": `D:\Claude\monitor\Glance.json`,
		"logPath":    `D:\Claude\monitor\glance.log`,
	}
}

func (f *fake) State() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return map[string]any{"editing": f.editing, "overlayShown": true, "autostart": f.auto, "elevated": true, "hotkeyErrors": map[string]string{}}
}

func main() {
	f := &fake{cfg: config.Default(), t0: time.Now(), game: os.Getenv("GAME") != "0", asleep: os.Getenv("ASLEEP") == "1"}
	s, err := web.Start(f)
	if err != nil {
		panic(err)
	}
	fmt.Println(s.URL())
	select {}
}

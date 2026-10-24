// Package config defines Glance's settings, their defaults, validation and
// persistence. It is platform independent so it can be unit tested anywhere.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"glance/internal/data"
)

const CurrentVersion = 3

type Item struct {
	Metric  string  `json:"metric"`
	Enabled bool    `json:"enabled"`
	Prefix  string  `json:"prefix"`
	Unit    string  `json:"unit"`
	Warn    float64 `json:"warn"`
	Crit    float64 `json:"crit"`
}

type Group struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Enabled bool   `json:"enabled"`
	Color   string `json:"color"`
	Items   []Item `json:"items"`
}

type Overlay struct {
	Visible     bool    `json:"visible"`
	Layout      string  `json:"layout"` // rows | line | list
	Anchor      string  `json:"anchor"` // tl tc tr ml mc mr bl bc br
	OffsetX     int     `json:"offsetX"`
	OffsetY     int     `json:"offsetY"`
	Monitor     string  `json:"monitor"` // "" = primary
	Font        string  `json:"font"`
	FontSize    float64 `json:"fontSize"` // px at 100% scaling
	Bold        bool    `json:"bold"`
	ValueColor  string  `json:"valueColor"`
	UnitColor   string  `json:"unitColor"`
	UnitScale   float64 `json:"unitScale"`
	BgColor     string  `json:"bgColor"`
	BgOpacity   int     `json:"bgOpacity"`
	Opacity     int     `json:"opacity"`
	Radius      int     `json:"radius"`
	Padding     int     `json:"padding"`
	ColGap      int     `json:"colGap"`
	RowGap      int     `json:"rowGap"`
	Effect      string  `json:"effect"` // none | shadow | outline
	ShowUnits   bool    `json:"showUnits"`
	ShowLabels  bool    `json:"showLabels"`
	WarnColor   string  `json:"warnColor"`
	CritColor   string  `json:"critColor"`
	Graph       bool    `json:"graph"`
	GraphHeight int     `json:"graphHeight"`
	GraphSecs   int     `json:"graphSecs"`
	GraphColor  string  `json:"graphColor"`
	ShowMode    string  `json:"showMode"` // always | fullscreen
	HideIdleFps bool    `json:"hideIdleFps"`
	ShowProcess bool    `json:"showProcess"`
	HideCapture bool    `json:"hideCapture"`
}

type Tray struct {
	IconMetric string `json:"iconMetric"` // "logo" or a metric id
	IconColor  string `json:"iconColor"`  // "auto" or #RRGGBB
	HoverCard  bool   `json:"hoverCard"`
}

type General struct {
	IntervalMs   int    `json:"intervalMs"`
	AutoElevate  bool   `json:"autoElevate"`
	HotkeyToggle string `json:"hotkeyToggle"`
	HotkeyEdit   string `json:"hotkeyEdit"`
	LowWindowSec int    `json:"lowWindowSec"`
	LowMethod    string `json:"lowMethod"` // avg | pct
	CPUClockMode string `json:"cpuClockMode"` // max | avg | pmax
	WakeGPU      bool     `json:"wakeGpu"`
	FPSCapture   bool     `json:"fpsCapture"`
	FPSExclude   []string `json:"fpsExclude"`
}

// DefaultFPSExclude lists apps that render continuously but are not games.
var DefaultFPSExclude = []string{
	"msedge.exe", "msedgewebview2.exe", "chrome.exe", "firefox.exe", "opera.exe", "brave.exe", "360se.exe", "qqbrowser.exe",
	"claude.exe", "chatgpt.exe", "codex.exe", "ccuwinui.exe", "code.exe", "discord.exe", "qq.exe", "wechat.exe", "weixin.exe", "dingtalk.exe", "feishu.exe",
	"ms-teams.exe", "slack.exe", "steamwebhelper.exe", "epicgameslauncher.exe", "nvidia app.exe", "nvidia overlay.exe",
	"obs64.exe", "wallpaper32.exe", "wallpaper64.exe", "webwallpaper32.exe", "potplayermini64.exe", "vlc.exe",
	"powerpnt.exe", "onenote.exe", "winword.exe", "excel.exe",
}

type Config struct {
	Version int     `json:"version"`
	Overlay Overlay `json:"overlay"`
	Groups  []Group `json:"groups"`
	Tray    Tray    `json:"tray"`
	General General `json:"general"`
}

func defaultItem(id string) Item {
	it := Item{Metric: id}
	switch id {
	case data.CPUUsage, data.CPUClock, data.CPUTemp, data.CPUPower, data.CPUVRM,
		data.GPUUsage, data.GPUClock, data.GPUTemp, data.GPUPower,
		data.FPS, data.FPSLow1, data.RAMUsage, data.Clock:
		it.Enabled = true
	}
	switch id {
	case data.CPUTemp:
		it.Warn, it.Crit = 85, 95
	case data.CPUTempMax:
		it.Warn, it.Crit = 90, 100
	case data.CPUVRM:
		it.Warn, it.Crit = 90, 105
	case data.GPUTemp:
		it.Warn, it.Crit = 80, 87
	case data.GPUHotspot:
		it.Warn, it.Crit = 95, 105
	case data.GPUMemTemp:
		it.Warn, it.Crit = 90, 100
	case data.RAMUsage:
		it.Warn, it.Crit = 85, 95
	}
	if mi := data.Info(id); mi != nil {
		it.Prefix = mi.Prefix
		it.Unit = mi.Unit
	}
	return it
}

// Default returns the factory configuration.
func Default() *Config {
	c := &Config{
		Version: CurrentVersion,
		Overlay: Overlay{
			Visible:     true,
			Layout:      "rows",
			Anchor:      "tl",
			OffsetX:     16,
			OffsetY:     16,
			Font:        "Segoe UI Semibold",
			FontSize:    15,
			Bold:        false,
			ValueColor:  "#FFFFFF",
			UnitColor:   "#AEB6C2",
			UnitScale:   0.72,
			BgColor:     "#0B0F14",
			BgOpacity:   55,
			Opacity:     100,
			Radius:      8,
			Padding:     8,
			ColGap:      12,
			RowGap:      2,
			Effect:      "shadow",
			ShowUnits:   true,
			ShowLabels:  true,
			WarnColor:   "#FFB020",
			CritColor:   "#FF4D4F",
			Graph:       false,
			GraphHeight: 36,
			GraphSecs:   10,
			GraphColor:  "#FFB547",
			ShowMode:    "always",
			HideIdleFps: true,
			ShowProcess: false,
			HideCapture: false,
		},
		Tray: Tray{IconMetric: data.CPUTemp, IconColor: "auto", HoverCard: true},
		General: General{
			IntervalMs:   1000,
			AutoElevate:  true,
			HotkeyToggle: "Ctrl+Alt+H",
			HotkeyEdit:   "Ctrl+Alt+M",
			LowWindowSec: 20,
			LowMethod:    "avg",
			CPUClockMode: "max",
			WakeGPU:      false,
			FPSCapture:   true,
			FPSExclude:   append([]string(nil), DefaultFPSExclude...),
		},
	}
	for _, g := range data.Groups {
		grp := Group{ID: g.ID, Label: g.Label, Enabled: g.ID != "ram" && g.ID != "time", Color: g.Color}
		for _, m := range data.Catalog {
			if m.Group == g.ID {
				grp.Items = append(grp.Items, defaultItem(m.ID))
			}
		}
		c.Groups = append(c.Groups, grp)
	}
	return c
}

var colorRe = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

func normColor(s, def string) string {
	s = strings.TrimSpace(s)
	if colorRe.MatchString(s) {
		return strings.ToUpper(s)
	}
	return def
}

func clampI(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func oneOf(v string, def string, allowed ...string) string {
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	return def
}

// Normalize repairs a config in place: unknown values fall back to
// defaults, numbers are clamped, and groups/items missing from older
// configs are appended so new metrics show up after upgrades.
func (c *Config) Normalize() {
	d := Default()
	if c.Version > 0 && c.Version < 2 && c.General.FPSExclude != nil {
		// v2 added WebView2 hosts (they render continuously) to the defaults.
		c.General.FPSExclude = append(c.General.FPSExclude, "msedgewebview2.exe")
	}
	if c.Version > 0 && c.Version < 3 && c.General.FPSExclude != nil {
		// Desktop AI clients and the OEM dashboard also present continuously.
		c.General.FPSExclude = append(c.General.FPSExclude, "chatgpt.exe", "codex.exe", "ccuwinui.exe")
	}
	c.Version = CurrentVersion
	o := &c.Overlay
	do := d.Overlay
	o.Layout = oneOf(o.Layout, do.Layout, "rows", "line", "list")
	o.Anchor = oneOf(o.Anchor, do.Anchor, "tl", "tc", "tr", "ml", "mc", "mr", "bl", "bc", "br")
	o.OffsetX = clampI(o.OffsetX, -2000, 8000)
	o.OffsetY = clampI(o.OffsetY, -2000, 8000)
	if strings.TrimSpace(o.Font) == "" {
		o.Font = do.Font
	}
	if len(o.Font) > 64 {
		o.Font = o.Font[:64]
	}
	o.FontSize = clampF(o.FontSize, 8, 48)
	o.ValueColor = normColor(o.ValueColor, do.ValueColor)
	o.UnitColor = normColor(o.UnitColor, do.UnitColor)
	o.UnitScale = clampF(o.UnitScale, 0.5, 1)
	o.BgColor = normColor(o.BgColor, do.BgColor)
	o.BgOpacity = clampI(o.BgOpacity, 0, 100)
	o.Opacity = clampI(o.Opacity, 20, 100)
	o.Radius = clampI(o.Radius, 0, 24)
	o.Padding = clampI(o.Padding, 0, 32)
	o.ColGap = clampI(o.ColGap, 2, 48)
	o.RowGap = clampI(o.RowGap, 0, 24)
	o.Effect = oneOf(o.Effect, do.Effect, "none", "shadow", "outline")
	o.WarnColor = normColor(o.WarnColor, do.WarnColor)
	o.CritColor = normColor(o.CritColor, do.CritColor)
	o.GraphHeight = clampI(o.GraphHeight, 16, 160)
	o.GraphSecs = clampI(o.GraphSecs, 2, 60)
	o.GraphColor = normColor(o.GraphColor, do.GraphColor)
	o.ShowMode = oneOf(o.ShowMode, do.ShowMode, "always", "fullscreen")

	t := &c.Tray
	if t.IconMetric != "logo" && data.Info(t.IconMetric) == nil {
		t.IconMetric = d.Tray.IconMetric
	}
	if t.IconColor != "auto" {
		t.IconColor = normColor(t.IconColor, "auto")
	}

	g := &c.General
	dg := d.General
	g.IntervalMs = clampI(g.IntervalMs, 250, 5000)
	g.LowWindowSec = clampI(g.LowWindowSec, 5, 120)
	g.LowMethod = oneOf(g.LowMethod, dg.LowMethod, "avg", "pct")
	g.CPUClockMode = oneOf(g.CPUClockMode, dg.CPUClockMode, "max", "avg", "pmax")
	if g.FPSExclude == nil {
		g.FPSExclude = append([]string(nil), DefaultFPSExclude...)
	}
	seenX := map[string]bool{}
	var excl []string
	for _, n := range g.FPSExclude {
		n = strings.ToLower(strings.TrimSpace(n))
		if n == "" || len(n) > 64 || seenX[n] || len(excl) >= 200 {
			continue
		}
		if !strings.Contains(n, ".") {
			n += ".exe"
		}
		seenX[n] = true
		excl = append(excl, n)
	}
	g.FPSExclude = excl
	if _, _, err := ParseHotkey(g.HotkeyToggle); err != nil && g.HotkeyToggle != "" {
		g.HotkeyToggle = dg.HotkeyToggle
	}
	if _, _, err := ParseHotkey(g.HotkeyEdit); err != nil && g.HotkeyEdit != "" {
		g.HotkeyEdit = dg.HotkeyEdit
	}

	// Groups: keep user order, drop unknown/duplicate, append missing.
	seenG := map[string]bool{}
	var groups []Group
	for _, grp := range c.Groups {
		var def *Group
		for i := range d.Groups {
			if d.Groups[i].ID == grp.ID {
				def = &d.Groups[i]
			}
		}
		if def == nil || seenG[grp.ID] {
			continue
		}
		seenG[grp.ID] = true
		grp.Label = strings.TrimSpace(grp.Label)
		if len([]rune(grp.Label)) > 12 {
			grp.Label = string([]rune(grp.Label)[:12])
		}
		grp.Color = normColor(grp.Color, def.Color)
		seenI := map[string]bool{}
		var items []Item
		for _, it := range grp.Items {
			mi := data.Info(it.Metric)
			if mi == nil || mi.Group != grp.ID || seenI[it.Metric] {
				continue
			}
			seenI[it.Metric] = true
			if len([]rune(it.Prefix)) > 8 {
				it.Prefix = string([]rune(it.Prefix)[:8])
			}
			it.Unit = data.UnitFor(it.Metric, it.Unit)
			it.Warn = clampF(it.Warn, -1e6, 1e6)
			it.Crit = clampF(it.Crit, -1e6, 1e6)
			items = append(items, it)
		}
		for _, di := range def.Items {
			if !seenI[di.Metric] {
				items = append(items, di)
			}
		}
		grp.Items = items
		groups = append(groups, grp)
	}
	for _, dg := range d.Groups {
		if !seenG[dg.ID] {
			groups = append(groups, dg)
		}
	}
	c.Groups = groups
}

// Clone deep-copies the config.
func (c *Config) Clone() *Config {
	b, _ := json.Marshal(c)
	var n Config
	_ = json.Unmarshal(b, &n)
	return &n
}

// Group returns the group with the given id.
func (c *Config) Group(id string) *Group {
	for i := range c.Groups {
		if c.Groups[i].ID == id {
			return &c.Groups[i]
		}
	}
	return nil
}

// Item finds the item config for a metric.
func (c *Config) Item(metric string) *Item {
	for gi := range c.Groups {
		for ii := range c.Groups[gi].Items {
			if c.Groups[gi].Items[ii].Metric == metric {
				return &c.Groups[gi].Items[ii]
			}
		}
	}
	return nil
}

// Enabled reports whether a metric is shown on the overlay.
func (c *Config) Enabled(metric string) bool {
	for _, g := range c.Groups {
		for _, it := range g.Items {
			if it.Metric == metric {
				return g.Enabled && it.Enabled
			}
		}
	}
	return false
}

// Load reads a config file, returning defaults when it does not exist.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), nil
		}
		return Default(), err
	}
	c := Default()
	c.Groups = nil
	if err := json.Unmarshal(b, c); err != nil {
		d := Default()
		return d, fmt.Errorf("配置文件损坏，已使用默认设置: %w", err)
	}
	c.Normalize()
	return c, nil
}

// Save writes the config atomically.
func Save(path string, c *Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ParseColor turns #RRGGBB into 0xRRGGBB.
func ParseColor(s string) uint32 {
	if !colorRe.MatchString(s) {
		return 0xFFFFFF
	}
	var v uint32
	fmt.Sscanf(s[1:], "%06x", &v)
	return v
}

package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"glance/internal/data"
)

func TestFPSExcludeUpgradePreservesCustomizations(t *testing.T) {
	c := Default()
	c.Version = 2
	c.General.FPSExclude = []string{"custom-renderer.exe", "CHATGPT.EXE"}
	c.Normalize()
	for _, name := range []string{"custom-renderer.exe", "chatgpt.exe", "codex.exe", "ccuwinui.exe"} {
		if !slices.Contains(c.General.FPSExclude, name) {
			t.Fatalf("missing %s after upgrade: %v", name, c.General.FPSExclude)
		}
	}
	if len(c.General.FPSExclude) != 4 {
		t.Fatalf("unexpected additions/duplicates: %v", c.General.FPSExclude)
	}
	// Once upgraded, a user's deliberate removal must survive normalization.
	c.General.FPSExclude = []string{"custom-renderer.exe"}
	c.Normalize()
	if !slices.Equal(c.General.FPSExclude, []string{"custom-renderer.exe"}) {
		t.Fatal("user's exclusion choices were overwritten")
	}
}

func TestDefaultsNormalizeIdempotent(t *testing.T) {
	c := Default()
	before := c.Clone()
	c.Normalize()
	if len(c.Groups) != len(before.Groups) {
		t.Fatalf("groups changed")
	}
	for gi := range c.Groups {
		if len(c.Groups[gi].Items) != len(before.Groups[gi].Items) {
			t.Fatalf("items changed in %s", c.Groups[gi].ID)
		}
	}
	if !c.Enabled(data.CPUTemp) || !c.Enabled(data.FPSLow1) || c.Enabled(data.RAMUsage) {
		t.Fatal("unexpected default enablement")
	}
}

func TestNormalizeRepairs(t *testing.T) {
	c := Default()
	c.Overlay.FontSize = 500
	c.Overlay.Layout = "weird"
	c.Overlay.ValueColor = "red"
	c.General.HotkeyToggle = "Ctrl+Nope"
	c.Groups = []Group{
		{ID: "fps", Label: "帧数帧数帧数帧数帧数帧数帧数", Enabled: true, Color: "#123456",
			Items: []Item{{Metric: data.FPS, Enabled: true}, {Metric: data.CPUTemp, Enabled: true}, {Metric: data.FPS}}},
		{ID: "bogus"},
	}
	c.Normalize()
	if c.Overlay.FontSize != 48 || c.Overlay.Layout != "rows" || c.Overlay.ValueColor != "#FFFFFF" {
		t.Fatalf("overlay not repaired: %+v", c.Overlay)
	}
	if c.General.HotkeyToggle != "Ctrl+Alt+H" {
		t.Fatalf("hotkey=%q", c.General.HotkeyToggle)
	}
	if c.Groups[0].ID != "fps" || len(c.Groups) != len(data.Groups) {
		t.Fatalf("group order/count wrong: %v", c.Groups)
	}
	fps := c.Groups[0]
	if len([]rune(fps.Label)) != 12 {
		t.Fatalf("label not truncated")
	}
	if fps.Items[0].Metric != data.FPS || fps.Items[0].Unit != "" {
		t.Fatalf("first item %+v", fps.Items[0])
	}
	for _, it := range fps.Items {
		if it.Metric == data.CPUTemp {
			t.Fatal("foreign metric kept")
		}
	}
	n := 0
	for _, m := range data.Catalog {
		if m.Group == "fps" {
			n++
		}
	}
	if len(fps.Items) != n {
		t.Fatalf("missing items appended? got %d want %d", len(fps.Items), n)
	}
}

func TestLoadSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "glance.json")
	c, err := Load(p)
	if err != nil || c == nil {
		t.Fatal(err)
	}
	c.Overlay.Anchor = "br"
	c.Group("gpu").Items[0].Enabled = false
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	c2, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c2.Overlay.Anchor != "br" || c2.Group("gpu").Items[0].Enabled {
		t.Fatal("not persisted")
	}
	os.WriteFile(p, []byte("{broken"), 0o644)
	if c3, err := Load(p); err == nil || c3.Overlay.Anchor != "tl" {
		t.Fatal("broken file should yield defaults + error")
	}
}

func TestHotkeys(t *testing.T) {
	cases := map[string]bool{"Ctrl+Alt+H": true, "F10": true, "Shift+F12": true, "H": false, "Ctrl+": false, "Ctrl+Alt+Num5": true, "": true}
	for s, ok := range cases {
		_, _, err := ParseHotkey(s)
		if (err == nil) != ok {
			t.Errorf("%q: err=%v", s, err)
		}
	}
	m, vk, _ := ParseHotkey("ctrl+alt+h")
	if m != ModCtrl|ModAlt || vk != 'H' {
		t.Fatalf("m=%x vk=%x", m, vk)
	}
}

func TestParseColor(t *testing.T) {
	if ParseColor("#FF8000") != 0xFF8000 || ParseColor("bad") != 0xFFFFFF {
		t.Fatal("color parse")
	}
}

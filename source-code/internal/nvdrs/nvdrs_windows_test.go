//go:build windows

package nvdrs

import (
	"slices"
	"testing"
	"time"
	"unsafe"
)

func TestLayout(t *testing.T) {
	if got := unsafe.Sizeof(drsSetting{}); got != 12320 {
		t.Errorf("NVDRS_SETTING_V1 size %d, want 12320", got)
	}
	if got := unsafe.Sizeof(drsProfile{}); got != 4116 {
		t.Errorf("NVDRS_PROFILE_V1 size %d, want 4116", got)
	}
	if got := unsafe.Sizeof(drsApp{}); got != 16396 {
		t.Errorf("NVDRS_APPLICATION_V3 size %d, want 16396", got)
	}
}

func TestValidExe(t *testing.T) {
	for _, s := range []string{"javaw.exe", "Minecraft.Windows.EXE", "my game.exe"} {
		if !ValidExe(s) {
			t.Errorf("ValidExe(%q) = false", s)
		}
	}
	for _, s := range []string{"", ".exe", "javaw", `C:\x\javaw.exe`, "a/b.exe", "x.exe\x00", "x.dll"} {
		if ValidExe(s) {
			t.Errorf("ValidExe(%q) = true", s)
		}
	}
}

// TestApplyUnsaved runs the whole create / set / read back / remove cycle
// against the real driver, but never calls SaveSettings: the session is
// destroyed at the end, so the driver's stored settings are untouched.
func TestApplyUnsaved(t *testing.T) {
	mu.Lock()
	defer mu.Unlock()
	s, err := open()
	if err != nil {
		t.Skip("NVIDIA profile API unavailable:", err)
	}
	defer s.close()

	const exe = "glance-selftest.exe"
	global := s.globalValue()
	before, err := s.state(exe, global)
	if err != nil {
		t.Fatal(err)
	}
	if before.Profile != "" {
		t.Skipf("%s unexpectedly already has profile %q", exe, before.Profile)
	}

	start := time.Now()
	for round := 0; round < 2; round++ { // second round: profile already exists
		if err := s.apply(exe, true); err != nil {
			t.Fatalf("apply on (round %d): %v", round, err)
		}
	}
	on, err := s.state(exe, global)
	if err != nil {
		t.Fatal(err)
	}
	if on.Value != PresentDXGI || !on.Ours || on.Profile != ProfilePrefix+exe {
		t.Fatalf("after enable: %+v", on)
	}
	if ours := s.ours(); !slices.Contains(ours, exe) {
		t.Fatalf("ours() = %v, missing %s", ours, exe)
	}
	t.Logf("enable + read back + enumerate took %v", time.Since(start))

	if err := s.apply(exe, false); err != nil {
		t.Fatal("apply off:", err)
	}
	off, err := s.state(exe, global)
	if err != nil {
		t.Fatal(err)
	}
	if off.Profile != "" || off.Value != global {
		t.Fatalf("after disable: %+v (global %d)", off, global)
	}
}

// TestAliasesUnsaved checks the alias path, also without saving: an alias the
// driver does not know goes into Glance's profile, and an alias in NVIDIA's
// own "Minecraft" profile gets just the one setting, removed again on off.
func TestAliasesUnsaved(t *testing.T) {
	const exe, unknown, known = "glance-selftest.exe", "glance.selftest.alias", "net.minecraft.client.main.main"
	Aliases[exe] = []string{unknown, known}
	defer delete(Aliases, exe)

	mu.Lock()
	defer mu.Unlock()
	s, err := open()
	if err != nil {
		t.Skip("NVIDIA profile API unavailable:", err)
	}
	defer s.close()

	mc, err := s.findApp(known)
	if err != nil || mc == 0 {
		t.Skipf("driver has no profile listing %s (err %v)", known, err)
	}
	mcName := s.profileName(mc)
	mcBefore, mcSet, _ := s.value(mc)
	if mcSet && mcBefore == PresentDXGI {
		t.Skipf("profile %q already uses DXGI", mcName)
	}
	global := s.globalValue()

	if err := s.apply(exe, true); err != nil {
		t.Fatal("apply on:", err)
	}
	if st, err := s.state(exe, global); err != nil || st.Value != PresentDXGI || !st.Ours {
		t.Fatalf("after enable: %+v %v", st, err)
	}
	if p, _ := s.findApp(unknown); p == 0 || s.profileName(p) != ProfilePrefix+exe {
		t.Fatalf("unknown alias not added to Glance's profile")
	}
	if v, ok, _ := s.value(mc); !ok || v != PresentDXGI {
		t.Fatalf("profile %q value %d/%v, want DXGI", mcName, v, ok)
	}

	if err := s.apply(exe, false); err != nil {
		t.Fatal("apply off:", err)
	}
	if p, _ := s.findApp(unknown); p != 0 {
		t.Fatal("unknown alias still listed after disable")
	}
	if v, ok, _ := s.value(mc); ok != mcSet || (ok && v != mcBefore) {
		t.Fatalf("profile %q value %d/%v after disable, want %d/%v", mcName, v, ok, mcBefore, mcSet)
	}
	if st, _ := s.state(exe, global); st.Profile != "" {
		t.Fatalf("after disable: %+v", st)
	}
}

func TestQueryReadOnly(t *testing.T) {
	start := time.Now()
	list, err := Query([]string{"javaw.exe", "bad/name.exe"})
	if err != nil {
		t.Skip("NVIDIA profile API unavailable:", err)
	}
	if len(list) == 0 || list[0].Exe != "javaw.exe" {
		t.Fatalf("Query = %+v", list)
	}
	for _, st := range list {
		if st.Exe == "bad/name.exe" {
			t.Fatal("invalid name was queried")
		}
	}
	t.Logf("Query took %v: %+v", time.Since(start), list)
}

//go:build windows

package ui

import (
	"errors"
	"log"
	"os"
	"strings"

	"glance/internal/data"
	"glance/internal/nvdrs"
	"glance/internal/w32"
)

// OpenGL / Vulkan fullscreen compatibility.
//
// On NVIDIA cards a fullscreen OpenGL or Vulkan game (Minecraft Java Edition
// is the usual one) presents straight to the display and the compositor
// never draws the overlay over it, although the overlay is above it in the
// z-order. The settings page lists such games and can switch each one to
// DXGI presentation in the NVIDIA driver profile (see package nvdrs).

// minecraftExe is always offered: it is by far the most common case.
const minecraftExe = "javaw.exe"

const maxRecentFull = 6

// recentFull is a program seen in the foreground covering its monitor.
type recentFull struct {
	exe string
	api string // present path seen while fullscreen ("" = no frames seen)
}

// skipFullscreen lists shell programs that cover the screen but are not games.
var skipFullscreen = map[string]bool{
	"explorer.exe": true, "searchhost.exe": true, "startmenuexperiencehost.exe": true,
	"shellexperiencehost.exe": true, "lockapp.exe": true, "textinputhost.exe": true,
	"applicationframehost.exe": true,
}

// noteFullscreen remembers the foreground program while it is fullscreen.
// Runs on the UI thread once per snapshot.
func (a *App) noteFullscreen(snap *data.Snapshot) {
	fg := w32.GetForegroundWindow()
	if fg == 0 || fg == a.overlay.hwnd || fg == a.card.hwnd || fg == a.hwnd || !isFullscreen(fg) {
		return
	}
	pid := w32.WindowPID(fg)
	if pid == 0 || pid == uint32(os.Getpid()) {
		return
	}
	if pid != a.fullPID {
		a.fullPID = pid
		a.fullExe = strings.ToLower(w32.ProcessNames()[pid])
	}
	exe := a.fullExe
	if exe == "" || skipFullscreen[exe] {
		return
	}
	api := ""
	if snap != nil && snap.FPSActive && snap.FPSPid == pid {
		api = snap.FPSAPI
	}
	for i, r := range a.recentFull {
		if r.exe == exe {
			if api == "" {
				api = r.api
			}
			a.recentFull = append(a.recentFull[:i], a.recentFull[i+1:]...)
			break
		}
	}
	a.recentFull = append([]recentFull{{exe: exe, api: api}}, a.recentFull...)
	if len(a.recentFull) > maxRecentFull {
		a.recentFull = a.recentFull[:maxRecentFull]
	}
}

// glApp is one row of the settings page list.
type glApp struct {
	nvdrs.State
	API     string `json:"api"`     // present path Glance saw while it was fullscreen
	Seen    bool   `json:"seen"`    // ran fullscreen since Glance started
	Running bool   `json:"running"` // currently running (needs a restart to pick up a change)
}

// glCompat serves the "gl_compat" action: arg "" lists, "on:x.exe" /
// "off:x.exe" changes one program. Runs on a web goroutine.
func (a *App) glCompat(arg string) (any, error) {
	var recent []recentFull
	a.runOnUI(func() { recent = append(recent, a.recentFull...) })

	if arg != "" {
		mode, exe, _ := strings.Cut(arg, ":")
		exe = strings.ToLower(exe)
		on := mode == "on"
		if (mode != "on" && mode != "off") || !nvdrs.ValidExe(exe) {
			return nil, errors.New("参数无效")
		}
		if on && exe != minecraftExe && !containsExe(recent, exe) {
			return nil, errors.New("只能为检测到的全屏程序开启")
		}
		if _, err := nvdrs.Set(exe, on); err != nil {
			log.Printf("NVIDIA present method for %s (dxgi=%v) failed: %v", exe, on, err)
			return nil, err
		}
		log.Printf("NVIDIA present method for %s: dxgi=%v", exe, on)
	}

	exes := make([]string, 0, len(recent)+1)
	for _, r := range recent {
		exes = append(exes, r.exe)
	}
	exes = append(exes, minecraftExe)
	states, err := nvdrs.Query(exes)
	if err != nil {
		return map[string]any{"available": false, "error": err.Error(), "apps": []glApp{}}, nil
	}
	running := map[string]bool{}
	for _, n := range w32.ProcessNames() {
		running[strings.ToLower(n)] = true
	}
	apps := []glApp{}
	for _, st := range states {
		g := glApp{State: st, Running: running[st.Exe]}
		for _, r := range recent {
			if r.exe == st.Exe {
				g.Seen, g.API = true, r.api
			}
		}
		// DirectX games already get the overlay; only list the rest, plus
		// anything already switched so it can be switched back.
		directX := g.API == "DXGI" || g.API == "D3D9"
		if (g.Seen && !directX) || st.Value == nvdrs.PresentDXGI || st.Ours || st.Exe == minecraftExe {
			apps = append(apps, g)
		}
	}
	return map[string]any{"available": true, "error": "", "apps": apps}, nil
}

func containsExe(list []recentFull, exe string) bool {
	for _, r := range list {
		if r.exe == exe {
			return true
		}
	}
	return false
}

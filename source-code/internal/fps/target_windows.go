//go:build windows

package fps

import (
	"strings"
	"time"

	"glance/internal/w32"
)

// Windows shell processes whose presents are never "the game".
var shellNames = map[string]bool{
	"dwm.exe": true, "explorer.exe": true, "shellexperiencehost.exe": true, "startmenuexperiencehost.exe": true,
	"searchhost.exe": true, "searchapp.exe": true, "textinputhost.exe": true, "lockapp.exe": true,
	"applicationframehost.exe": true, "csrss.exe": true, "shellhost.exe": true,
}

// Target decides which process the overlay reports FPS for: the
// foreground app when it renders, otherwise the last game while it keeps
// rendering (so clicking a second monitor does not lose the counter).
type Target struct {
	tr      *Tracker
	self    uint32
	names   map[uint32]string
	namesAt time.Time
	last    uint32
	exclude map[string]bool
}

func NewTarget(tr *Tracker, self uint32) *Target {
	return &Target{tr: tr, self: self, names: map[uint32]string{}, exclude: map[string]bool{}}
}

// SetExclude replaces the user's list of processes never reported.
func (t *Target) SetExclude(names []string) {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		if n = strings.ToLower(strings.TrimSpace(n)); n != "" {
			m[n] = true
		}
	}
	t.exclude = m
}

// Name returns the exe name for a pid (cached toolhelp snapshot).
func (t *Target) Name(pid uint32) string {
	if n, ok := t.names[pid]; ok {
		return n
	}
	if time.Since(t.namesAt) > 2*time.Second {
		t.names = w32.ProcessNames()
		t.namesAt = time.Now()
	}
	return t.names[pid]
}

func (t *Target) eligible(pid uint32) bool {
	if pid == 0 || pid == t.self {
		return false
	}
	n := strings.ToLower(t.Name(pid))
	// A process can appear between Toolhelp snapshots (notably the settings
	// browser during restart). Wait until its name is known before targeting it.
	return n != "" && !shellNames[n] && !t.exclude[n]
}

// qualifies reports whether pid renders enough to be treated as a game.
// The current target keeps its slot as long as it renders at all.
func (t *Target) qualifies(pid uint32, now int64) bool {
	rate, src, ok := t.tr.PresentRate(pid, now, 2.5)
	if !ok {
		return false
	}
	if pid == t.last {
		return true
	}
	if src == SrcKernel {
		return rate >= 20
	}
	return rate >= 10
}

// Resolve returns the pid to report (0 = none).
func (t *Target) Resolve(now int64) uint32 {
	fg := w32.GetForegroundWindow()
	pid := w32.WindowPID(fg)
	if t.eligible(pid) && t.qualifies(pid, now) {
		t.last = pid
		return pid
	}
	// UWP / hosted windows: the renderer lives in a child window's process.
	if pid != 0 {
		for _, ch := range w32.ChildWindows(fg) {
			cp := w32.WindowPID(ch)
			if cp != pid && t.eligible(cp) && t.qualifies(cp, now) {
				t.last = cp
				return cp
			}
		}
	}
	if t.last != 0 {
		if t.eligible(t.last) && t.qualifies(t.last, now) {
			return t.last
		}
		t.last = 0
	}
	return 0
}

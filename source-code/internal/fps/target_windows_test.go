//go:build windows

package fps

import (
	"testing"
	"time"
)

func TestEligibleRequiresKnownNonExcludedProcess(t *testing.T) {
	target := NewTarget(nil, 1)
	target.namesAt = time.Now()
	target.names = map[uint32]string{2: "ChatGPT.exe", 3: "Cyberpunk2077.exe", 4: "explorer.exe"}
	target.SetExclude([]string{"chatgpt.exe"})
	for _, pid := range []uint32{0, 1, 2, 4, 5} {
		if target.eligible(pid) {
			t.Errorf("pid %d should not qualify", pid)
		}
	}
	if !target.eligible(3) {
		t.Fatal("known game rejected")
	}
	target.names[5] = "Game.exe"
	if !target.eligible(5) {
		t.Fatal("new game rejected after name resolved")
	}
}

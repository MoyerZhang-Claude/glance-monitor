package config

import (
	"errors"
	"strings"
)

// Modifier flags (match Win32 MOD_* values).
const (
	ModAlt   = 0x1
	ModCtrl  = 0x2
	ModShift = 0x4
	ModWin   = 0x8
)

var keyNames = func() map[string]uint32 {
	m := map[string]uint32{
		"SPACE": 0x20, "PAGEUP": 0x21, "PAGEDOWN": 0x22, "END": 0x23, "HOME": 0x24,
		"LEFT": 0x25, "UP": 0x26, "RIGHT": 0x27, "DOWN": 0x28,
		"INSERT": 0x2D, "DELETE": 0x2E, "PAUSE": 0x13, "SCROLLLOCK": 0x91,
		"`": 0xC0, "-": 0xBD, "=": 0xBB, "[": 0xDB, "]": 0xDD, ";": 0xBA, "'": 0xDE,
		",": 0xBC, ".": 0xBE, "/": 0xBF, "\\": 0xDC,
	}
	for c := 'A'; c <= 'Z'; c++ {
		m[string(c)] = uint32(c)
	}
	for c := '0'; c <= '9'; c++ {
		m[string(c)] = uint32(c)
		m["NUM"+string(c)] = 0x60 + uint32(c-'0')
	}
	for i := 1; i <= 24; i++ {
		m["F"+itoa(i)] = 0x70 + uint32(i-1)
	}
	return m
}()

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}

// ParseHotkey parses strings like "Ctrl+Alt+H". An empty string disables
// the hotkey (returns 0, 0, nil).
func ParseHotkey(s string) (mods, vk uint32, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, 0, nil
	}
	parts := strings.Split(s, "+")
	for i, p := range parts {
		p = strings.ToUpper(strings.TrimSpace(p))
		last := i == len(parts)-1
		switch {
		case !last && (p == "CTRL" || p == "CONTROL"):
			mods |= ModCtrl
		case !last && p == "ALT":
			mods |= ModAlt
		case !last && p == "SHIFT":
			mods |= ModShift
		case !last && p == "WIN":
			mods |= ModWin
		case last:
			v, ok := keyNames[p]
			if !ok {
				return 0, 0, errors.New("未知按键: " + p)
			}
			vk = v
		default:
			return 0, 0, errors.New("无效的组合键: " + s)
		}
	}
	if vk == 0 {
		return 0, 0, errors.New("缺少按键")
	}
	isF := vk >= 0x70 && vk <= 0x87
	if mods == 0 && !isF {
		return 0, 0, errors.New("需要至少一个修饰键 (Ctrl/Alt/Shift/Win)")
	}
	return mods, vk, nil
}

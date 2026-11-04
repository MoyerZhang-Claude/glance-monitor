//go:build windows

// Package nvdrs reads and changes one NVIDIA driver profile setting,
// "Vulkan/OpenGL present method", per program.
//
// On NVIDIA cards an OpenGL or Vulkan game in fullscreen (Minecraft Java
// Edition is the common case) presents straight to the display by default,
// bypassing the desktop compositor, so no other window - the overlay
// included - can appear on top of it, even when it is higher in the z-order.
// Presenting through a DXGI swap chain instead lets Windows compose topmost
// windows over the game the same way it does for DirectX games.
//
// Only the driver's own profile API is used; nothing touches the game.
package nvdrs

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"glance/internal/w32"
)

// Values of the "Vulkan/OpenGL present method" setting.
const (
	PresentNative = 0 // "Prefer native"
	PresentDXGI   = 1 // "Prefer layered on DXGI swapchain"
	PresentAuto   = 2 // "Auto", the driver default
)

const (
	settingPresent = 0x20D690F8 // OGL_CPL_PREFER_DXPRESENT

	// ProfilePrefix names the profiles Glance creates, so they can be found
	// and removed again.
	ProfilePrefix = "Glance - "
)

// NVAPI status codes handled here.
const (
	nvOK              = 0
	nvEndEnumeration  = -7
	nvNoPrivilege     = -137
	nvSettingNotFound = -160
	nvProfileNotFound = -163
	nvExeNotFound     = -166
)

// ErrNoDriver means no NVIDIA driver with the profile API is installed.
var ErrNoDriver = errors.New("没有找到 NVIDIA 显卡驱动")

// State describes one program's setting.
type State struct {
	Exe     string `json:"exe"`
	Value   int    `json:"value"`             // effective value (PresentNative / PresentDXGI / PresentAuto)
	Profile string `json:"profile,omitempty"` // driver profile that lists the program ("" = none)
	Ours    bool   `json:"ours"`              // that profile was created by Glance
}

type uniStr [2048]uint16

type drsSetting struct { // NVDRS_SETTING_V1
	Version             uint32
	Name                uniStr
	ID                  uint32
	Type                uint32
	Location            uint32
	IsCurrentPredefined uint32
	IsPredefinedValid   uint32
	Predefined          [4100]byte
	Current             [4100]byte
}

type drsProfile struct { // NVDRS_PROFILE_V1
	Version      uint32
	Name         uniStr
	GPUSupport   uint32
	IsPredefined uint32
	NumApps      uint32
	NumSettings  uint32
}

type drsApp struct { // NVDRS_APPLICATION_V3
	Version      uint32
	IsPredefined uint32
	AppName      uniStr
	FriendlyName uniStr
	Launcher     uniStr
	FileInFolder uniStr
	Flags        uint32
}

// The layouts above must match nvapi.h exactly (the driver checks the size
// encoded in each version field); TestLayout pins them.

type api struct {
	createSession, destroySession, loadSettings, saveSettings uintptr
	findApp, findProfile, createProfile, deleteProfile        uintptr
	createApp, profileInfo, enumProfiles, globalProfile       uintptr
	getSetting, setSetting, deleteSetting                     uintptr
}

var (
	loadOnce sync.Once
	fns      *api
	loadErr  error

	// mu serialises sessions: the driver keeps one settings database.
	mu sync.Mutex
)

func load() (*api, error) {
	loadOnce.Do(func() {
		dll, err := w32.LoadDLL("nvapi64.dll", false)
		if err != nil {
			loadErr = ErrNoDriver
			return
		}
		qi, err := dll.FindProc("nvapi_QueryInterface")
		if err != nil {
			loadErr = ErrNoDriver
			return
		}
		q := func(id uint32) uintptr { r, _, _ := qi.Call(uintptr(id)); return r }
		a := &api{
			createSession:  q(0x0694D52E),
			destroySession: q(0xDAD9CFF8),
			loadSettings:   q(0x375DBD6B),
			saveSettings:   q(0xFCBC7E14),
			findApp:        q(0xEEE566B2),
			findProfile:    q(0x7E4A9A0B),
			createProfile:  q(0xCC176068),
			deleteProfile:  q(0x17093206),
			createApp:      q(0x4347A9DE),
			profileInfo:    q(0x61CD6FD6),
			enumProfiles:   q(0xBC371EE0),
			globalProfile:  q(0x617BFF9F),
			getSetting:     q(0x73BF8338),
			setSetting:     q(0x577DD202),
			deleteSetting:  q(0xE4A26362),
		}
		initFn := q(0x0150E828)
		for _, p := range []uintptr{initFn, a.createSession, a.destroySession, a.loadSettings, a.saveSettings,
			a.findApp, a.findProfile, a.createProfile, a.deleteProfile, a.createApp, a.profileInfo,
			a.enumProfiles, a.globalProfile, a.getSetting, a.setSetting, a.deleteSetting} {
			if p == 0 {
				loadErr = errors.New("这个 NVIDIA 驱动版本不支持修改程序设置")
				return
			}
		}
		if r := call(initFn); r != nvOK {
			loadErr = ErrNoDriver
			return
		}
		fns = a
	})
	return fns, loadErr
}

func call(fn uintptr, args ...uintptr) int32 {
	r, _, _ := syscall.SyscallN(fn, args...)
	return int32(r)
}

func nvErr(op string, r int32) error {
	if r == nvNoPrivilege {
		return errors.New("需要管理员权限才能修改 NVIDIA 驱动设置")
	}
	return fmt.Errorf("NVIDIA 驱动接口 %s 返回错误 %d", op, r)
}

func toUni(s string) (*uniStr, error) {
	u, err := syscall.UTF16FromString(s)
	if err != nil || len(u) > len(uniStr{}) {
		return nil, errors.New("名称无效")
	}
	b := new(uniStr)
	copy(b[:], u)
	return b, nil
}

func fromUni(b *uniStr) string { return syscall.UTF16ToString(b[:]) }

func u32(b *[4100]byte) uint32 { return *(*uint32)(unsafe.Pointer(&b[0])) }

// ValidExe reports whether s is a bare program file name such as "javaw.exe".
func ValidExe(s string) bool {
	if len(s) < 5 || len(s) > 200 || !strings.HasSuffix(strings.ToLower(s), ".exe") {
		return false
	}
	return !strings.ContainsAny(s, `\/:*?"<>|`) && !strings.ContainsRune(s, 0)
}

type session struct {
	a *api
	h uintptr
}

func open() (*session, error) {
	a, err := load()
	if err != nil {
		return nil, err
	}
	s := &session{a: a}
	if r := call(a.createSession, uintptr(unsafe.Pointer(&s.h))); r != nvOK {
		return nil, nvErr("CreateSession", r)
	}
	if r := call(a.loadSettings, s.h); r != nvOK {
		s.close()
		return nil, nvErr("LoadSettings", r)
	}
	return s, nil
}

// close discards anything not saved with save.
func (s *session) close() { call(s.a.destroySession, s.h) }

func (s *session) save() error {
	if r := call(s.a.saveSettings, s.h); r != nvOK {
		return nvErr("SaveSettings", r)
	}
	return nil
}

// findApp returns the profile listing exe, or 0 when there is none.
func (s *session) findApp(exe string) (uintptr, error) {
	name, err := toUni(exe)
	if err != nil {
		return 0, err
	}
	app := new(drsApp)
	app.Version = uint32(unsafe.Sizeof(*app)) | 3<<16
	var prof uintptr
	r := call(s.a.findApp, s.h, uintptr(unsafe.Pointer(&name[0])), uintptr(unsafe.Pointer(&prof)), uintptr(unsafe.Pointer(app)))
	switch r {
	case nvOK:
		return prof, nil
	case nvExeNotFound:
		return 0, nil
	}
	return 0, nvErr("FindApplicationByName", r)
}

func (s *session) profileName(prof uintptr) string { return s.profileNameInto(prof, new(drsProfile)) }

// profileNameInto reuses p, so enumerating thousands of profiles does not
// allocate a 4 KiB buffer for each one.
func (s *session) profileNameInto(prof uintptr, p *drsProfile) string {
	*p = drsProfile{Version: uint32(unsafe.Sizeof(*p)) | 1<<16}
	if call(s.a.profileInfo, s.h, prof, uintptr(unsafe.Pointer(p))) != nvOK {
		return ""
	}
	return fromUni(&p.Name)
}

// value reads the setting stored in prof (ok=false when it holds none).
func (s *session) value(prof uintptr) (v int, ok bool, err error) {
	st := new(drsSetting)
	st.Version = uint32(unsafe.Sizeof(*st)) | 1<<16
	r := call(s.a.getSetting, s.h, prof, settingPresent, uintptr(unsafe.Pointer(st)))
	switch r {
	case nvOK:
		return int(u32(&st.Current)), true, nil
	case nvSettingNotFound:
		return 0, false, nil
	}
	return 0, false, nvErr("GetSetting", r)
}

func (s *session) globalValue() int {
	var g uintptr
	if call(s.a.globalProfile, s.h, uintptr(unsafe.Pointer(&g))) == nvOK {
		if v, ok, _ := s.value(g); ok {
			return v
		}
	}
	return PresentAuto
}

// Aliases lists other names the driver may know a program by.
//
// Java programs are also matched by the main class on their command line.
// Sodium, a popular Minecraft mod, rewrites the command line while the
// OpenGL driver starts ("net.caffeinemc.sodium / net.minecraft.client.main.Main /"),
// so the driver no longer matches javaw.exe and uses NVIDIA's own
// "Minecraft" profile (app "net.minecraft.client.main.main") instead.
// Verified on driver 621.01 with a test program that rewrites its command
// line the same way.
var Aliases = map[string][]string{
	"javaw.exe": {"net.minecraft.client.main.main"},
}

// names returns exe followed by its aliases.
func names(exe string) []string { return append([]string{exe}, Aliases[exe]...) }

// state reports exe's value; it only counts as DXGI when every alias the
// driver knows is switched too.
func (s *session) state(exe string, global int) (State, error) {
	exe = strings.ToLower(exe)
	st := State{Exe: exe, Value: global}
	for i, name := range names(exe) {
		prof, err := s.findApp(name)
		if err != nil {
			return st, err
		}
		if prof == 0 && i > 0 {
			continue // alias unknown to this driver: nothing can match it
		}
		v := global
		if prof != 0 {
			if i == 0 {
				st.Profile = s.profileName(prof)
				st.Ours = st.Profile == ProfilePrefix+exe
			}
			pv, ok, err := s.value(prof)
			if err != nil {
				return st, err
			}
			if ok {
				v = pv
			}
		}
		if i == 0 || (st.Value == PresentDXGI && v != PresentDXGI) {
			st.Value = v
		}
	}
	return st, nil
}

// ours lists the programs that have a Glance-created profile.
func (s *session) ours() []string {
	var out []string
	buf := new(drsProfile)
	for i := uint32(0); i < 100000; i++ {
		var prof uintptr
		if call(s.a.enumProfiles, s.h, uintptr(i), uintptr(unsafe.Pointer(&prof))) != nvOK {
			break // nvEndEnumeration, or an error: either way stop
		}
		if name := s.profileNameInto(prof, buf); strings.HasPrefix(name, ProfilePrefix) {
			if exe := strings.TrimPrefix(name, ProfilePrefix); ValidExe(exe) {
				out = append(out, exe)
			}
		}
	}
	return out
}

// apply changes the in-session settings for exe and its aliases; the
// caller saves. Names no profile lists yet go into Glance's own profile;
// names in someone else's profile (NVIDIA's or the user's) get only this one
// setting changed, and switching off removes just that setting again.
func (s *session) apply(exe string, on bool) error {
	exe = strings.ToLower(exe)
	var ours uintptr // Glance's profile for exe, found or created on demand
	for _, name := range names(exe) {
		prof, err := s.findApp(name)
		if err != nil {
			return err
		}
		mine := prof != 0 && s.profileName(prof) == ProfilePrefix+exe
		if mine {
			ours = prof
		}
		if !on {
			if prof != 0 && !mine {
				if r := call(s.a.deleteSetting, s.h, prof, settingPresent); r != nvOK && r != nvSettingNotFound {
					return nvErr("DeleteProfileSetting", r)
				}
			}
			continue
		}
		if prof == 0 {
			if ours == 0 {
				if ours, err = s.ourProfile(exe); err != nil {
					return err
				}
			}
			if err := s.addApp(ours, name); err != nil {
				return err
			}
			prof = ours
		}
		st := new(drsSetting)
		st.Version = uint32(unsafe.Sizeof(*st)) | 1<<16
		st.ID = settingPresent
		*(*uint32)(unsafe.Pointer(&st.Current[0])) = PresentDXGI
		if r := call(s.a.setSetting, s.h, prof, uintptr(unsafe.Pointer(st))); r != nvOK {
			return nvErr("SetSetting", r)
		}
	}
	if !on && ours != 0 {
		// Deleting the profile also drops every name listed in it.
		if r := call(s.a.deleteProfile, s.h, ours); r != nvOK {
			return nvErr("DeleteProfile", r)
		}
	}
	return nil
}

func (s *session) addApp(prof uintptr, name string) error {
	n, err := toUni(name)
	if err != nil {
		return err
	}
	app := new(drsApp)
	app.Version = uint32(unsafe.Sizeof(*app)) | 3<<16
	app.AppName = *n
	if r := call(s.a.createApp, s.h, prof, uintptr(unsafe.Pointer(app))); r != nvOK {
		return nvErr("CreateApplication", r)
	}
	return nil
}

// ourProfile finds or creates the "Glance - exe" profile.
func (s *session) ourProfile(exe string) (uintptr, error) {
	name, err := toUni(ProfilePrefix + exe)
	if err != nil {
		return 0, err
	}
	var prof uintptr
	r := call(s.a.findProfile, s.h, uintptr(unsafe.Pointer(&name[0])), uintptr(unsafe.Pointer(&prof)))
	if r == nvProfileNotFound {
		p := new(drsProfile)
		p.Version = uint32(unsafe.Sizeof(*p)) | 1<<16
		p.Name = *name
		if r = call(s.a.createProfile, s.h, uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&prof))); r != nvOK {
			return 0, nvErr("CreateProfile", r)
		}
	} else if r != nvOK {
		return 0, nvErr("FindProfileByName", r)
	}
	return prof, nil
}

// Query returns the setting for each program in exes, followed by any other
// program Glance has changed before.
func Query(exes []string) ([]State, error) {
	mu.Lock()
	defer mu.Unlock()
	s, err := open()
	if err != nil {
		return nil, err
	}
	defer s.close()
	seen := map[string]bool{}
	var list []string
	for _, e := range append(append([]string{}, exes...), s.ours()...) {
		e = strings.ToLower(e)
		if ValidExe(e) && !seen[e] {
			seen[e] = true
			list = append(list, e)
		}
	}
	global := s.globalValue()
	out := make([]State, 0, len(list))
	for _, e := range list {
		st, err := s.state(e, global)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}

// Set switches exe to DXGI presentation (on) or back to the driver default
// (off) and saves the driver settings. The game must be restarted to pick
// up the change.
func Set(exe string, on bool) (State, error) {
	if !ValidExe(exe) {
		return State{}, errors.New("程序名无效")
	}
	mu.Lock()
	defer mu.Unlock()
	s, err := open()
	if err != nil {
		return State{}, err
	}
	defer s.close()
	if err := s.apply(exe, on); err != nil {
		return State{}, err
	}
	if err := s.save(); err != nil {
		return State{}, err
	}
	return s.state(strings.ToLower(exe), s.globalValue())
}

//go:build windows

// Package ui is the Windows front end: hidden main window, overlay, tray
// icon, hover card, hotkeys, menus and the bridge to the settings page.
package ui

import (
	"errors"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"glance/internal/config"
	"glance/internal/data"
	"glance/internal/gdip"
	"glance/internal/layout"
	"glance/internal/sensors"
	"glance/internal/sys"
	"glance/internal/w32"
	"glance/internal/web"
)

const (
	Version   = "1.0.0-b10"
	MainClass = "GlanceHUD.Main"

	wmSnapshot     = w32.WM_APP + 2
	wmRun          = w32.WM_APP + 3
	WMShowSettings = w32.WM_APP + 4

	timerSave = 1

	hkToggle = 1
	hkEdit   = 2

	settingsTitle = "Glance 设置"
)

// Options passed from main.
type Options struct {
	ConfigPath  string
	LogPath     string
	FirstRun    bool
	OpenSettings bool
	ConfigError string
	Restart     func(elevated bool) bool // relaunch helper; returns true if we should quit
	ApplyUpdate func() error             // swap in Glance.new.exe and start it
}

type App struct {
	opts    Options
	inst    w32.HANDLE
	hwnd    w32.HWND
	cfgMu   sync.RWMutex
	cfg     *config.Config
	mgr     *sensors.Manager
	overlay *overlayWin
	card    *cardWin
	tray    *trayIcon

	srvMu sync.Mutex
	srv   *web.Server

	light          bool
	menuOpen       bool
	editing        bool
	hotkeyErr      map[string]string
	taskbarCreated uint32
	fgHook         w32.HANDLE
	lastFullscreen bool
	lastOpts       sensors.Options
	autostart      atomic.Bool
	pendingSnap    atomic.Bool
	lastSettings   time.Time
	quitting       bool

	qmu   sync.Mutex
	queue []func()

	fonts     []string
	fontsOnce sync.Once
}

var theApp *App

var mainProc = syscall.NewCallback(func(h, msg, w, l uintptr) uintptr {
	if a := theApp; a != nil && (a.hwnd == h || a.hwnd == 0) {
		if r, ok := a.wndProc(h, uint32(msg), w, l); ok {
			return r
		}
	}
	return w32.DefWindowProc(h, uint32(msg), w, l)
})

var winEventProc = syscall.NewCallback(func(hook, event, hwnd, idObject, idChild, thread, ts uintptr) uintptr {
	if a := theApp; a != nil && uint32(event) == w32.EVENT_SYSTEM_FOREGROUND {
		a.onForeground(hwnd)
	}
	return 0
})

// Run creates the UI and blocks in the message loop until exit.
func Run(cfg *config.Config, opts Options) error {
	a := &App{opts: opts, cfg: cfg, hotkeyErr: map[string]string{}}
	theApp = a
	a.inst = w32.GetModuleHandle()
	if !gdip.Startup() {
		return errors.New("GDI+ 初始化失败")
	}
	defer gdip.Shutdown()

	wc := w32.WNDCLASSEXW{WndProc: mainProc, Instance: a.inst, ClassName: w32.Ptr(MainClass),
		Icon: w32.LoadIconRes(a.inst, 1, 32, 32), IconSm: w32.LoadIconRes(a.inst, 1, 16, 16)}
	w32.RegisterClass(&wc)
	a.hwnd = w32.CreateWindow(w32.WS_EX_TOOLWINDOW, MainClass, "Glance", w32.WS_POPUP, 0, 0, 0, 0, 0, a.inst)
	if a.hwnd == 0 {
		return errors.New("无法创建主窗口")
	}
	a.taskbarCreated = w32.RegisterWindowMessage("TaskbarCreated")
	// Let explorer and a second (unelevated) instance reach our elevated window.
	for _, m := range []uint32{a.taskbarCreated, wmTray, WMShowSettings, w32.WM_COMMAND} {
		w32.AllowMessage(a.hwnd, m)
	}

	a.light = lightTaskbar()
	a.overlay = newOverlay(a)
	a.overlay.setCaptureExcluded(cfg.Overlay.HideCapture)
	a.card = newCard(a)
	a.tray = newTray(a)
	a.registerHotkeys()
	a.fgHook = w32.SetWinEventHook(w32.EVENT_SYSTEM_FOREGROUND, w32.EVENT_SYSTEM_FOREGROUND, winEventProc, w32.WINEVENT_OUTOFCONTEXT)

	go func() { a.autostart.Store(sys.AutostartEnabled()) }()

	a.lastOpts = a.sensorOptions()
	a.mgr = sensors.NewManager(a.lastOpts, func() {
		if !a.pendingSnap.Swap(true) {
			w32.PostMessage(a.hwnd, wmSnapshot, 0, 0)
		}
	})
	a.mgr.Start()
	a.tray.add()
	a.onSnapshot()
	log.Printf("Glance %s started (elevated=%v, config=%s)", Version, w32.IsElevated(), opts.ConfigPath)

	if opts.FirstRun {
		a.openSettings("welcome")
	} else if opts.OpenSettings {
		a.openSettings("")
	}

	var msg w32.MSG
	for w32.GetMessage(&msg) > 0 {
		w32.TranslateMessage(&msg)
		w32.DispatchMessage(&msg)
	}
	a.cleanup()
	return nil
}

func (a *App) cleanup() {
	w32.KillTimer(a.hwnd, timerSave)
	a.saveNow()
	if a.fgHook != 0 {
		w32.UnhookWinEvent(a.fgHook)
	}
	a.unregisterHotkeys()
	a.tray.remove()
	a.card.hide()
	a.overlay.hide()
	a.mgr.Stop()
	a.srvMu.Lock()
	if a.srv != nil {
		a.srv.Close()
	}
	a.srvMu.Unlock()
	log.Printf("Glance exited")
}

// Quit ends the message loop.
func (a *App) Quit() {
	if a.quitting {
		return
	}
	a.quitting = true
	w32.PostQuitMessage(0)
}

// ---------------- config ----------------

func (a *App) config() *config.Config {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return a.cfg
}

func (a *App) sensorOptions() sensors.Options {
	c := a.cfg
	settings := a.settingsActive()
	cardOn := a.card != nil && a.card.shown
	trayM := c.Tray.IconMetric
	graph := 0.0
	if c.Overlay.Graph {
		graph = float64(c.Overlay.GraphSecs)
	}
	return sensors.Options{
		IntervalMs:   c.General.IntervalMs,
		ClockMode:    c.General.CPUClockMode,
		WakeGPU:      c.General.WakeGPU,
		PerCoreTemps: c.Enabled(data.CPUTempMax) || cardOn || settings || trayM == data.CPUTempMax,
		GPUThermals: c.Enabled(data.GPUHotspot) || c.Enabled(data.GPUMemTemp) || cardOn || settings ||
			trayM == data.GPUHotspot || trayM == data.GPUMemTemp,
		LowWindowSec: float64(c.General.LowWindowSec),
		LowMethod:    c.General.LowMethod,
		GraphSec:     graph,
		FPSCapture:   c.General.FPSCapture,
		FPSExclude:   strings.Join(c.General.FPSExclude, "|"),
	}
}

func (a *App) syncSensorOptions() {
	o := a.sensorOptions()
	if o != a.lastOpts {
		a.lastOpts = o
		a.mgr.SetOptions(o)
	}
}

func (a *App) applyConfig(n *config.Config) {
	n.Normalize()
	old := a.cfg
	a.cfgMu.Lock()
	a.cfg = n
	a.cfgMu.Unlock()
	if old.General.HotkeyToggle != n.General.HotkeyToggle || old.General.HotkeyEdit != n.General.HotkeyEdit {
		a.registerHotkeys()
	}
	a.overlay.setCaptureExcluded(n.Overlay.HideCapture)
	a.syncSensorOptions()
	a.render()
	a.scheduleSave()
}

func (a *App) scheduleSave() { w32.SetTimer(a.hwnd, timerSave, 600) }

func (a *App) saveNow() {
	if err := config.Save(a.opts.ConfigPath, a.cfg); err != nil {
		log.Printf("save config: %v", err)
	}
}

// runOnUI executes fn on the UI thread and waits (bounded) for it.
func (a *App) runOnUI(fn func()) {
	done := make(chan struct{})
	a.qmu.Lock()
	a.queue = append(a.queue, func() { fn(); close(done) })
	a.qmu.Unlock()
	w32.PostMessage(a.hwnd, wmRun, 0, 0)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
	}
}

func (a *App) drainQueue() {
	a.qmu.Lock()
	q := a.queue
	a.queue = nil
	a.qmu.Unlock()
	for _, fn := range q {
		fn()
	}
}

// ---------------- rendering ----------------

func (a *App) onSnapshot() {
	a.pendingSnap.Store(false)
	a.syncSensorOptions()
	a.render()
}

func (a *App) render() {
	snap := a.mgr.Latest()
	a.overlay.update(a.cfg, snap, a.overlayVisible())
	a.tray.update(a.cfg, snap, a.light)
	if a.card.shown {
		a.card.update(a.cfg, snap, a.light)
	}
}

func (a *App) overlayVisible() bool {
	if !a.cfg.Overlay.Visible {
		return false
	}
	if a.cfg.Overlay.ShowMode == "fullscreen" {
		fg := w32.GetForegroundWindow()
		if fg != a.overlay.hwnd && fg != a.card.hwnd && fg != a.hwnd {
			a.lastFullscreen = isFullscreen(fg)
		}
		return a.lastFullscreen
	}
	return true
}

func (a *App) onForeground(h w32.HWND) {
	if a.card.shown && !a.menuOpen {
		a.hideCard()
	}
	if a.cfg.Overlay.ShowMode == "fullscreen" {
		a.render()
	} else {
		a.overlay.bringToTop()
	}
}

// ---------------- card ----------------

func (a *App) showCard(iconRect w32.RECT) {
	a.card.showAt(iconRect, a.cfg, a.mgr.Latest(), a.light)
	a.tray.startPolling()
	a.syncSensorOptions()
}

func (a *App) hideCard() {
	a.card.hide()
	a.syncSensorOptions()
}

// ---------------- edit mode / position ----------------

func (a *App) setEditMode(on bool) {
	a.editing = on
	a.overlay.setEditing(on)
	a.render()
}

func (a *App) setOverlayPosition(anchor string, ox, oy int, device string) {
	a.cfgMu.Lock()
	a.cfg.Overlay.Anchor = anchor
	a.cfg.Overlay.OffsetX = ox
	a.cfg.Overlay.OffsetY = oy
	a.cfg.Overlay.Monitor = device
	a.cfgMu.Unlock()
	a.render()
	a.scheduleSave()
}

func (a *App) resetPosition() {
	d := config.Default().Overlay
	a.setOverlayPosition(d.Anchor, d.OffsetX, d.OffsetY, "")
}

func (a *App) toggleOverlay() {
	a.cfgMu.Lock()
	a.cfg.Overlay.Visible = !a.cfg.Overlay.Visible
	a.cfgMu.Unlock()
	if !a.cfg.Overlay.Visible && a.editing {
		a.setEditMode(false)
	}
	a.render()
	a.scheduleSave()
}

// ---------------- hotkeys ----------------

func (a *App) unregisterHotkeys() {
	w32.UnregisterHotKey(a.hwnd, hkToggle)
	w32.UnregisterHotKey(a.hwnd, hkEdit)
}

func (a *App) registerHotkeys() {
	a.unregisterHotkeys()
	a.hotkeyErr = map[string]string{}
	reg := func(id int32, name, s string) {
		mods, vk, err := config.ParseHotkey(s)
		if err != nil {
			a.hotkeyErr[name] = err.Error()
			return
		}
		if vk == 0 {
			return
		}
		if !w32.RegisterHotKey(a.hwnd, id, mods|w32.MOD_NOREPEAT, vk) {
			a.hotkeyErr[name] = "快捷键已被其他程序占用"
			log.Printf("hotkey %s (%s) unavailable", name, s)
		}
	}
	reg(hkToggle, "toggle", a.cfg.General.HotkeyToggle)
	reg(hkEdit, "edit", a.cfg.General.HotkeyEdit)
}

// ---------------- settings window ----------------

func (a *App) settingsActive() bool {
	a.srvMu.Lock()
	defer a.srvMu.Unlock()
	return a.srv != nil && a.srv.Active(4*time.Second)
}

func (a *App) openSettings(section string) {
	if time.Since(a.lastSettings) < time.Second {
		return
	}
	a.lastSettings = time.Now()
	a.srvMu.Lock()
	fresh := a.srv == nil
	a.srvMu.Unlock()
	if h := w32.FindWindowTitle(settingsTitle); h != 0 {
		if !fresh {
			w32.Restore(h)
			return
		}
		// A window left over from a previous Glance process talks to a
		// server that no longer exists: close it and open a fresh one.
		w32.PostMessage(h, w32.WM_CLOSE, 0, 0)
	}
	a.srvMu.Lock()
	if a.srv == nil {
		s, err := web.Start(a)
		if err != nil {
			a.srvMu.Unlock()
			log.Printf("settings server: %v", err)
			w32.MessageBox(a.hwnd, "无法启动设置界面: "+err.Error(), "Glance", w32.MB_ICONERROR)
			return
		}
		a.srv = s
	}
	url := a.srv.URL()
	a.srvMu.Unlock()
	if section != "" {
		url += "#" + section
	}
	log.Printf("opening settings window (fresh server=%v)", fresh)
	go func() {
		if err := sys.OpenURLWindow(url); err != nil {
			log.Printf("open settings: %v", err)
		}
	}()
}

// ---------------- menus ----------------

func check(b bool) uint32 {
	if b {
		return w32.MF_CHECKED
	}
	return 0
}

func (a *App) showTrayMenu() {
	a.tray.stopPolling()
	a.hideCard()
	m := w32.CreatePopupMenu()
	defer w32.DestroyMenu(m)
	hk := func(s string) string {
		if s == "" {
			return ""
		}
		return "\t" + s
	}
	w32.AppendMenu(m, w32.MF_STRING|check(a.cfg.Overlay.Visible), 1, "显示悬浮窗"+hk(a.cfg.General.HotkeyToggle))
	w32.AppendMenu(m, w32.MF_STRING|check(a.editing), 2, "调整悬浮窗位置"+hk(a.cfg.General.HotkeyEdit))
	w32.AppendMenu(m, w32.MF_SEPARATOR, 0, "")
	w32.AppendMenu(m, w32.MF_STRING, 3, "设置…")
	w32.AppendMenu(m, w32.MF_STRING|check(a.autostart.Load()), 4, "开机自动启动")
	if !w32.IsElevated() {
		w32.AppendMenu(m, w32.MF_STRING, 5, "以管理员身份重新启动（启用温度/功耗/帧率）")
	}
	if sys.UpdateAvailable() != "" {
		w32.AppendMenu(m, w32.MF_STRING, 6, "安装新版本并重启")
	}
	w32.AppendMenu(m, w32.MF_SEPARATOR, 0, "")
	w32.AppendMenu(m, w32.MF_STRING, 9, "退出 Glance")

	a.menuOpen = true
	w32.SetForegroundWindow(a.hwnd)
	p := w32.GetCursorPos()
	cmd := w32.TrackPopupMenu(m, w32.TPM_RETURNCMD|w32.TPM_RIGHTBUTTON|w32.TPM_NONOTIFY, p.X, p.Y, a.hwnd)
	w32.PostMessage(a.hwnd, w32.WM_NULL, 0, 0)
	a.menuOpen = false
	switch cmd {
	case 1:
		a.toggleOverlay()
	case 2:
		a.setEditMode(!a.editing)
	case 3:
		a.openSettings("")
	case 4:
		a.setAutostart(!a.autostart.Load())
	case 5:
		a.restart(true)
	case 6:
		if err := a.applyUpdate(); err != nil {
			w32.MessageBox(a.hwnd, "更新失败: "+err.Error(), "Glance", w32.MB_ICONERROR)
		}
	case 9:
		a.Quit()
	}
}

func (a *App) showOverlayMenu() {
	m := w32.CreatePopupMenu()
	defer w32.DestroyMenu(m)
	w32.AppendMenu(m, w32.MF_STRING, 1, "锁定位置")
	w32.AppendMenu(m, w32.MF_STRING, 2, "恢复默认位置（左上角）")
	w32.AppendMenu(m, w32.MF_SEPARATOR, 0, "")
	w32.AppendMenu(m, w32.MF_STRING, 3, "设置…")
	a.menuOpen = true
	w32.SetForegroundWindow(a.hwnd)
	p := w32.GetCursorPos()
	cmd := w32.TrackPopupMenu(m, w32.TPM_RETURNCMD|w32.TPM_RIGHTBUTTON|w32.TPM_NONOTIFY, p.X, p.Y, a.hwnd)
	w32.PostMessage(a.hwnd, w32.WM_NULL, 0, 0)
	a.menuOpen = false
	switch cmd {
	case 1:
		a.setEditMode(false)
	case 2:
		a.resetPosition()
	case 3:
		a.openSettings("")
	}
}

func (a *App) setAutostart(on bool) error {
	err := sys.SetAutostart(on)
	if err != nil {
		log.Printf("autostart: %v", err)
	}
	a.autostart.Store(sys.AutostartEnabled())
	return err
}

func (a *App) applyUpdate() error {
	if a.opts.ApplyUpdate == nil {
		return errors.New("not supported")
	}
	a.saveNow()
	if err := a.opts.ApplyUpdate(); err != nil {
		log.Printf("update: %v", err)
		return err
	}
	log.Printf("update applied, restarting")
	w32.PostMessage(a.hwnd, w32.WM_CLOSE, 0, 0)
	return nil
}

func (a *App) restart(elevated bool) {
	if a.opts.Restart != nil && a.opts.Restart(elevated) {
		a.Quit()
	}
}

// ---------------- window procedure ----------------

func (a *App) wndProc(h w32.HWND, msg uint32, w, l uintptr) (uintptr, bool) {
	switch msg {
	case wmSnapshot:
		a.onSnapshot()
		return 0, true
	case wmRun:
		a.drainQueue()
		return 0, true
	case WMShowSettings:
		a.openSettings("")
		return 0, true
	case wmTray:
		a.onTray(w, l)
		return 0, true
	case w32.WM_HOTKEY:
		switch int32(w) {
		case hkToggle:
			a.toggleOverlay()
		case hkEdit:
			if !a.cfg.Overlay.Visible {
				a.toggleOverlay()
			}
			a.setEditMode(!a.editing)
		}
		return 0, true
	case w32.WM_TIMER:
		switch w {
		case timerSave:
			w32.KillTimer(a.hwnd, timerSave)
			a.saveNow()
		case timerHover:
			a.tray.onPollTimer()
		case timerShowCard:
			a.tray.onShowTimer()
		}
		return 0, true
	case w32.WM_SETTINGCHANGE, w32.WM_DISPLAYCHANGE:
		a.light = lightTaskbar()
		a.tray.iconKey = ""
		a.render()
		return 0, false
	case w32.WM_QUERYENDSESSION:
		return 1, true
	case w32.WM_ENDSESSION:
		if w != 0 {
			a.cleanup()
		}
		return 0, true
	case w32.WM_CLOSE:
		a.Quit()
		return 0, true
	}
	if msg == a.taskbarCreated && msg != 0 {
		a.tray.added = false
		a.tray.add()
		return 0, true
	}
	return 0, false
}

func (a *App) onTray(w, l uintptr) {
	switch uint32(w32.LOWORD(l)) {
	case w32.WM_MOUSEMOVE:
		a.tray.onMouseMove(w32.POINT{X: w32.GET_X(w), Y: w32.GET_Y(w)})
	case w32.NIN_POPUPOPEN:
		if a.cfg.Tray.HoverCard && !a.card.shown && !a.menuOpen {
			a.tray.hoverPos = w32.GetCursorPos()
			a.showCard(a.tray.iconRect())
		}
	case w32.WM_LBUTTONUP, w32.NIN_SELECT, w32.NIN_KEYSELECT:
		a.tray.stopPolling()
		a.hideCard()
		a.openSettings("")
	case w32.WM_CONTEXTMENU:
		a.showTrayMenu()
	}
}

// ---------------- web.Backend ----------------

func (a *App) Config() *config.Config { return a.config().Clone() }

func (a *App) SetConfig(c *config.Config) (*config.Config, error) {
	n := c.Clone()
	a.runOnUI(func() { a.applyConfig(n) })
	return a.config().Clone(), nil
}

func (a *App) Snapshot() *data.Snapshot { return a.mgr.Latest() }

func (a *App) Meta() map[string]any {
	a.fontsOnce.Do(func() {
		a.fonts = w32.FontFamilies()
		sort.Strings(a.fonts)
	})
	var mons []MonitorInfo
	a.runOnUI(func() { mons = listMonitors() })
	exe := sys.ExePath()
	return map[string]any{
		"version":    Version,
		"fonts":      a.fonts,
		"monitors":   mons,
		"exe":        exe,
		"configPath": a.opts.ConfigPath,
		"logPath":    a.opts.LogPath,
		"configError": a.opts.ConfigError,
	}
}

func (a *App) State() map[string]any {
	var editing, visible bool
	var hk map[string]string
	a.runOnUI(func() {
		editing = a.editing
		visible = a.overlay.shown
		hk = map[string]string{}
		for k, v := range a.hotkeyErr {
			hk[k] = v
		}
	})
	return map[string]any{
		"editing":        editing,
		"overlayShown":   visible,
		"autostart":      a.autostart.Load(),
		"elevated":       w32.IsElevated(),
		"hotkeyErrors":   hk,
		"updateReady":    sys.UpdateAvailable() != "",
		"cardShown":      a.card.shown,
	}
}

func (a *App) Action(name, arg string) (any, error) {
	var err error
	var res any = map[string]bool{"ok": true}
	switch name {
	case "toggle_overlay":
		a.runOnUI(a.toggleOverlay)
	case "edit_mode":
		a.runOnUI(func() {
			on := !a.editing
			if arg == "on" {
				on = true
			} else if arg == "off" {
				on = false
			}
			if on && !a.cfg.Overlay.Visible {
				a.toggleOverlay()
			}
			a.setEditMode(on)
		})
	case "reset_position":
		a.runOnUI(a.resetPosition)
	case "autostart":
		err = a.setAutostart(arg == "on")
	case "restart_admin":
		a.runOnUI(func() { a.restart(true) })
	case "open_log":
		err = sys.OpenPath(a.opts.LogPath)
	case "open_folder":
		err = sys.OpenPath(filepath.Dir(a.opts.ConfigPath))
	case "reset_config":
		d := config.Default()
		a.runOnUI(func() { a.applyConfig(d) })
		res = a.config().Clone()
	case "quit":
		a.runOnUI(a.Quit)
	case "apply_update":
		err = a.applyUpdate()
	case "diagnostics":
		res = a.diagnostics()
	default:
		err = errors.New("unknown action")
	}
	return res, err
}

func (a *App) diagnostics() string {
	s := a.mgr.Latest()
	var b strings.Builder
	b.WriteString("Glance " + Version + "\n")
	b.WriteString("Windows build: " + osBuild() + "\n")
	b.WriteString("CPU: " + s.CPUName + "\nGPU: " + s.GPUName + "\n")
	st := s.Status
	b.WriteString("Elevated: " + btoa(st.Elevated) + "  PawnIO: " + st.PawnIO + " " + st.PawnIOVersion +
		"  NVML: " + st.NVML + "  ETW: " + st.ETW + "  CPU temp: " + st.CPUTempSource + "\n")
	for _, r := range s.Readings {
		b.WriteString(r.Group + " | " + r.Name + " = " + ftoa(float32(r.Value)) + " " + r.Unit + "\n")
	}
	for k, v := range s.Values {
		if v.OK {
			b.WriteString("value " + k + " = " + ftoa(float32(v.V)) + "\n")
		}
	}
	log.Printf("diagnostics:\n%s", b.String())
	return b.String()
}

func osBuild() string {
	var k syscall.Handle
	p, _ := syscall.UTF16PtrFromString(`SOFTWARE\Microsoft\Windows NT\CurrentVersion`)
	if syscall.RegOpenKeyEx(syscall.HKEY_LOCAL_MACHINE, p, 0, syscall.KEY_READ, &k) != nil {
		return "?"
	}
	defer syscall.RegCloseKey(k)
	get := func(name string) string {
		n, _ := syscall.UTF16PtrFromString(name)
		buf := make([]uint16, 128)
		size := uint32(len(buf) * 2)
		var typ uint32
		if syscall.RegQueryValueEx(k, n, nil, &typ, (*byte)(unsafePointer(&buf[0])), &size) != nil {
			return ""
		}
		return syscall.UTF16ToString(buf)
	}
	return get("ProductName") + " " + get("DisplayVersion") + " (" + get("CurrentBuildNumber") + ")"
}

// layoutColor exposes threshold colouring to the tray code.
func layoutColor(cfg *config.Config, it *config.Item, v float64) uint32 {
	return layout.ValueColor(cfg, it, v, 0)
}


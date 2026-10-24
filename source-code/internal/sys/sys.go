//go:build windows

// Package sys holds process-level helpers: paths, logging, autostart via
// Task Scheduler, elevation and launching the settings window.
package sys

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unicode/utf16"

	"glance/internal/w32"
)

const TaskName = "GlanceHUD"

// ExePath returns the absolute path of the running executable.
func ExePath() string {
	p, err := os.Executable()
	if err != nil {
		return os.Args[0]
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// DataDir is where config and log live: next to the exe when writable
// (portable), otherwise %APPDATA%\Glance.
func DataDir() string {
	dir := filepath.Dir(ExePath())
	probe := filepath.Join(dir, ".glance-write-test")
	if f, err := os.Create(probe); err == nil {
		f.Close()
		os.Remove(probe)
		return dir
	}
	ad := os.Getenv("APPDATA")
	if ad == "" {
		return dir
	}
	d := filepath.Join(ad, "Glance")
	os.MkdirAll(d, 0o755)
	return d
}

// rotatingFile is a tiny size-capped log writer.
type rotatingFile struct {
	mu   sync.Mutex
	path string
	f    *os.File
	size int64
}

func (r *rotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return len(p), nil
		}
		st, _ := f.Stat()
		r.f, r.size = f, st.Size()
	}
	if r.size > 512*1024 {
		r.f.Close()
		os.Remove(r.path + ".old")
		os.Rename(r.path, r.path+".old")
		f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			r.f = nil
			return len(p), nil
		}
		r.f, r.size = f, 0
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

// SetupLog sends the standard logger to glance.log in dir.
func SetupLog(dir string) string {
	p := filepath.Join(dir, "glance.log")
	log.SetOutput(io.Writer(&rotatingFile{path: p}))
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	return p
}

func quote(s string) string { return `"` + s + `"` }

// run executes a console tool hidden and returns its exit code.
func run(cmdLine string) uint32 {
	h, err := w32.StartProcess("", cmdLine, true)
	if err != nil {
		log.Printf("run %s: %v", cmdLine, err)
		return 0xFFFFFFFF
	}
	defer w32.CloseHandle(h)
	return w32.WaitProcess(h, 15000)
}

func system32() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return filepath.Join(root, "System32")
}

// AutostartEnabled reports whether our logon task exists and points here.
func AutostartEnabled() bool {
	return run(quote(filepath.Join(system32(), "schtasks.exe"))+" /Query /TN "+TaskName) == 0
}

// SetAutostart creates or removes a logon task that starts Glance with
// highest privileges (no UAC prompt at every boot), also on battery.
func SetAutostart(on bool) error {
	schtasks := quote(filepath.Join(system32(), "schtasks.exe"))
	if !on {
		if code := run(schtasks + " /Delete /TN " + TaskName + " /F"); code != 0 && AutostartEnabled() {
			return fmt.Errorf("删除计划任务失败 (代码 %d)", code)
		}
		return nil
	}
	user := os.Getenv("USERDOMAIN") + `\` + os.Getenv("USERNAME")
	xml := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo><Description>Glance 硬件监控悬浮窗 开机自启</Description></RegistrationInfo>
  <Triggers>
    <LogonTrigger><Enabled>true</Enabled><UserId>%s</UserId><Delay>PT8S</Delay></LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author"><UserId>%s</UserId><LogonType>InteractiveToken</LogonType><RunLevel>HighestAvailable</RunLevel></Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>false</StartWhenAvailable>
    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>
    <IdleSettings><StopOnIdleEnd>false</StopOnIdleEnd><RestartOnIdle>false</RestartOnIdle></IdleSettings>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <Enabled>true</Enabled>
    <Hidden>false</Hidden>
    <RunOnlyIfIdle>false</RunOnlyIfIdle>
    <WakeToRun>false</WakeToRun>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Priority>5</Priority>
  </Settings>
  <Actions Context="Author">
    <Exec><Command>%s</Command><Arguments>--autostart</Arguments><WorkingDirectory>%s</WorkingDirectory></Exec>
  </Actions>
</Task>`, xmlEsc(user), xmlEsc(user), xmlEsc(ExePath()), xmlEsc(filepath.Dir(ExePath())))

	tmp := filepath.Join(os.TempDir(), "glance-task.xml")
	u := utf16.Encode([]rune(xml))
	buf := make([]byte, 2+2*len(u))
	buf[0], buf[1] = 0xFF, 0xFE
	for i, c := range u {
		binary.LittleEndian.PutUint16(buf[2+2*i:], c)
	}
	if err := os.WriteFile(tmp, buf, 0o600); err != nil {
		return err
	}
	defer os.Remove(tmp)
	if code := run(schtasks + " /Create /TN " + TaskName + " /XML " + quote(tmp) + " /F"); code != 0 {
		return fmt.Errorf("创建计划任务失败 (代码 %d)，请确认以管理员身份运行", code)
	}
	return nil
}

func xmlEsc(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

// RelaunchElevated starts a new elevated copy. Returns ERROR_CANCELLED if
// the user declined UAC.
func RelaunchElevated(args string) error {
	return w32.ShellExecute("runas", ExePath(), args, filepath.Dir(ExePath()), 1)
}

// OpenURLWindow shows url in an app-style Edge (or Chrome) window, started
// without admin rights even when Glance is elevated.
func OpenURLWindow(url string) error {
	browsers := []string{
		regAppPath("msedge.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), `Microsoft\Edge\Application\msedge.exe`),
		filepath.Join(os.Getenv("ProgramFiles"), `Microsoft\Edge\Application\msedge.exe`),
		regAppPath("chrome.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), `Google\Chrome\Application\chrome.exe`),
	}
	for _, b := range browsers {
		if b == "" {
			continue
		}
		if _, err := os.Stat(b); err != nil {
			continue
		}
		cmd := quote(b) + " --app=" + quote(url) + " --window-size=1080,760 --no-first-run --no-default-browser-check"
		if err := startUser(b, cmd); err == nil {
			return nil
		} else {
			log.Printf("open %s: %v", b, err)
		}
	}
	// Last resort: default browser through explorer (runs unelevated).
	explorer := filepath.Join(os.Getenv("SystemRoot"), "explorer.exe")
	return startUser(explorer, quote(explorer)+" "+quote(url))
}

// OpenPath opens a file or folder in its default app, unelevated.
func OpenPath(p string) error {
	explorer := filepath.Join(os.Getenv("SystemRoot"), "explorer.exe")
	return startUser(explorer, quote(explorer)+" "+quote(p))
}

func startUser(app, cmd string) error {
	if w32.IsElevated() {
		if err := w32.StartUnelevated(app, cmd); err == nil {
			return nil
		} else {
			log.Printf("unelevated launch failed (%v), falling back", err)
		}
	}
	h, err := w32.StartProcess(app, cmd, false)
	if err == nil {
		w32.CloseHandle(h)
	}
	return err
}

func regAppPath(exe string) string {
	for _, root := range []syscall.Handle{syscall.HKEY_LOCAL_MACHINE, syscall.HKEY_CURRENT_USER} {
		var k syscall.Handle
		p, _ := syscall.UTF16PtrFromString(`SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\` + exe)
		if syscall.RegOpenKeyEx(root, p, 0, syscall.KEY_READ, &k) != nil {
			continue
		}
		var typ, size uint32
		syscall.RegQueryValueEx(k, nil, nil, &typ, nil, &size)
		if size > 0 {
			buf := make([]uint16, size/2+1)
			if syscall.RegQueryValueEx(k, nil, nil, &typ, (*byte)(unsafePtr(&buf[0])), &size) == nil {
				syscall.RegCloseKey(k)
				return strings.Trim(syscall.UTF16ToString(buf), `"`)
			}
		}
		syscall.RegCloseKey(k)
	}
	return ""
}

// ---- in-place update: Glance.new.exe next to the exe ----

func newExePath() string { return filepath.Join(filepath.Dir(ExePath()), "Glance.new.exe") }
func oldExePath() string { return filepath.Join(filepath.Dir(ExePath()), "Glance.old.exe") }

var (
	updMu    sync.Mutex
	updKey   string
	updValid bool
)

// UpdateAvailable returns a valid pending update, or "". Invalid/incomplete
// files are left alone: a copy may still be in progress.
func UpdateAvailable() string {
	p := newExePath()
	st, err := os.Stat(p)
	if err != nil {
		return ""
	}
	updMu.Lock()
	defer updMu.Unlock()
	key := fmt.Sprintf("%d|%d", st.Size(), st.ModTime().UnixNano())
	if key == updKey {
		if updValid {
			return p
		}
		return ""
	}
	updKey, updValid = key, false
	if validateUpdate(p) != nil || sameFile(p, ExePath()) {
		return ""
	}
	updValid = true
	return p
}

// ApplyUpdate renames the running exe out of the way (allowed on Windows)
// and starts the replacement. A failed launch restores both file names.
func ApplyUpdate(start func() error) error {
	updMu.Lock()
	defer updMu.Unlock()
	defer func() { updKey, updValid = "", false }()
	return applyUpdate(ExePath(), start)
}

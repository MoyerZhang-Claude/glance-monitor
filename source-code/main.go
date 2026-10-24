//go:build windows

// Glance — a lightweight hardware monitor overlay for Windows.
package main

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"glance/internal/config"
	"glance/internal/fps"
	"glance/internal/sys"
	"glance/internal/ui"
	"glance/internal/w32"
)

const mutexName = `Local\GlanceHUD.SingleInstance`

func init() { runtime.LockOSThread() }

func hasArg(name string) bool {
	for _, a := range os.Args[1:] {
		if strings.EqualFold(a, name) {
			return true
		}
	}
	return false
}

func main() {
	debug.SetGCPercent(50) // keep the resident set small; we allocate little
	dir := sys.DataDir()
	logPath := sys.SetupLog(dir)
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC: %v\n%s", r, debug.Stack())
			w32.MessageBox(0, "Glance 遇到错误并退出，详情见 glance.log", "Glance", w32.MB_ICONERROR)
			os.Exit(2)
		}
	}()

	// After an in-place update the previous process may still be exiting.
	waitOld := hasArg("--updated")
	mutex, exists := w32.CreateMutex(mutexName)
	for i := 0; exists && waitOld && i < 150; i++ {
		w32.CloseHandle(mutex)
		time.Sleep(100 * time.Millisecond)
		mutex, exists = w32.CreateMutex(mutexName)
	}
	if exists {
		// Already running: ask the first instance to show its settings.
		if h := w32.FindWindow(ui.MainClass); h != 0 {
			w32.PostMessage(h, ui.WMShowSettings, 0, 0)
		}
		return
	}

	cfgPath := filepath.Join(dir, "Glance.json")
	_, statErr := os.Stat(cfgPath)
	firstRun := errors.Is(statErr, os.ErrNotExist)
	cfg, cfgErr := config.Load(cfgPath)
	cfgErrText := ""
	if cfgErr != nil {
		cfgErrText = cfgErr.Error()
		log.Printf("config: %v", cfgErr)
	}

	passArgs := func() string {
		var keep []string
		for _, a := range os.Args[1:] {
			if !strings.EqualFold(a, "--elevated") {
				keep = append(keep, a)
			}
		}
		return strings.Join(keep, " ")
	}

	relaunch := func(extra string) bool {
		w32.CloseHandle(mutex)
		err := sys.RelaunchElevated(strings.TrimSpace("--elevated " + extra))
		if err == nil {
			return true
		}
		if w32.IsErr(err, syscall.Errno(1223)) { // ERROR_CANCELLED
			log.Printf("elevation declined by user")
		} else {
			log.Printf("elevation failed: %v", err)
		}
		mutex, _ = w32.CreateMutex(mutexName)
		return false
	}

	if !w32.IsElevated() && cfg.General.AutoElevate && !hasArg("--no-elevate") && !hasArg("--elevated") {
		if relaunch(passArgs()) {
			return
		}
	}

	if w32.IsElevated() {
		fps.StopStaleSession()
	}

	err := ui.Run(cfg, ui.Options{
		ConfigPath:   cfgPath,
		LogPath:      logPath,
		FirstRun:     firstRun,
		OpenSettings: hasArg("--settings"),
		ConfigError:  cfgErrText,
		Restart: func(elevated bool) bool {
			return relaunch("--settings")
		},
		ApplyUpdate: func() error {
			// Keep the mutex: the new process waits for it, so it never
			// overlaps with our shutdown (e.g. the ETW session).
			args := "--updated --settings"
			if w32.IsElevated() {
				args += " --elevated"
			}
			return sys.ApplyUpdate(func() error {
				h, err := w32.StartProcess(sys.ExePath(), `"`+sys.ExePath()+`" `+args, false)
				if err == nil {
					w32.CloseHandle(h)
				}
				return err
			})
		},
	})
	if err != nil {
		log.Printf("fatal: %v", err)
		w32.MessageBox(0, err.Error(), "Glance", w32.MB_ICONERROR)
	}
	w32.CloseHandle(mutex)
}

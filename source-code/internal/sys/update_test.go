package sys

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

var fixtureOnce sync.Once
var fixtureImage []byte
var fixtureErr error

func updateFixture(t *testing.T) []byte {
	t.Helper()
	fixtureOnce.Do(func() {
		var dir string
		dir, fixtureErr = os.MkdirTemp("", "glance-update-test-")
		if fixtureErr != nil {
			return
		}
		defer os.RemoveAll(dir)
		for name, content := range map[string]string{
			"go.mod":  "module glance\n\ngo 1.24\n",
			"main.go": "package main\nfunc main() {}\n",
		} {
			if fixtureErr = os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); fixtureErr != nil {
				return
			}
		}
		goBin := "go"
		if runtime.GOOS == "windows" {
			goBin += ".exe"
		}
		cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", goBin), "build", "-o", "fixture.exe", ".")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0", "GOWORK=off")
		if out, err := cmd.CombinedOutput(); err != nil {
			fixtureErr = errors.New(string(out) + err.Error())
			return
		}
		fixtureImage, fixtureErr = os.ReadFile(filepath.Join(dir, "fixture.exe"))
	})
	if fixtureErr != nil {
		t.Fatal(fixtureErr)
	}
	return bytes.Clone(fixtureImage)
}

func writeUpdateFile(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestValidateUpdate(t *testing.T) {
	valid := updateFixture(t)
	wrongArch := bytes.Clone(valid)
	peOffset := binary.LittleEndian.Uint32(wrongArch[0x3c:])
	binary.LittleEndian.PutUint16(wrongArch[peOffset+4:], 0xaa64)
	dll := bytes.Clone(valid)
	flags := binary.LittleEndian.Uint16(dll[peOffset+22:])
	binary.LittleEndian.PutUint16(dll[peOffset+22:], flags|0x2000)
	for _, tc := range []struct {
		name   string
		data   []byte
		wantOK bool
	}{
		{"valid", valid, true},
		{"large non executable", bytes.Repeat([]byte("x"), 2<<20), false},
		{"truncated section", valid[:len(valid)-1], false},
		{"wrong architecture", wrongArch, false},
		{"DLL", dll, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "Glance.new.exe")
			writeUpdateFile(t, path, tc.data)
			if err := validateUpdate(path); (err == nil) != tc.wantOK {
				t.Fatalf("validateUpdate: %v; want valid=%v", err, tc.wantOK)
			}
		})
	}
	// A different real executable must not be accepted merely because it is PE.
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := validateUpdate(self); err == nil {
		t.Fatal("accepted unrelated test executable")
	}
}

func TestUpdateTransaction(t *testing.T) {
	for _, failLaunch := range []bool{false, true} {
		t.Run(map[bool]string{false: "success retains backup", true: "launch failure rolls back"}[failLaunch], func(t *testing.T) {
			dir := t.TempDir()
			exe, newp, old := filepath.Join(dir, "Glance.exe"), filepath.Join(dir, "Glance.new.exe"), filepath.Join(dir, "Glance.old.exe")
			oldImage := append(updateFixture(t), []byte("old version")...)
			newImage := updateFixture(t)
			writeUpdateFile(t, exe, oldImage)
			writeUpdateFile(t, newp, newImage)
			launchErr := errors.New("simulated CreateProcess failure")
			called := false
			err := applyUpdate(exe, func() error {
				called = true
				got, _ := os.ReadFile(exe)
				if !bytes.Equal(got, newImage) {
					t.Fatal("launch did not see new image")
				}
				if failLaunch {
					return launchErr
				}
				return nil
			})
			if !called || (err != nil) != failLaunch {
				t.Fatalf("called=%v err=%v", called, err)
			}
			if failLaunch {
				if !errors.Is(err, launchErr) {
					t.Fatal(err)
				}
				got, _ := os.ReadFile(exe)
				candidate, _ := os.ReadFile(newp)
				if !bytes.Equal(got, oldImage) || !bytes.Equal(candidate, newImage) {
					t.Fatal("rollback lost files")
				}
			} else {
				backup, _ := os.ReadFile(old)
				if !bytes.Equal(backup, oldImage) {
					t.Fatal("previous version not retained")
				}
				if _, err := os.Stat(newp); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("pending file still exists")
				}
			}
		})
	}
}

func TestInvalidUpdatePreservesCurrent(t *testing.T) {
	dir := t.TempDir()
	exe, newp := filepath.Join(dir, "Glance.exe"), filepath.Join(dir, "Glance.new.exe")
	original := updateFixture(t)
	writeUpdateFile(t, exe, original)
	writeUpdateFile(t, newp, bytes.Repeat([]byte("x"), 2<<20))
	if err := applyUpdate(exe, func() error { t.Fatal("invalid image launched"); return nil }); err == nil {
		t.Fatal("invalid image accepted")
	}
	got, _ := os.ReadFile(exe)
	if !bytes.Equal(got, original) {
		t.Fatal("current image modified")
	}
	if _, err := os.Stat(newp); err != nil {
		t.Fatal("invalid/incomplete candidate removed")
	}
	// The candidate can be replaced after a UI check; applyUpdate must read it again.
	writeUpdateFile(t, newp, original)
	if err := applyUpdate(exe, func() error { t.Fatal("identical update launched"); return nil }); err == nil {
		t.Fatal("identical image accepted")
	}
}

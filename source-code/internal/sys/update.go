package sys

import (
	"crypto/sha256"
	"debug/buildinfo"
	"debug/pe"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// validateUpdate checks the complete on-disk image, not just its MZ header.
// This is a compatibility/corruption check, not a signature/authenticity check.
func validateUpdate(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("更新文件不是普通文件")
	}
	p, err := pe.NewFile(f)
	if err != nil {
		return fmt.Errorf("更新文件不是有效的 Windows 程序: %w", err)
	}
	defer p.Close()
	h, ok := p.OptionalHeader.(*pe.OptionalHeader64)
	if !ok || p.Machine != pe.IMAGE_FILE_MACHINE_AMD64 ||
		p.Characteristics&pe.IMAGE_FILE_EXECUTABLE_IMAGE == 0 ||
		p.Characteristics&pe.IMAGE_FILE_DLL != 0 || h.AddressOfEntryPoint == 0 {
		return fmt.Errorf("更新文件必须是 Windows x64 可执行程序")
	}
	if len(p.Sections) == 0 || h.SizeOfHeaders == 0 || int64(h.SizeOfHeaders) > st.Size() {
		return fmt.Errorf("更新文件头不完整")
	}
	for _, s := range p.Sections {
		if s.Size > 0 && (s.Offset < h.SizeOfHeaders || uint64(s.Offset)+uint64(s.Size) > uint64(st.Size())) {
			return fmt.Errorf("更新文件不完整（%s），请等待复制完成", s.Name)
		}
	}
	info, err := buildinfo.Read(f)
	if err != nil || info.Path != "glance" || info.Main.Path != "glance" {
		return fmt.Errorf("更新文件不是 Glance")
	}
	return nil
}

func sameFile(a, b string) bool {
	ha, err1 := fileHash(a)
	hb, err2 := fileHash(b)
	return err1 == nil && err2 == nil && ha == hb
}

func fileHash(path string) ([32]byte, error) {
	var sum [32]byte
	f, err := os.Open(path)
	if err != nil {
		return sum, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return sum, err
	}
	copy(sum[:], h.Sum(nil))
	return sum, nil
}

func applyUpdate(exe string, start func() error) error {
	if start == nil {
		return fmt.Errorf("缺少更新启动操作")
	}
	dir := filepath.Dir(exe)
	newp, old := filepath.Join(dir, "Glance.new.exe"), filepath.Join(dir, "Glance.old.exe")
	// Always revalidate here, even if the settings page cached an earlier result.
	if err := validateUpdate(newp); err != nil {
		return err
	}
	if sameFile(exe, newp) {
		return fmt.Errorf("更新文件与当前版本相同")
	}
	if err := os.Remove(old); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("无法清理上次更新备份: %w", err)
	}
	if err := os.Rename(exe, old); err != nil {
		return fmt.Errorf("无法备份当前版本: %w", err)
	}
	if err := os.Rename(newp, exe); err != nil {
		return errors.Join(fmt.Errorf("安装更新失败: %w", err), restoreOld(old, exe))
	}
	if err := start(); err != nil {
		launchErr := fmt.Errorf("新版启动失败: %w", err)
		if moveErr := os.Rename(exe, newp); moveErr != nil {
			return errors.Join(launchErr, fmt.Errorf("无法移回新版，旧版保存在 %s: %w", old, moveErr))
		}
		return errors.Join(launchErr, restoreOld(old, exe))
	}
	// Retain the backup until the next update, including if the new process crashes.
	return nil
}

func restoreOld(old, exe string) error {
	if err := os.Rename(old, exe); err != nil {
		return fmt.Errorf("恢复旧版失败，备份在 %s: %w", old, err)
	}
	return nil
}

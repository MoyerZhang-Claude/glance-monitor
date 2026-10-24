//go:build windows

package sensors

import (
	"syscall"
	"unsafe"

	"glance/internal/w32"
)

// nvAPI wraps the few NVAPI calls used as a fallback for GPU power.
type nvAPI struct {
	gpu       uintptr
	topology  uintptr
}

func loadNVAPI(bus uint32) *nvAPI {
	dll, err := w32.LoadDLL("nvapi64.dll", false)
	if err != nil {
		return nil
	}
	qi, err := dll.FindProc("nvapi_QueryInterface")
	if err != nil {
		return nil
	}
	fn := func(id uint32) uintptr { r, _, _ := qi.Call(uintptr(id)); return r }
	initFn := fn(0x0150E828)
	enum := fn(0xE5AC921F)
	getBus := fn(0x1BE0B8E5)
	topo := fn(0xEDCF624E)
	if initFn == 0 || enum == 0 || topo == 0 {
		return nil
	}
	if r, _, _ := syscall.SyscallN(initFn); int32(r) != 0 {
		return nil
	}
	var handles [64]uintptr
	var count uint32
	if r, _, _ := syscall.SyscallN(enum, uintptr(unsafe.Pointer(&handles[0])), uintptr(unsafe.Pointer(&count))); int32(r) != 0 || count == 0 {
		return nil
	}
	a := &nvAPI{gpu: handles[0], topology: topo}
	if getBus != 0 {
		for i := uint32(0); i < count; i++ {
			var b uint32
			if r, _, _ := syscall.SyscallN(getBus, handles[i], uintptr(unsafe.Pointer(&b))); int32(r) == 0 && b == bus {
				a.gpu = handles[i]
				break
			}
		}
	}
	return a
}

type nvPowerTopology struct {
	Version uint32
	Count   uint32
	Entries [4]struct {
		Domain    uint32
		_         uint32
		Usage     uint32 // 1/1000 percent of the power limit
		_         uint32
	}
}

// powerPercent returns GPU power as percent of its power limit.
func (a *nvAPI) powerPercent() (float64, int32) {
	if a == nil {
		return 0, -1
	}
	var t nvPowerTopology
	t.Version = uint32(unsafe.Sizeof(t)) | 1<<16
	r, _, _ := syscall.SyscallN(a.topology, a.gpu, uintptr(unsafe.Pointer(&t)))
	if int32(r) != 0 {
		return 0, int32(r)
	}
	for i := uint32(0); i < t.Count && i < 4; i++ {
		if t.Entries[i].Domain == 0 {
			return float64(t.Entries[i].Usage) / 1000, 0
		}
	}
	// Some boards only report the board domain.
	for i := uint32(0); i < t.Count && i < 4; i++ {
		if t.Entries[i].Domain == 1 {
			return float64(t.Entries[i].Usage) / 1000, 0
		}
	}
	return float64(t.Count), -2
}

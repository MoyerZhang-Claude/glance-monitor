//go:build windows

package sensors

import (
	"syscall"
	"unsafe"
)

var (
	pdh                          = syscall.NewLazyDLL("pdh.dll")
	procPdhOpenQueryW            = pdh.NewProc("PdhOpenQueryW")
	procPdhAddEnglishCounterW    = pdh.NewProc("PdhAddEnglishCounterW")
	procPdhCollectQueryData      = pdh.NewProc("PdhCollectQueryData")
	procPdhGetFormattedCounterValue = pdh.NewProc("PdhGetFormattedCounterValue")
	procPdhGetFormattedCounterArrayW = pdh.NewProc("PdhGetFormattedCounterArrayW")
	procPdhCloseQuery            = pdh.NewProc("PdhCloseQuery")
)

const (
	pdhFmtDouble   = 0x00000200
	pdhFmtNoCap100 = 0x00008000
	pdhMoreData    = 0x800007D2
)

type pdhQuery struct{ h uintptr }

type pdhCounter struct{ h uintptr }

func newPdhQuery() *pdhQuery {
	q := &pdhQuery{}
	r, _, _ := procPdhOpenQueryW.Call(0, 0, uintptr(unsafe.Pointer(&q.h)))
	if r != 0 {
		return nil
	}
	return q
}

func (q *pdhQuery) add(path string) *pdhCounter {
	if q == nil {
		return nil
	}
	c := &pdhCounter{}
	p, _ := syscall.UTF16PtrFromString(path)
	r, _, _ := procPdhAddEnglishCounterW.Call(q.h, uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(&c.h)))
	if r != 0 {
		return nil
	}
	return c
}

func (q *pdhQuery) collect() bool {
	if q == nil {
		return false
	}
	r, _, _ := procPdhCollectQueryData.Call(q.h)
	return r == 0
}

func (q *pdhQuery) close() {
	if q != nil && q.h != 0 {
		procPdhCloseQuery.Call(q.h)
		q.h = 0
	}
}

type pdhValue struct {
	Status uint32
	_      uint32
	Double float64
}

func (c *pdhCounter) value() (float64, bool) {
	if c == nil {
		return 0, false
	}
	var v pdhValue
	r, _, _ := procPdhGetFormattedCounterValue.Call(c.h, pdhFmtDouble|pdhFmtNoCap100, 0, uintptr(unsafe.Pointer(&v)))
	if r != 0 || v.Status > 1 {
		return 0, false
	}
	return v.Double, true
}

type pdhItem struct {
	Name   *uint16
	Status uint32
	_      uint32
	Double float64
}

// array returns instance name -> value for wildcard counters.
func (c *pdhCounter) array() map[string]float64 {
	if c == nil {
		return nil
	}
	var size, count uint32
	r, _, _ := procPdhGetFormattedCounterArrayW.Call(c.h, pdhFmtDouble|pdhFmtNoCap100, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), 0)
	if uint32(r) != pdhMoreData || size == 0 {
		return nil
	}
	buf := make([]byte, size)
	r, _, _ = procPdhGetFormattedCounterArrayW.Call(c.h, pdhFmtDouble|pdhFmtNoCap100, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&buf[0])))
	if r != 0 {
		return nil
	}
	items := unsafe.Slice((*pdhItem)(unsafe.Pointer(&buf[0])), count)
	out := make(map[string]float64, count)
	for _, it := range items {
		if it.Status > 1 {
			continue
		}
		out[ptrString(it.Name)] = it.Double
	}
	return out
}

func ptrString(p *uint16) string {
	if p == nil {
		return ""
	}
	n := 0
	for ptr := unsafe.Pointer(p); *(*uint16)(ptr) != 0 && n < 512; n++ {
		ptr = unsafe.Add(ptr, 2)
	}
	return syscall.UTF16ToString(unsafe.Slice(p, n))
}

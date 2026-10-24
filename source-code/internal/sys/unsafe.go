//go:build windows

package sys

import "unsafe"

func unsafePtr(p *uint16) unsafe.Pointer { return unsafe.Pointer(p) }

//go:build windows

package fps

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

var (
	advapi32               = syscall.NewLazyDLL("advapi32.dll")
	procStartTraceW        = advapi32.NewProc("StartTraceW")
	procControlTraceW      = advapi32.NewProc("ControlTraceW")
	procEnableTraceEx2     = advapi32.NewProc("EnableTraceEx2")
	procOpenTraceW         = advapi32.NewProc("OpenTraceW")
	procProcessTrace       = advapi32.NewProc("ProcessTrace")
	procCloseTrace         = advapi32.NewProc("CloseTrace")
	tdh                    = syscall.NewLazyDLL("tdh.dll")
	procTdhGetEventInformation = tdh.NewProc("TdhGetEventInformation")
	ntdll                  = syscall.NewLazyDLL("ntdll.dll")
	procRtlGetVersion      = ntdll.NewProc("RtlGetVersion")
)

const sessionName = "GlanceHUD-PresentTrace"

const invalidProcessTraceHandle = ^uint64(0)

// Session is a real-time ETW session collecting present events.
type Session struct {
	tracker *Tracker
	session uint64
	trace   uint64
	props   *traceProps
	running atomic.Bool
	done    chan struct{}
	selfPID uint32
	Events  atomic.Uint64
	Lost    atomic.Uint32

	modelMu  sync.Mutex
	modelOff map[uint8]int // PresentHistory_Start version -> byte offset of Model (-1 unknown)
}

var current atomic.Pointer[Session]

var recordCallback = syscall.NewCallback(func(rec *eventRecord) uintptr {
	if s := current.Load(); s != nil {
		s.handle(rec)
	}
	return 0
})

func windowsBuild() uint32 {
	var vi struct {
		Size, Major, Minor, Build, Platform uint32
		CSD                                 [128]uint16
	}
	vi.Size = uint32(unsafe.Sizeof(vi))
	procRtlGetVersion.Call(uintptr(unsafe.Pointer(&vi)))
	return vi.Build
}

func newProps() *traceProps {
	p := &traceProps{}
	p.P.Wnode.BufferSize = uint32(unsafe.Sizeof(*p))
	p.P.Wnode.ClientContext = 1 // QPC timestamps
	p.P.Wnode.Flags = wnodeFlagTracedGUID
	p.P.LogFileMode = eventTraceRealTimeMode | eventTraceNoPerProcessorBuf
	p.P.BufferSize = 16 // KB
	p.P.MinimumBuffers = 8
	p.P.MaximumBuffers = 64
	p.P.FlushTimer = 1
	p.P.LoggerNameOffset = uint32(unsafe.Offsetof(p.LoggerName))
	p.P.LogFileNameOffset = 0
	return p
}

func stopNamed(name string) {
	p := newProps()
	n, _ := syscall.UTF16PtrFromString(name)
	procControlTraceW.Call(0, uintptr(unsafe.Pointer(n)), uintptr(unsafe.Pointer(p)), eventTraceControlStop)
}

// StopStaleSession removes a session left behind by a crashed instance.
func StopStaleSession() { stopNamed(sessionName) }

// Start creates the session and begins consuming events on a dedicated
// goroutine. It requires administrator rights (or Performance Log Users).
func Start(tr *Tracker, selfPID uint32) (*Session, error) {
	s := &Session{tracker: tr, done: make(chan struct{}), selfPID: selfPID, modelOff: map[uint8]int{}}
	name, _ := syscall.UTF16PtrFromString(sessionName)

	for attempt := 0; attempt < 2; attempt++ {
		s.props = newProps()
		r, _, _ := procStartTraceW.Call(uintptr(unsafe.Pointer(&s.session)), uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(s.props)))
		if r == 0 {
			break
		}
		if syscall.Errno(r) == syscall.ERROR_ALREADY_EXISTS && attempt == 0 {
			stopNamed(sessionName)
			continue
		}
		return nil, syscall.Errno(r)
	}
	sessGUID := s.props.P.Wnode.Guid

	win11 := windowsBuild() >= 22000
	dxgkKeyword := uint64(0x1) // Base
	if win11 {
		dxgkKeyword |= 0x8000000 // Present
	}
	providers := []struct {
		g   guid
		ids []uint16
		kw  uint64
	}{
		{guidDxgKrnl, []uint16{idDxgkPresentHistoryStart}, dxgkKeyword},
		{guidDXGI, []uint16{idDXGIPresentStart, idDXGIPresentMPOStart}, 0x8000000000000002},
		{guidD3D9, []uint16{idD3D9PresentStart}, 0x8000000000000002},
	}
	enabled := 0
	for _, pr := range providers {
		if err := enable(s.session, pr.g, pr.ids, pr.kw, sessGUID); err == nil {
			enabled++
		}
	}
	if enabled == 0 {
		s.stopSession()
		return nil, errors.New("无法启用任何帧事件提供程序")
	}

	lf := &eventTraceLogfile{}
	lf.LoggerName = name
	lf.ProcessTraceMode = processTraceModeRealTime | processTraceModeEventRecord | processTraceModeRawTimestamp
	lf.EventRecordCallback = recordCallback
	h, _, err := procOpenTraceW.Call(uintptr(unsafe.Pointer(lf)))
	if uint64(h) == invalidProcessTraceHandle {
		s.stopSession()
		return nil, err
	}
	s.trace = uint64(h)
	current.Store(s)
	s.running.Store(true)
	go func() {
		runtime.LockOSThread()
		defer close(s.done)
		th := s.trace
		procProcessTrace.Call(uintptr(unsafe.Pointer(&th)), 1, 0, 0)
	}()
	return s, nil
}

func enable(session uint64, g guid, ids []uint16, kw uint64, src guid) error {
	// EVENT_FILTER_EVENT_ID { BOOLEAN FilterIn; UCHAR Reserved; USHORT Count; USHORT Events[] }
	filter := make([]byte, 4+2*len(ids))
	filter[0] = 1
	filter[2] = byte(len(ids))
	for i, id := range ids {
		filter[4+2*i] = byte(id)
		filter[5+2*i] = byte(id >> 8)
	}
	desc := eventFilterDescriptor{Ptr: uint64(uintptr(unsafe.Pointer(&filter[0]))), Size: uint32(len(filter)), Type: eventFilterTypeEventID}
	params := enableTraceParameters{
		Version:          enableTraceParamsVersion2,
		EnableProperty:   eventEnablePropIgnoreKeyword0,
		SourceID:         src,
		EnableFilterDesc: uintptr(unsafe.Pointer(&desc)),
		FilterDescCount:  1,
	}
	r, _, _ := procEnableTraceEx2.Call(uintptr(session), uintptr(unsafe.Pointer(&g)), eventControlCodeEnable,
		0, uintptr(kw), uintptr(kw), 0, uintptr(unsafe.Pointer(&params)))
	runtime.KeepAlive(filter)
	runtime.KeepAlive(&desc)
	if r == 0 {
		return nil
	}
	// Fallback without event-id filtering.
	r, _, _ = procEnableTraceEx2.Call(uintptr(session), uintptr(unsafe.Pointer(&g)), eventControlCodeEnable,
		0, uintptr(kw), uintptr(kw), 0, 0)
	if r != 0 {
		return syscall.Errno(r)
	}
	return nil
}

func (s *Session) stopSession() {
	p := newProps()
	procControlTraceW.Call(uintptr(s.session), 0, uintptr(unsafe.Pointer(p)), eventTraceControlStop)
	s.Lost.Store(p.P.EventsLost)
}

// Stop ends the session and waits for the consumer goroutine.
func (s *Session) Stop() {
	if s == nil || !s.running.Swap(false) {
		return
	}
	s.stopSession()
	procCloseTrace.Call(uintptr(s.trace))
	<-s.done
	current.CompareAndSwap(s, nil)
}

func (s *Session) handle(rec *eventRecord) {
	h := &rec.EventHeader
	pid := h.ProcessID
	if pid == s.selfPID || pid == 0 || pid == 4 {
		return
	}
	ptrSize := 8
	if h.Flags&eventHeaderFlag32Bit != 0 {
		ptrSize = 4
	}
	ud := rec.UserData
	n := int(rec.UserDataLength)
	switch h.ProviderID {
	case guidDXGI:
		if h.EventDescriptor.ID != idDXGIPresentStart && h.EventDescriptor.ID != idDXGIPresentMPOStart {
			return
		}
		if n < ptrSize+4 {
			return
		}
		chain := readPtr(ud, ptrSize)
		flags := *(*uint32)(unsafe.Pointer(ud + uintptr(ptrSize)))
		if flags&dxgiPresentTest != 0 {
			return
		}
		s.tracker.Add(Event{PID: pid, TS: h.TimeStamp, Src: SrcDXGI, Chain: chain})
	case guidD3D9:
		if h.EventDescriptor.ID != idD3D9PresentStart || n < ptrSize {
			return
		}
		s.tracker.Add(Event{PID: pid, TS: h.TimeStamp, Src: SrcD3D9, Chain: readPtr(ud, ptrSize)})
	case guidDxgKrnl:
		if h.EventDescriptor.ID != idDxgkPresentHistoryStart {
			return
		}
		if off := s.modelOffset(rec); off >= 0 && off+4 <= n {
			if *(*uint32)(unsafe.Pointer(ud + uintptr(off))) == presentModelRedirectedGDI {
				return
			}
		}
		s.tracker.Add(Event{PID: pid, TS: h.TimeStamp, Src: SrcKernel})
	default:
		return
	}
	s.Events.Add(1)
}

func readPtr(p uintptr, size int) uint64 {
	if size == 4 {
		return uint64(*(*uint32)(unsafe.Pointer(p)))
	}
	return *(*uint64)(unsafe.Pointer(p))
}

// modelOffset finds the byte offset of the "Model" field of
// DxgKrnl PresentHistory_Start using TDH (cached per event version).
func (s *Session) modelOffset(rec *eventRecord) int {
	v := rec.EventHeader.EventDescriptor.Version
	s.modelMu.Lock()
	off, ok := s.modelOff[v]
	s.modelMu.Unlock()
	if ok {
		return off
	}
	off = tdhFieldOffset(rec, "Model")
	s.modelMu.Lock()
	s.modelOff[v] = off
	s.modelMu.Unlock()
	return off
}

func tdhFieldOffset(rec *eventRecord, field string) int {
	var size uint32
	r, _, _ := procTdhGetEventInformation.Call(uintptr(unsafe.Pointer(rec)), 0, 0, 0, uintptr(unsafe.Pointer(&size)))
	if syscall.Errno(r) != syscall.ERROR_INSUFFICIENT_BUFFER || size == 0 {
		return -1
	}
	buf := make([]byte, size)
	r, _, _ = procTdhGetEventInformation.Call(uintptr(unsafe.Pointer(rec)), 0, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r != 0 {
		return -1
	}
	ptrSize := 8
	if rec.EventHeader.Flags&eventHeaderFlag32Bit != 0 {
		ptrSize = 4
	}
	topCount := int(*(*uint32)(unsafe.Pointer(&buf[104])))
	off := 0
	for i := 0; i < topCount; i++ {
		base := 112 + 24*i
		if base+24 > len(buf) {
			return -1
		}
		flags := *(*uint32)(unsafe.Pointer(&buf[base]))
		nameOff := *(*uint32)(unsafe.Pointer(&buf[base+4]))
		inType := *(*uint16)(unsafe.Pointer(&buf[base+8]))
		count := *(*uint16)(unsafe.Pointer(&buf[base+16]))
		length := *(*uint16)(unsafe.Pointer(&buf[base+18]))
		name := utf16At(buf, int(nameOff))
		if flags&0x7 != 0 { // struct / param length / param count
			return -1
		}
		var sz int
		switch inType {
		case 3, 4:
			sz = 1
		case 5, 6:
			sz = 2
		case 7, 8, 11, 13, 20:
			sz = 4
		case 9, 10, 12, 17, 21:
			sz = 8
		case 15, 18:
			sz = 16
		case 16:
			sz = ptrSize
		case 14:
			sz = int(length)
		default:
			if name == field {
				return off
			}
			return -1
		}
		if name == field {
			return off
		}
		if count > 1 {
			sz *= int(count)
		}
		off += sz
	}
	return -1
}

func utf16At(buf []byte, off int) string {
	if off <= 0 || off >= len(buf) {
		return ""
	}
	var u []uint16
	for i := off; i+1 < len(buf); i += 2 {
		c := uint16(buf[i]) | uint16(buf[i+1])<<8
		if c == 0 {
			break
		}
		u = append(u, c)
	}
	return syscall.UTF16ToString(u)
}

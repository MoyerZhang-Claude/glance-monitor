package fps

// Layouts of the ETW structures we use (x64). Kept platform independent so
// their sizes can be verified by unit tests on any amd64 machine.

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var (
	guidDXGI    = guid{0xCA11C036, 0x0102, 0x4A2D, [8]byte{0xA6, 0xAD, 0xF0, 0x3C, 0xFE, 0xD5, 0xD3, 0xC9}}
	guidD3D9    = guid{0x783ACA0A, 0x790E, 0x4D7F, [8]byte{0x84, 0x51, 0xAA, 0x85, 0x05, 0x11, 0xC6, 0xB9}}
	guidDxgKrnl = guid{0x802EC45A, 0x1E99, 0x4B83, [8]byte{0x99, 0x20, 0x87, 0xC9, 0x82, 0x77, 0xBA, 0x9D}}
)

type wnodeHeader struct {
	BufferSize        uint32
	ProviderID        uint32
	HistoricalContext uint64
	TimeStamp         int64
	Guid              guid
	ClientContext     uint32
	Flags             uint32
}

type eventTraceProperties struct {
	Wnode               wnodeHeader
	BufferSize          uint32
	MinimumBuffers      uint32
	MaximumBuffers      uint32
	MaximumFileSize     uint32
	LogFileMode         uint32
	FlushTimer          uint32
	EnableFlags         uint32
	AgeLimit            int32
	NumberOfBuffers     uint32
	FreeBuffers         uint32
	EventsLost          uint32
	BuffersWritten      uint32
	LogBuffersLost      uint32
	RealTimeBuffersLost uint32
	LoggerThreadID      uintptr
	LogFileNameOffset   uint32
	LoggerNameOffset    uint32
}

// traceProps is EVENT_TRACE_PROPERTIES followed by room for the names.
type traceProps struct {
	P           eventTraceProperties
	LoggerName  [128]uint16
	LogFileName [128]uint16
}

type eventTraceLogfile struct {
	LogFileName         *uint16
	LoggerName          *uint16
	CurrentTime         int64
	BuffersRead         uint32
	ProcessTraceMode    uint32
	CurrentEvent        [88]byte
	LogfileHeader       [280]byte
	BufferCallback      uintptr
	BufferSize          uint32
	Filled              uint32
	EventsLost          uint32
	_                   uint32
	EventRecordCallback uintptr
	IsKernelTrace       uint32
	_                   uint32
	Context             uintptr
}

type eventDescriptor struct {
	ID      uint16
	Version uint8
	Channel uint8
	Level   uint8
	Opcode  uint8
	Task    uint16
	Keyword uint64
}

type eventHeader struct {
	Size            uint16
	HeaderType      uint16
	Flags           uint16
	EventProperty   uint16
	ThreadID        uint32
	ProcessID       uint32
	TimeStamp       int64
	ProviderID      guid
	EventDescriptor eventDescriptor
	ProcessorTime   uint64
	ActivityID      guid
}

type eventRecord struct {
	EventHeader       eventHeader
	BufferContext     uint32
	ExtendedDataCount uint16
	UserDataLength    uint16
	ExtendedData      uintptr
	UserData          uintptr
	UserContext       uintptr
}

type enableTraceParameters struct {
	Version          uint32
	EnableProperty   uint32
	ControlFlags     uint32
	SourceID         guid
	EnableFilterDesc uintptr
	FilterDescCount  uint32
}

type eventFilterDescriptor struct {
	Ptr  uint64
	Size uint32
	Type uint32
}

const (
	eventHeaderFlag32Bit = 0x0020

	wnodeFlagTracedGUID          = 0x00020000
	eventTraceRealTimeMode       = 0x00000100
	eventTraceNoPerProcessorBuf  = 0x10000000
	eventTraceControlStop        = 1
	eventControlCodeEnable       = 1
	processTraceModeRealTime     = 0x00000100
	processTraceModeRawTimestamp = 0x00001000
	processTraceModeEventRecord  = 0x10000000
	eventFilterTypeEventID       = 0x80000200
	enableTraceParamsVersion2    = 2
	eventEnablePropIgnoreKeyword0 = 0x10

	// event ids
	idDXGIPresentStart    = 0x2A
	idDXGIPresentMPOStart = 0x37
	idD3D9PresentStart    = 0x01
	idDxgkPresentHistoryStart = 0xAB

	dxgiPresentTest = 0x1
	presentModelRedirectedGDI = 1
)

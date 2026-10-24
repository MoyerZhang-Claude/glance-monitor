package fps

import (
	"runtime"
	"testing"
	"unsafe"
)

func TestETWStructSizes(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("layouts are for x64")
	}
	check := func(name string, got, want uintptr) {
		if got != want {
			t.Errorf("%s: size %d, want %d", name, got, want)
		}
	}
	check("WNODE_HEADER", unsafe.Sizeof(wnodeHeader{}), 48)
	check("EVENT_TRACE_PROPERTIES", unsafe.Sizeof(eventTraceProperties{}), 120)
	check("EVENT_TRACE_LOGFILEW", unsafe.Sizeof(eventTraceLogfile{}), 448)
	check("EVENT_HEADER", unsafe.Sizeof(eventHeader{}), 80)
	check("EVENT_RECORD", unsafe.Sizeof(eventRecord{}), 112)
	check("ENABLE_TRACE_PARAMETERS", unsafe.Sizeof(enableTraceParameters{}), 48)
	check("EVENT_FILTER_DESCRIPTOR", unsafe.Sizeof(eventFilterDescriptor{}), 16)

	var lf eventTraceLogfile
	check("LogFile.CurrentEvent", unsafe.Offsetof(lf.CurrentEvent), 32)
	check("LogFile.LogfileHeader", unsafe.Offsetof(lf.LogfileHeader), 120)
	check("LogFile.BufferCallback", unsafe.Offsetof(lf.BufferCallback), 400)
	check("LogFile.EventRecordCallback", unsafe.Offsetof(lf.EventRecordCallback), 424)
	check("LogFile.Context", unsafe.Offsetof(lf.Context), 440)
	var p eventTraceProperties
	check("Props.LoggerThreadID", unsafe.Offsetof(p.LoggerThreadID), 104)
	check("Props.LoggerNameOffset", unsafe.Offsetof(p.LoggerNameOffset), 116)
	var h eventHeader
	check("Header.EventDescriptor", unsafe.Offsetof(h.EventDescriptor), 40)
	check("Header.TimeStamp", unsafe.Offsetof(h.TimeStamp), 16)
	var r eventRecord
	check("Record.UserData", unsafe.Offsetof(r.UserData), 96)
	var e enableTraceParameters
	check("Enable.SourceID", unsafe.Offsetof(e.SourceID), 12)
	check("Enable.EnableFilterDesc", unsafe.Offsetof(e.EnableFilterDesc), 32)
}

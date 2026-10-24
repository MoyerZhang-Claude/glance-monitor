//go:build windows

package w32

import "syscall"

type (
	HWND    = uintptr
	HANDLE  = uintptr
	HDC     = uintptr
	HICON   = uintptr
	HMENU   = uintptr
	HMONITOR = uintptr
)

const (
	WM_NULL            = 0x0000
	WM_CREATE          = 0x0001
	WM_DESTROY         = 0x0002
	WM_MOVE            = 0x0003
	WM_CLOSE           = 0x0010
	WM_QUERYENDSESSION = 0x0011
	WM_QUIT            = 0x0012
	WM_ENDSESSION      = 0x0016
	WM_SETTINGCHANGE   = 0x001A
	WM_SETCURSOR       = 0x0020
	WM_MOUSEACTIVATE   = 0x0021
	WM_WINDOWPOSCHANGING = 0x0046
	WM_CONTEXTMENU     = 0x007B
	WM_DISPLAYCHANGE   = 0x007E
	WM_NCHITTEST       = 0x0084
	WM_KEYDOWN         = 0x0100
	WM_COMMAND         = 0x0111
	WM_TIMER           = 0x0113
	WM_MOUSEMOVE       = 0x0200
	WM_LBUTTONDOWN     = 0x0201
	WM_LBUTTONUP       = 0x0202
	WM_LBUTTONDBLCLK   = 0x0203
	WM_RBUTTONDOWN     = 0x0204
	WM_RBUTTONUP       = 0x0205
	WM_MOUSELEAVE      = 0x02A3
	WM_MOVING          = 0x0216
	WM_ENTERSIZEMOVE   = 0x0231
	WM_EXITSIZEMOVE    = 0x0232
	WM_DPICHANGED      = 0x02E0
	WM_HOTKEY          = 0x0312
	WM_POWERBROADCAST  = 0x0218
	WM_USER            = 0x0400
	WM_APP             = 0x8000
	WM_NCLBUTTONDBLCLK = 0x00A3
	WM_NCRBUTTONUP     = 0x00A5
	WM_NCRBUTTONDOWN   = 0x00A4
	WM_NCLBUTTONDOWN   = 0x00A1

	NIN_SELECT     = WM_USER + 0
	NIN_KEYSELECT  = WM_USER + 1
	NIN_POPUPOPEN  = WM_USER + 6
	NIN_POPUPCLOSE = WM_USER + 7

	HTCLIENT  = 1
	HTCAPTION = 2
	HTTRANSPARENT = ^uintptr(0) // -1

	WS_POPUP          = 0x80000000
	WS_VISIBLE        = 0x10000000
	WS_EX_TOPMOST     = 0x00000008
	WS_EX_TRANSPARENT = 0x00000020
	WS_EX_TOOLWINDOW  = 0x00000080
	WS_EX_LAYERED     = 0x00080000
	WS_EX_NOACTIVATE  = 0x08000000

	CS_DBLCLKS = 0x0008

	GWL_EXSTYLE = -20

	SW_HIDE           = 0
	SW_SHOWNOACTIVATE = 4
	SW_SHOWNA         = 8

	SWP_NOSIZE       = 0x0001
	SWP_NOMOVE       = 0x0002
	SWP_NOZORDER     = 0x0004
	SWP_NOACTIVATE   = 0x0010
	SWP_FRAMECHANGED = 0x0020
	SWP_SHOWWINDOW   = 0x0040
	SWP_NOOWNERZORDER = 0x0200
	SWP_NOSENDCHANGING = 0x0400

	HWND_TOPMOST   = ^uintptr(0) // -1
	HWND_NOTOPMOST = ^uintptr(1) // -2

	ULW_ALPHA     = 0x00000002
	AC_SRC_OVER   = 0x00
	AC_SRC_ALPHA  = 0x01

	MONITOR_DEFAULTTONULL    = 0
	MONITOR_DEFAULTTOPRIMARY = 1
	MONITOR_DEFAULTTONEAREST = 2

	MF_STRING    = 0x0000
	MF_GRAYED    = 0x0001
	MF_CHECKED   = 0x0008
	MF_SEPARATOR = 0x0800
	MF_POPUP     = 0x0010

	TPM_RIGHTBUTTON = 0x0002
	TPM_RETURNCMD   = 0x0100
	TPM_NONOTIFY    = 0x0080
	TPM_BOTTOMALIGN = 0x0020
	TPM_RIGHTALIGN  = 0x0008

	IMAGE_ICON      = 1
	LR_DEFAULTCOLOR = 0
	LR_SHARED       = 0x8000

	IDC_ARROW   = 32512
	IDC_SIZEALL = 32646

	MSGFLT_ALLOW = 1

	EVENT_SYSTEM_FOREGROUND = 0x0003
	WINEVENT_OUTOFCONTEXT   = 0x0000
	WINEVENT_SKIPOWNPROCESS = 0x0002

	MOD_ALT      = 0x0001
	MOD_CONTROL  = 0x0002
	MOD_SHIFT    = 0x0004
	MOD_WIN      = 0x0008
	MOD_NOREPEAT = 0x4000

	SM_CXSCREEN = 0
	SM_CYSCREEN = 1
	SM_CXSMICON = 49
	SM_CYSMICON = 50

	WDA_NONE               = 0
	WDA_EXCLUDEFROMCAPTURE = 0x11

	MDT_EFFECTIVE_DPI = 0

	MB_OK              = 0
	MB_ICONINFORMATION = 0x40
	MB_ICONWARNING     = 0x30
	MB_ICONERROR       = 0x10
	MB_YESNO           = 0x04
	MB_SETFOREGROUND   = 0x10000
	MB_TOPMOST         = 0x40000
	IDYES              = 6

	GA_ROOT      = 2
	GA_ROOTOWNER = 3

	VK_SHIFT   = 0x10
	VK_CONTROL = 0x11
	VK_MENU    = 0x12
	VK_ESCAPE  = 0x1B
	VK_RETURN  = 0x0D

	TME_LEAVE = 0x00000002

	PBT_APMRESUMEAUTOMATIC = 0x0012
	PBT_APMRESUMESUSPEND   = 0x0007

	DWMWA_CLOAKED = 14

	INFINITE = 0xFFFFFFFF
)

// Shell_NotifyIcon
const (
	NIM_ADD        = 0
	NIM_MODIFY     = 1
	NIM_DELETE     = 2
	NIM_SETVERSION = 4

	NIF_MESSAGE = 0x01
	NIF_ICON    = 0x02
	NIF_TIP     = 0x04
	NIF_STATE   = 0x08
	NIF_INFO    = 0x10
	NIF_SHOWTIP = 0x80

	NOTIFYICON_VERSION_4 = 4
)

type POINT struct{ X, Y int32 }

type RECT struct{ Left, Top, Right, Bottom int32 }

func (r RECT) W() int32 { return r.Right - r.Left }
func (r RECT) H() int32 { return r.Bottom - r.Top }
func (r RECT) Contains(p POINT) bool {
	return p.X >= r.Left && p.X < r.Right && p.Y >= r.Top && p.Y < r.Bottom
}

type SIZE struct{ CX, CY int32 }

type MSG struct {
	Hwnd    HWND
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
	_       uint32
}

type WNDCLASSEXW struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   HANDLE
	Icon       HICON
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     HICON
}

type BLENDFUNCTION struct {
	BlendOp             byte
	BlendFlags          byte
	SourceConstantAlpha byte
	AlphaFormat         byte
}

type MONITORINFO struct {
	Size    uint32
	Monitor RECT
	Work    RECT
	Flags   uint32
}

type MONITORINFOEXW struct {
	MONITORINFO
	Device [32]uint16
}

type BITMAPINFOHEADER struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type BITMAPINFO struct {
	Header BITMAPINFOHEADER
	Colors [1]uint32
}

type GUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

type NOTIFYICONDATAW struct {
	Size            uint32
	Wnd             HWND
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            HICON
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GuidItem        GUID
	BalloonIcon     HICON
}

type NOTIFYICONIDENTIFIER struct {
	Size     uint32
	Wnd      HWND
	ID       uint32
	GuidItem GUID
}

type MEMORYSTATUSEX struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

type FILETIME struct{ Low, High uint32 }

func (f FILETIME) U64() uint64 { return uint64(f.High)<<32 | uint64(f.Low) }

type PROCESSENTRY32W struct {
	Size            uint32
	Usage           uint32
	ProcessID       uint32
	DefaultHeapID   uintptr
	ModuleID        uint32
	Threads         uint32
	ParentProcessID uint32
	PriClassBase    int32
	Flags           uint32
	ExeFile         [260]uint16
}

type SYSTEM_POWER_STATUS struct {
	ACLineStatus        byte
	BatteryFlag         byte
	BatteryLifePercent  byte
	SystemStatusFlag    byte
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

type SHELLEXECUTEINFOW struct {
	Size         uint32
	Mask         uint32
	Wnd          HWND
	Verb         *uint16
	File         *uint16
	Parameters   *uint16
	Directory    *uint16
	Show         int32
	InstApp      uintptr
	IDList       uintptr
	Class        *uint16
	KeyClass     uintptr
	HotKey       uint32
	IconOrMonitor uintptr
	Process      HANDLE
}

type STARTUPINFOW struct {
	Cb            uint32
	_             *uint16
	Desktop       *uint16
	Title         *uint16
	X, Y          uint32
	XSize, YSize  uint32
	XCountChars   uint32
	YCountChars   uint32
	FillAttribute uint32
	Flags         uint32
	ShowWindow    uint16
	_             uint16
	_             *byte
	StdInput      HANDLE
	StdOutput     HANDLE
	StdError      HANDLE
}

type PROCESS_INFORMATION struct {
	Process   HANDLE
	Thread    HANDLE
	ProcessID uint32
	ThreadID  uint32
}

type TRACKMOUSEEVENT struct {
	Size      uint32
	Flags     uint32
	Track     HWND
	HoverTime uint32
}

// LOGFONTW is used for font enumeration only.
type LOGFONTW struct {
	Height         int32
	Width          int32
	Escapement     int32
	Orientation    int32
	Weight         int32
	Italic         byte
	Underline      byte
	StrikeOut      byte
	CharSet        byte
	OutPrecision   byte
	ClipPrecision  byte
	Quality        byte
	PitchAndFamily byte
	FaceName       [32]uint16
}

func LOWORD(v uintptr) uint16 { return uint16(v & 0xFFFF) }
func HIWORD(v uintptr) uint16 { return uint16((v >> 16) & 0xFFFF) }
func GET_X(v uintptr) int32  { return int32(int16(v & 0xFFFF)) }
func GET_Y(v uintptr) int32  { return int32(int16((v >> 16) & 0xFFFF)) }

// Errno helpers
func IsErr(err error, code syscall.Errno) bool {
	e, ok := err.(syscall.Errno)
	return ok && e == code
}

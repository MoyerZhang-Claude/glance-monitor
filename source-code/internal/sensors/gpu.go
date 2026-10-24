//go:build windows

package sensors

import (
	"log"
	"math"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"glance/internal/data"
	"glance/internal/w32"
)

// ---------------- NVML ----------------

type nvml struct {
	dll   *syscall.DLL
	procs map[string]*syscall.Proc
	dev   uintptr
	name  string
	bus   uint32
	slot  uint32
	powerOK, energyOK bool
	lastEnergy uint64
	lastE      time.Time
	powerMethod string  // nvml | field | energy | nvapi | none
	probedAt    time.Time
	powerField  uint32
	limitW      float64 // power limit in W when known (for percent-based fallback)
	nvapi       *nvAPI
}

func (n *nvml) proc(name string) *syscall.Proc {
	if p, ok := n.procs[name]; ok {
		return p
	}
	p, err := n.dll.FindProc(name)
	if err != nil {
		p = nil
	}
	n.procs[name] = p
	return p
}

func (n *nvml) call(name string, args ...uintptr) int {
	p := n.proc(name)
	if p == nil {
		return -1
	}
	r, _, _ := p.Call(args...)
	return int(int32(r))
}

func loadNVML() (*nvml, error) {
	var dll *syscall.DLL
	var err error
	dll, err = w32.LoadDLL("nvml.dll", false)
	if err != nil {
		pf := envOr("ProgramW6432", envOr("ProgramFiles", `C:\Program Files`))
		dll, err = w32.LoadDLL(pf+`\NVIDIA Corporation\NVSMI\nvml.dll`, true)
		if err != nil {
			return nil, err
		}
	}
	n := &nvml{dll: dll, procs: map[string]*syscall.Proc{}}
	if r := n.call("nvmlInit_v2"); r != 0 {
		dll.Release()
		return nil, syscall.Errno(r)
	}
	var count uint32
	if n.call("nvmlDeviceGetCount_v2", uintptr(unsafe.Pointer(&count))) != 0 || count == 0 {
		n.close()
		return nil, syscall.ERROR_NOT_FOUND
	}
	if n.call("nvmlDeviceGetHandleByIndex_v2", 0, uintptr(unsafe.Pointer(&n.dev))) != 0 {
		n.close()
		return nil, syscall.ERROR_NOT_FOUND
	}
	buf := make([]byte, 96)
	if n.call("nvmlDeviceGetName", n.dev, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))) == 0 {
		n.name = cString(buf)
	}
	var pci struct {
		BusIDLegacy [16]byte
		Domain      uint32
		Bus         uint32
		Device      uint32
		DeviceID    uint32
		SubsysID    uint32
		BusID       [32]byte
	}
	if n.call("nvmlDeviceGetPciInfo_v3", n.dev, uintptr(unsafe.Pointer(&pci))) == 0 {
		n.bus, n.slot = pci.Bus, pci.Device
	}
	n.powerOK, n.energyOK = true, true
	n.probePower()
	return n, nil
}

type nvmlFieldValue struct {
	FieldID   uint32
	ScopeID   uint32
	Timestamp int64
	Latency   int64
	ValueType uint32
	Ret       uint32
	Value     uint64
}

func (n *nvml) field(id uint32) (float64, int) {
	fv := []nvmlFieldValue{{FieldID: id}}
	r := n.call("nvmlDeviceGetFieldValues", n.dev, 1, uintptr(unsafe.Pointer(&fv[0])))
	if r != 0 {
		return 0, r
	}
	if fv[0].Ret != 0 {
		return 0, int(fv[0].Ret)
	}
	switch fv[0].ValueType {
	case 0:
		return math.Float64frombits(fv[0].Value), 0
	case 1, 5:
		return float64(uint32(fv[0].Value)), 0
	default:
		return float64(fv[0].Value), 0
	}
}

// probePower finds a working way to read board power. Laptop GPUs often
// report NOT_SUPPORTED for the classic NVML call.
// reprobePower quietly retries the watt sources after a failed probe. On
// this laptop the driver has reported NOT_SUPPORTED until a reboot, then
// worked; retrying lets GPU power come back without restarting Glance.
func (n *nvml) reprobePower() {
	n.probedAt = time.Now()
	var mw uint32
	if n.call("nvmlDeviceGetPowerUsage", n.dev, uintptr(unsafe.Pointer(&mw))) == 0 && mw > 0 {
		n.powerMethod = "nvml"
	} else if v, r := n.field(186); r == 0 && v > 0 {
		n.powerMethod, n.powerField = "field", 186
	} else if v, r := n.field(185); r == 0 && v > 0 {
		n.powerMethod, n.powerField = "field", 185
	} else {
		var mj uint64
		if n.call("nvmlDeviceGetTotalEnergyConsumption", n.dev, uintptr(unsafe.Pointer(&mj))) == 0 && mj > 0 {
			n.powerMethod = "energy"
		}
	}
	if n.powerMethod != "none" {
		log.Printf("GPU power recovered: method %s", n.powerMethod)
	}
}

func (n *nvml) probePower() {
	n.probedAt = time.Now()
	var mw uint32
	r := n.call("nvmlDeviceGetPowerUsage", n.dev, uintptr(unsafe.Pointer(&mw)))
	log.Printf("NVML power usage: ret=%d value=%dmW", r, mw)
	if r == 0 && mw > 0 {
		n.powerMethod = "nvml"
	}
	for _, id := range []uint32{186, 185} {
		v, r := n.field(id)
		log.Printf("NVML field %d (power): ret=%d value=%.0f", id, r, v)
		if r == 0 && v > 0 && n.powerMethod == "" {
			n.powerMethod, n.powerField = "field", id
		}
	}
	var mj uint64
	r = n.call("nvmlDeviceGetTotalEnergyConsumption", n.dev, uintptr(unsafe.Pointer(&mj)))
	log.Printf("NVML total energy: ret=%d value=%dmJ", r, mj)
	if r == 0 && mj > 0 && n.powerMethod == "" {
		n.powerMethod = "energy"
	}
	for _, f := range []string{"nvmlDeviceGetEnforcedPowerLimit", "nvmlDeviceGetPowerManagementLimit", "nvmlDeviceGetPowerManagementDefaultLimit"} {
		var lim uint32
		r := n.call(f, n.dev, uintptr(unsafe.Pointer(&lim)))
		log.Printf("NVML %s: ret=%d value=%dmW", f, r, lim)
		if r == 0 && lim > 0 && n.limitW == 0 {
			n.limitW = float64(lim) / 1000
		}
	}
	for _, id := range []uint32{190, 189, 188} {
		v, r := n.field(id)
		log.Printf("NVML field %d (limit): ret=%d value=%.0f", id, r, v)
		if r == 0 && v > 0 && n.limitW == 0 {
			n.limitW = v / 1000
		}
	}
	n.nvapi = loadNVAPI(n.bus)
	if n.nvapi != nil {
		pct, st := n.nvapi.powerPercent()
		log.Printf("NVAPI power topology: status=%d value=%.1f (limit known: %.0fW; status -2 => value is entry count)", st, pct, n.limitW)
		if st == 0 && n.powerMethod == "" {
			n.powerMethod = "nvapi"
		}
	}
	if n.powerMethod == "" {
		n.powerMethod = "none"
	}
	log.Printf("GPU power method: %s", n.powerMethod)
}

func (n *nvml) close() {
	if n == nil || n.dll == nil {
		return
	}
	n.call("nvmlShutdown")
	n.dll.Release()
	n.dll = nil
}

func (n *nvml) read(s *data.Snapshot) {
	var u32 uint32
	if n.call("nvmlDeviceGetTemperature", n.dev, 0, uintptr(unsafe.Pointer(&u32))) == 0 {
		s.Set(data.GPUTemp, float64(u32))
		s.Readings = append(s.Readings, data.Reading{Group: "GPU", Name: "核心温度", Value: float64(u32), Unit: "°C"})
	}
	var util struct{ GPU, Mem uint32 }
	if n.call("nvmlDeviceGetUtilizationRates", n.dev, uintptr(unsafe.Pointer(&util))) == 0 {
		s.Set(data.GPUUsage, float64(util.GPU))
		s.Readings = append(s.Readings, data.Reading{Group: "GPU", Name: "核心占用", Value: float64(util.GPU), Unit: "%"})
		s.Readings = append(s.Readings, data.Reading{Group: "GPU", Name: "显存控制器占用", Value: float64(util.Mem), Unit: "%"})
	}
	if n.call("nvmlDeviceGetClockInfo", n.dev, 0, uintptr(unsafe.Pointer(&u32))) == 0 {
		s.Set(data.GPUClock, float64(u32))
		s.Readings = append(s.Readings, data.Reading{Group: "GPU", Name: "核心频率", Value: float64(u32), Unit: "MHz"})
	}
	if n.call("nvmlDeviceGetClockInfo", n.dev, 2, uintptr(unsafe.Pointer(&u32))) == 0 {
		s.Set(data.GPUMemClk, float64(u32))
		s.Readings = append(s.Readings, data.Reading{Group: "GPU", Name: "显存频率", Value: float64(u32), Unit: "MHz"})
	}
	switch n.powerMethod {
	case "nvml":
		if n.call("nvmlDeviceGetPowerUsage", n.dev, uintptr(unsafe.Pointer(&u32))) == 0 {
			n.setPower(s, float64(u32)/1000)
		}
	case "field":
		if v, r := n.field(n.powerField); r == 0 {
			n.setPower(s, v/1000)
		}
	case "energy":
		var mj uint64
		if n.call("nvmlDeviceGetTotalEnergyConsumption", n.dev, uintptr(unsafe.Pointer(&mj))) == 0 {
			now := time.Now()
			if !n.lastE.IsZero() && mj >= n.lastEnergy {
				if dt := now.Sub(n.lastE).Seconds(); dt > 0.05 {
					n.setPower(s, float64(mj-n.lastEnergy)/1000/dt)
				}
			}
			n.lastEnergy, n.lastE = mj, now
		}
	case "nvapi":
		if pct, st := n.nvapi.powerPercent(); st == 0 {
			s.Readings = append(s.Readings, data.Reading{Group: "GPU", Name: "功耗（占功耗墙）", Value: pct, Unit: "%"})
			s.Set("gpu.powerpct", pct)
			if n.limitW > 0 {
				n.setPower(s, pct/100*n.limitW)
			}
		}
	}
	var mem struct{ Total, Free, Used uint64 }
	if n.call("nvmlDeviceGetMemoryInfo", n.dev, uintptr(unsafe.Pointer(&mem))) == 0 && mem.Total > 0 {
		gb := float64(mem.Used) / (1 << 30)
		s.Set(data.GPUVRAM, gb)
		s.Readings = append(s.Readings, data.Reading{Group: "GPU", Name: "显存已用", Value: gb, Unit: "GB"})
		s.Readings = append(s.Readings, data.Reading{Group: "GPU", Name: "显存总量", Value: float64(mem.Total) / (1 << 30), Unit: "GB"})
	}
	if n.call("nvmlDeviceGetFanSpeed", n.dev, uintptr(unsafe.Pointer(&u32))) == 0 && u32 <= 100 {
		s.Set(data.GPUFan, float64(u32))
		s.Readings = append(s.Readings, data.Reading{Group: "GPU", Name: "风扇", Value: float64(u32), Unit: "%"})
	}
}

func (n *nvml) setPower(s *data.Snapshot, w float64) {
	if w < 0 || w > 2000 {
		return
	}
	s.Set(data.GPUPower, w)
	s.Readings = append(s.Readings, data.Reading{Group: "GPU", Name: "功耗", Value: w, Unit: "W"})
}

func cString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

func envOr(k, def string) string {
	if v, ok := syscall.Getenv(k); ok && v != "" {
		return v
	}
	return def
}

// ---------------- NVIDIA thermal registers via PawnIO ----------------

type nvThermal struct {
	m   *pawnModule
	pci *globalMutex
	bus, slot uint32
	memOK bool
}

func newNVThermal(bus, slot uint32) *nvThermal {
	m, err := openPawnModule(modNvidia)
	if err != nil {
		return nil
	}
	return &nvThermal{m: m, pci: newGlobalMutex(`Global\Access_PCI`), bus: bus, slot: slot, memOK: true}
}

func (t *nvThermal) close() { t.m.close() }

func (t *nvThermal) read(s *data.Snapshot) {
	if !t.pci.lock(10 * time.Millisecond) {
		return
	}
	defer t.pci.unlock()
	if r, err := t.m.exec("ioctl_read_thermal_registers", []uint64{uint64(t.bus), uint64(t.slot), 0}, 6); err == nil {
		max := math.Inf(-1)
		for i, raw := range r {
			v := uint32(raw)
			if v&(1<<30) == 0 {
				continue
			}
			c := float64(v&0xFFFF) / 256
			if c <= 0 || c > 150 {
				continue
			}
			s.Readings = append(s.Readings, data.Reading{Group: "GPU", Name: "热点通道 #" + strconv.Itoa(i+1), Value: c, Unit: "°C"})
			max = math.Max(max, c)
		}
		if !math.IsInf(max, -1) {
			s.Set(data.GPUHotspot, max)
		}
	}
	if t.memOK {
		r, err := t.m.exec("ioctl_read_memory_temperatures", []uint64{uint64(t.bus), uint64(t.slot), 0}, 50)
		if err != nil || len(r) != 50 || r[1] != 48 {
			t.memOK = err == nil // keep trying only if the call itself works
			return
		}
		max := math.Inf(-1)
		for i := 0; i < 48; i++ {
			v := int32(uint32(r[i+2]))
			if v == math.MinInt32 || v <= 0 || v > 150 {
				continue
			}
			max = math.Max(max, float64(v))
		}
		if !math.IsInf(max, -1) {
			s.Set(data.GPUMemTemp, max)
			s.Readings = append(s.Readings, data.Reading{Group: "GPU", Name: "显存温度 (最高)", Value: max, Unit: "°C"})
		}
	}
}

// ---------------- device power state (don't wake a sleeping dGPU) ----------------

var (
	setupapi                              = syscall.NewLazyDLL("setupapi.dll")
	procSetupDiGetClassDevsW              = setupapi.NewProc("SetupDiGetClassDevsW")
	procSetupDiEnumDeviceInfo             = setupapi.NewProc("SetupDiEnumDeviceInfo")
	procSetupDiGetDeviceRegistryPropertyW = setupapi.NewProc("SetupDiGetDeviceRegistryPropertyW")
	procSetupDiDestroyDeviceInfoList      = setupapi.NewProc("SetupDiDestroyDeviceInfoList")
)

var guidDevclassDisplay = w32.GUID{Data1: 0x4d36e968, Data2: 0xe325, Data3: 0x11ce, Data4: [8]byte{0xbf, 0xc1, 0x08, 0x00, 0x2b, 0xe1, 0x03, 0x18}}

type spDevinfoData struct {
	Size      uint32
	ClassGuid w32.GUID
	DevInst   uint32
	Reserved  uintptr
}

type gpuPower struct {
	set  uintptr
	info spDevinfoData
	ok   bool
	Name string
}

// findGPUDevice locates the first display adapter whose hardware id
// contains vendor (e.g. "VEN_10DE").
func findGPUDevice(vendor string) *gpuPower {
	set, _, _ := procSetupDiGetClassDevsW.Call(uintptr(unsafe.Pointer(&guidDevclassDisplay)), 0, 0, 0x2 /*DIGCF_PRESENT*/)
	if set == 0 || set == w32.INVALID_HANDLE {
		return nil
	}
	for i := uint32(0); i < 16; i++ {
		var di spDevinfoData
		di.Size = uint32(unsafe.Sizeof(di))
		r, _, _ := procSetupDiEnumDeviceInfo.Call(set, uintptr(i), uintptr(unsafe.Pointer(&di)))
		if r == 0 {
			break
		}
		hw := devPropString(set, &di, 1 /*SPDRP_HARDWAREID*/)
		if strings.Contains(strings.ToUpper(hw), vendor) {
			g := &gpuPower{set: set, info: di, ok: true}
			g.Name = devPropString(set, &di, 0 /*SPDRP_DEVICEDESC*/)
			return g
		}
	}
	procSetupDiDestroyDeviceInfoList.Call(set)
	return nil
}

func devPropString(set uintptr, di *spDevinfoData, prop uint32) string {
	buf := make([]uint16, 512)
	var typ, req uint32
	r, _, _ := procSetupDiGetDeviceRegistryPropertyW.Call(set, uintptr(unsafe.Pointer(di)), uintptr(prop),
		uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)*2), uintptr(unsafe.Pointer(&req)))
	if r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

// state returns the most recent device power state: 1=D0 (on) ... 4=D3.
func (g *gpuPower) state() int {
	if g == nil || !g.ok {
		return 0
	}
	var pd struct {
		Size           uint32
		MostRecent     uint32
		Capabilities   uint32
		D1, D2, D3     uint32
		Mapping        [7]uint32
		DeepestWake    uint32
	}
	var typ, req uint32
	r, _, _ := procSetupDiGetDeviceRegistryPropertyW.Call(g.set, uintptr(unsafe.Pointer(&g.info)), 0x1E, /*SPDRP_DEVICE_POWER_DATA*/
		uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&pd)), unsafe.Sizeof(pd), uintptr(unsafe.Pointer(&req)))
	if r == 0 {
		return 0
	}
	return int(pd.MostRecent)
}

func (g *gpuPower) close() {
	if g != nil && g.set != 0 {
		procSetupDiDestroyDeviceInfoList.Call(g.set)
		g.set = 0
	}
}

// ---------------- generic GPU usage via PDH (non-NVIDIA fallback) ----------------

type gpuPDH struct {
	q    *pdhQuery
	util *pdhCounter
	mem  *pdhCounter
	primed bool
}

func newGPUPDH() *gpuPDH {
	g := &gpuPDH{q: newPdhQuery()}
	g.util = g.q.add(`\GPU Engine(*engtype_3D)\Utilization Percentage`)
	g.mem = g.q.add(`\GPU Adapter Memory(*)\Dedicated Usage`)
	g.q.collect()
	return g
}

func (g *gpuPDH) close() { g.q.close() }

func (g *gpuPDH) read(s *data.Snapshot) {
	if !g.q.collect() {
		return
	}
	if !g.primed {
		g.primed = true
		return
	}
	// instance: pid_1234_luid_0x00000000_0x0000D1F4_phys_0_eng_0_engtype_3D
	perEngine := map[string]float64{}
	for inst, v := range g.util.array() {
		i := strings.Index(inst, "luid_")
		if i < 0 {
			continue
		}
		perEngine[inst[i:]] += v
	}
	perLuid := map[string]float64{}
	for k, v := range perEngine {
		luid := k
		if j := strings.Index(k, "_phys"); j > 0 {
			luid = k[:j]
		}
		perLuid[luid] = math.Max(perLuid[luid], v)
	}
	best := -1.0
	for _, v := range perLuid {
		best = math.Max(best, v)
	}
	if best >= 0 {
		s.Set(data.GPUUsage, math.Min(100, best))
	}
	var maxMem float64
	for _, v := range g.mem.array() {
		maxMem = math.Max(maxMem, v)
	}
	if maxMem > 0 {
		s.Set(data.GPUVRAM, maxMem/(1<<30))
	}
}

//go:build windows

package sensors

import (
	"fmt"
	"math"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"glance/internal/data"
	"glance/internal/w32"
)

func cpuid(eaxArg, ecxArg uint32) (eax, ebx, ecx, edx uint32)

type cpuIdent struct {
	vendor   string // GenuineIntel / AuthenticAMD
	family   uint32
	model    uint32
	stepping uint32
	brand    string
	maxLeaf  uint32
}

func identifyCPU() cpuIdent {
	var id cpuIdent
	max, b, c, d := cpuid(0, 0)
	id.maxLeaf = max
	v := make([]byte, 12)
	for i := 0; i < 4; i++ {
		v[i] = byte(b >> (8 * i))
		v[4+i] = byte(d >> (8 * i))
		v[8+i] = byte(c >> (8 * i))
	}
	id.vendor = string(v)
	if max >= 1 {
		a, _, _, _ := cpuid(1, 0)
		base := (a >> 8) & 0xF
		ext := (a >> 20) & 0xFF
		id.family = base
		if base == 0xF {
			id.family = base + ext
		}
		id.model = (a >> 4) & 0xF
		if base == 0x6 || base == 0xF {
			id.model |= ((a >> 16) & 0xF) << 4
		}
		id.stepping = a & 0xF
	}
	id.brand = strings.TrimSpace(regStr(syscall.HKEY_LOCAL_MACHINE, `HARDWARE\DESCRIPTION\System\CentralProcessor\0`, "ProcessorNameString"))
	if id.brand == "" {
		if xm, _, _, _ := cpuid(0x80000000, 0); xm >= 0x80000004 {
			var sb strings.Builder
			for leaf := uint32(0x80000002); leaf <= 0x80000004; leaf++ {
				a, b, c, d := cpuid(leaf, 0)
				for _, r := range []uint32{a, b, c, d} {
					for i := 0; i < 4; i++ {
						if ch := byte(r >> (8 * i)); ch != 0 {
							sb.WriteByte(ch)
						}
					}
				}
			}
			id.brand = strings.TrimSpace(sb.String())
		}
	}
	return id
}

// prettyCPUName shortens marketing strings for display.
func prettyCPUName(s string) string {
	s = strings.ReplaceAll(s, "(R)", "")
	s = strings.ReplaceAll(s, "(TM)", "")
	s = strings.ReplaceAll(s, "(tm)", "")
	s = strings.Replace(s, " Processor", "", 1)
	s = strings.Replace(s, " CPU", "", 1)
	if i := strings.Index(s, " @ "); i > 0 {
		s = s[:i]
	}
	if i := strings.Index(s, "-Core"); i > 0 {
		// "AMD Ryzen 9 7945HX 16-Core" -> drop core count suffix
		if j := strings.LastIndex(s[:i], " "); j > 0 {
			s = s[:j]
		}
	}
	return strings.Join(strings.Fields(s), " ")
}

// ---------------- generic CPU (PDH) ----------------

type cpuGeneric struct {
	q        *pdhQuery
	util     *pdhCounter
	perf     *pdhCounter
	freq     *pdhCounter
	thermal  *pdhCounter
	pCores   map[int]bool // logical processors on the fastest core type
	hybrid   bool
	lastIdle, lastKernel, lastUser uint64
	primed   bool
	lastCollect time.Time
	lastClock   float64
}

func newCPUGeneric() *cpuGeneric {
	g := &cpuGeneric{q: newPdhQuery(), pCores: map[int]bool{}}
	g.util = g.q.add(`\Processor Information(_Total)\% Processor Utility`)
	g.perf = g.q.add(`\Processor Information(*)\% Processor Performance`)
	g.freq = g.q.add(`\Processor Information(*)\Processor Frequency`)
	g.thermal = g.q.add(`\Thermal Zone Information(*)\Temperature`)
	g.q.collect()
	cores := w32.Cores()
	maxEff := 0
	for _, c := range cores {
		if c.Efficiency > maxEff {
			maxEff = c.Efficiency
		}
	}
	for _, c := range cores {
		if c.Efficiency != maxEff {
			g.hybrid = true
			continue
		}
		for i := 0; i < 64; i++ {
			if c.Mask&(1<<uint(i)) != 0 {
				g.pCores[i] = true
			}
		}
	}
	g.lastIdle, g.lastKernel, g.lastUser, _ = w32.GetSystemTimes()
	g.lastCollect = time.Now()
	return g
}

func (g *cpuGeneric) close() { g.q.close() }

// read fills usage and clock; returns the thermal-zone fallback temperature.
func (g *cpuGeneric) read(s *data.Snapshot, clockMode string) (thermalC float64, thermalOK bool) {
	collected := g.q.collect()
	now := time.Now()
	window := now.Sub(g.lastCollect).Seconds()
	g.lastCollect = now
	// "% Processor Performance" over very short windows (startup, a settings
	// change kicking an early sample) can spike far above the real boost
	// clock; only trust windows of at least ~0.45 s and otherwise repeat the
	// last good value.
	clockOK := window >= 0.45

	// Usage: Task Manager's "Processor Utility" when available.
	usageOK := false
	if collected && g.primed {
		if v, ok := g.util.value(); ok {
			s.Set(data.CPUUsage, math.Min(100, math.Max(0, v)))
			usageOK = true
		}
	}
	idle, kernel, user, ok := w32.GetSystemTimes()
	if ok && !usageOK {
		di := float64(idle - g.lastIdle)
		dt := float64((kernel - g.lastKernel) + (user - g.lastUser))
		if dt > 0 {
			s.Set(data.CPUUsage, math.Min(100, math.Max(0, (dt-di)/dt*100)))
		}
	}
	g.lastIdle, g.lastKernel, g.lastUser = idle, kernel, user

	if collected && g.primed && !clockOK && g.lastClock > 0 {
		s.Set(data.CPUClock, g.lastClock)
	}
	if collected && g.primed && clockOK {
		perf := g.perf.array()
		freq := g.freq.array()
		var max, sum, pmax float64
		n := 0
		for inst, p := range perf {
			if strings.Contains(inst, "_Total") {
				continue
			}
			f, ok := freq[inst]
			if !ok || f <= 0 {
				continue
			}
			mhz := f * p / 100
			if mhz <= 0 {
				continue
			}
			s.Readings = append(s.Readings, data.Reading{Group: "CPU", Name: "频率 #" + inst, Value: mhz, Unit: "MHz"})
			sum += mhz
			n++
			if mhz > max {
				max = mhz
			}
			if idx := lpIndex(inst); idx >= 0 && g.pCores[idx] && mhz > pmax {
				pmax = mhz
			}
		}
		if n > 0 {
			clock := max
			switch clockMode {
			case "avg":
				clock = sum / float64(n)
			case "pmax":
				if pmax > 0 {
					clock = pmax
				}
			}
			s.Set(data.CPUClock, clock)
			g.lastClock = clock
		}
		if zones := g.thermal.array(); len(zones) > 0 {
			for name, k := range zones {
				c := k - 273.15
				if k > 200 && c > 0 && c < 150 {
					s.Readings = append(s.Readings, data.Reading{Group: "ACPI", Name: "温度区 " + name, Value: c, Unit: "°C"})
					if c > thermalC {
						thermalC, thermalOK = c, true
					}
				}
			}
		}
	}
	g.primed = true
	return
}

func lpIndex(inst string) int {
	// "0,13" -> 13 (group 0 only)
	parts := strings.Split(inst, ",")
	if len(parts) != 2 || parts[0] != "0" {
		return -1
	}
	n, err := strconv.Atoi(parts[1])
	if err != nil {
		return -1
	}
	return n
}

// ---------------- Intel (MSR via PawnIO) ----------------

const (
	msrPlatformInfo     = 0xCE
	msrPerfStatus       = 0x198
	msrThermStatus      = 0x19C
	msrTemperatureTarget = 0x1A2
	msrPkgThermStatus   = 0x1B1
	msrRaplPowerUnit    = 0x606
	msrPkgEnergyStatus  = 0x611
	msrPP0EnergyStatus  = 0x639
)

type intelCPU struct {
	m          *pawnModule
	tjMax      float64
	hasPkgTemp bool
	hasDTS     bool
	energyUnit float64
	lastEnergy uint32
	lastE      time.Time
	cores      []w32.CoreInfo
}

func newIntelCPU(id cpuIdent) (*intelCPU, error) {
	m, err := openPawnModule(modIntelMSR)
	if err != nil {
		return nil, err
	}
	c := &intelCPU{m: m, cores: w32.Cores()}
	if id.maxLeaf >= 6 {
		a, _, _, _ := cpuid(6, 0)
		c.hasDTS = a&1 != 0
		c.hasPkgTemp = a&(1<<6) != 0
	}
	c.tjMax = 100
	if v, ok := m.readMSR(msrTemperatureTarget); ok {
		if t := float64((v >> 16) & 0xFF); t >= 70 && t <= 130 {
			c.tjMax = t
		}
	}
	if v, ok := m.readMSR(msrRaplPowerUnit); ok {
		esu := (v >> 8) & 0x1F
		c.energyUnit = 1.0 / float64(uint64(1)<<esu)
		if e, ok := m.readMSR(msrPkgEnergyStatus); ok {
			c.lastEnergy = uint32(e)
			c.lastE = time.Now()
		} else {
			c.energyUnit = 0
		}
	}
	if !c.hasPkgTemp && !c.hasDTS && c.energyUnit == 0 {
		m.close()
		return nil, fmt.Errorf("CPU 不支持温度/能耗寄存器")
	}
	return c, nil
}

func (c *intelCPU) close() { c.m.close() }

// read must run on a goroutine locked to its OS thread (affinity changes).
func (c *intelCPU) read(s *data.Snapshot, perCore bool) {
	if c.hasPkgTemp {
		if v, ok := c.m.readMSR(msrPkgThermStatus); ok && v&0x80000000 != 0 {
			t := c.tjMax - float64((v>>16)&0x7F)
			s.Set(data.CPUTemp, t)
			s.Readings = append(s.Readings, data.Reading{Group: "CPU", Name: "封装温度 (Package)", Value: t, Unit: "°C"})
		}
	}
	if perCore && c.hasDTS && len(c.cores) > 0 {
		maxT := -1.0
		var pMax, eMax float64
		maxEff := 0
		for _, core := range c.cores {
			if core.Efficiency > maxEff {
				maxEff = core.Efficiency
			}
		}
		for i, core := range c.cores {
			lowest := core.Mask & -core.Mask
			prev := w32.SetThreadAffinity(uintptr(lowest))
			if prev == 0 {
				continue
			}
			v, ok := c.m.readMSR(msrThermStatus)
			w32.SetThreadAffinity(prev)
			if !ok || v&0x80000000 == 0 {
				continue
			}
			t := c.tjMax - float64((v>>16)&0x7F)
			kind := "P"
			if core.Efficiency < maxEff {
				kind = "E"
				eMax = math.Max(eMax, t)
			} else {
				pMax = math.Max(pMax, t)
			}
			s.Readings = append(s.Readings, data.Reading{Group: "CPU", Name: fmt.Sprintf("核心 #%d (%s)", i+1, kind), Value: t, Unit: "°C"})
			if t > maxT {
				maxT = t
			}
		}
		if maxT >= 0 {
			s.Set(data.CPUTempMax, maxT)
			if _, ok := s.Get(data.CPUTemp); !ok {
				s.Set(data.CPUTemp, maxT)
			}
		}
		_ = pMax
		_ = eMax
	}
	if c.energyUnit > 0 {
		if v, ok := c.m.readMSR(msrPkgEnergyStatus); ok {
			now := time.Now()
			e := uint32(v)
			dt := now.Sub(c.lastE).Seconds()
			if dt > 0.05 {
				w := float64(e-c.lastEnergy) * c.energyUnit / dt
				if w >= 0 && w < 1000 {
					s.Set(data.CPUPower, w)
					s.Readings = append(s.Readings, data.Reading{Group: "CPU", Name: "封装功耗 (Package)", Value: w, Unit: "W"})
				}
				c.lastEnergy, c.lastE = e, now
			}
		}
	}
}

// ---------------- AMD Zen (SMN + MSR via PawnIO) ----------------

const (
	amdThmTconCurTmp = 0x00059800
	amdMsrPwrUnit    = 0xC0010299
	amdMsrPkgEnergy  = 0xC001029B
)

type amdCPU struct {
	m          *pawnModule
	pci        *globalMutex
	energyUnit float64
	lastEnergy uint32
	lastE      time.Time
	offset     float64
}

func newAMDCPU(id cpuIdent) (*amdCPU, error) {
	if id.family < 0x17 {
		return nil, fmt.Errorf("不支持的 AMD CPU 家族 %Xh", id.family)
	}
	m, err := openPawnModule(modAMD17)
	if err != nil {
		return nil, err
	}
	c := &amdCPU{m: m, pci: newGlobalMutex(`Global\Access_PCI`)}
	switch {
	case strings.Contains(id.brand, "1600X"), strings.Contains(id.brand, "1700X"), strings.Contains(id.brand, "1800X"):
		c.offset = -20
	case strings.Contains(id.brand, "2700X"):
		c.offset = -10
	case strings.Contains(id.brand, "Threadripper 19"), strings.Contains(id.brand, "Threadripper 29"):
		c.offset = -27
	}
	if v, ok := m.readMSR(amdMsrPwrUnit); ok {
		esu := (v >> 8) & 0x1F
		c.energyUnit = math.Pow(0.5, float64(esu))
		if e, ok := m.readMSR(amdMsrPkgEnergy); ok {
			c.lastEnergy, c.lastE = uint32(e), time.Now()
		}
	}
	return c, nil
}

func (c *amdCPU) close() { c.m.close() }

func (c *amdCPU) read(s *data.Snapshot) {
	if c.pci.lock(10 * time.Millisecond) {
		r, err := c.m.exec("ioctl_read_smn", []uint64{amdThmTconCurTmp}, 1)
		c.pci.unlock()
		if err == nil && len(r) == 1 {
			raw := uint32(r[0])
			t := float64(raw>>21) * 0.125
			if raw&0x80000 != 0 || raw&0x30000 == 0x30000 {
				t -= 49
			}
			t += c.offset
			if t > 0 && t < 150 {
				s.Set(data.CPUTemp, t)
				s.Readings = append(s.Readings, data.Reading{Group: "CPU", Name: "Tctl/Tdie", Value: t, Unit: "°C"})
			}
		}
	}
	if c.energyUnit > 0 {
		if v, ok := c.m.readMSR(amdMsrPkgEnergy); ok {
			now := time.Now()
			e := uint32(v)
			if dt := now.Sub(c.lastE).Seconds(); dt > 0.05 {
				w := float64(e-c.lastEnergy) * c.energyUnit / dt
				if w >= 0 && w < 1000 {
					s.Set(data.CPUPower, w)
					s.Readings = append(s.Readings, data.Reading{Group: "CPU", Name: "封装功耗 (Package)", Value: w, Unit: "W"})
				}
				c.lastEnergy, c.lastE = e, now
			}
		}
	}
}

// lockThread pins the calling goroutine to its OS thread (needed before
// changing thread affinity for per-core MSR reads).
func lockThread() { runtime.LockOSThread() }

func syscallGetpid() int { return syscall.Getpid() }

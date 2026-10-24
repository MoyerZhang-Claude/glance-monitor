//go:build windows

// Package sensors gathers hardware readings on a dedicated OS thread and
// publishes immutable snapshots.
package sensors

import (
	"fmt"
	"runtime"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"glance/internal/data"
	"glance/internal/fps"
	"glance/internal/w32"
)

// Options are the parts of the configuration the sampler needs.
type Options struct {
	IntervalMs   int
	ClockMode    string
	WakeGPU      bool
	PerCoreTemps bool
	GPUThermals  bool
	LowWindowSec float64
	LowMethod    string
	GraphSec     float64
	FPSCapture   bool
	FPSExclude   string // "|"-joined process names (kept comparable)
}

type Manager struct {
	opts   atomic.Pointer[Options]
	latest atomic.Pointer[data.Snapshot]
	notify func()
	kick   chan struct{}
	quit   chan struct{}
	done   chan struct{}

	// owned by the sampler goroutine
	id       cpuIdent
	cpuName  string
	generic  *cpuGeneric
	intel    *intelCPU
	amd      *amdCPU
	gpuDev   *gpuPower
	nv       *nvml
	nvTried  time.Time
	nvTherm  *nvThermal
	gpuPDH   *gpuPDH
	gpuName  string
	elevated bool
	pawnVer  string
	pawnOK   bool

	tracker *fps.Tracker
	target  *fps.Target
	sessMu  sync.Mutex
	session *fps.Session
	etwErr  string

	status     data.Status
	samples    int
	excludeKey string
	selfCPU    uint64
	selfAt     time.Time
	ncpu       int
}

func NewManager(o Options, notify func()) *Manager {
	m := &Manager{notify: notify, kick: make(chan struct{}, 1), quit: make(chan struct{}), done: make(chan struct{})}
	m.opts.Store(&o)
	return m
}

// SetOptions applies new options and triggers an immediate sample.
func (m *Manager) SetOptions(o Options) {
	m.opts.Store(&o)
	select {
	case m.kick <- struct{}{}:
	default:
	}
}

// Latest returns the most recent snapshot (never nil after Start).
func (m *Manager) Latest() *data.Snapshot {
	if s := m.latest.Load(); s != nil {
		return s
	}
	return data.NewSnapshot()
}

func (m *Manager) Start() {
	ready := make(chan struct{})
	go m.run(ready)
	<-ready
}

func (m *Manager) Stop() {
	select {
	case <-m.quit:
	default:
		close(m.quit)
	}
	<-m.done
}

func (m *Manager) run(ready chan struct{}) {
	lockThread()
	defer close(m.done)
	m.init()
	m.sample()
	lastSample := time.Now()
	close(ready)

	for {
		o := m.opts.Load()
		iv := time.Duration(o.IntervalMs) * time.Millisecond
		if iv < 250*time.Millisecond {
			iv = 250 * time.Millisecond
		}
		// Align samples to the interval so the overlay ticks evenly.
		wait := iv - time.Duration(time.Now().UnixNano())%iv
		// Startup and settings kicks can otherwise put PDH samples only a few
		// milliseconds apart, producing impossible clock values (e.g. 42 GHz).
		if minWait := 250*time.Millisecond - time.Since(lastSample); wait < minWait {
			wait = minWait
		}
		t := time.NewTimer(wait)
		select {
		case <-m.quit:
			t.Stop()
			m.shutdown()
			return
		case <-m.kick:
			t.Stop()
			if time.Since(lastSample) < 250*time.Millisecond {
				continue
			}
		case <-t.C:
		}
		m.sample()
		lastSample = time.Now()
	}
}

func (m *Manager) init() {
	m.ncpu = w32.ActiveProcessorCount()
	m.elevated = w32.IsElevated()
	m.status.Elevated = m.elevated
	m.id = identifyCPU()
	m.cpuName = prettyCPUName(m.id.brand)
	m.generic = newCPUGeneric()

	m.pawnVer = PawnIOInstalledVersion()
	m.status.PawnIOVersion = m.pawnVer
	switch {
	case !m.elevated:
		m.status.PawnIO = "denied"
	default:
		var err error
		switch m.id.vendor {
		case "GenuineIntel":
			m.intel, err = newIntelCPU(m.id)
		case "AuthenticAMD", "HygonGenuine":
			m.amd, err = newAMDCPU(m.id)
		default:
			err = fmt.Errorf("unsupported vendor %q", m.id.vendor)
		}
		if err != nil {
			if m.pawnVer == "" {
				m.status.PawnIO = "missing"
			} else {
				m.status.PawnIO = "failed"
			}
			log.Printf("PawnIO CPU module: %v (installed=%q)", err, m.pawnVer)
		} else {
			m.status.PawnIO = "ok"
			m.pawnOK = true
		}
	}
	log.Printf("CPU: %s (vendor=%s family=%Xh model=%Xh) PawnIO=%s %s", m.cpuName, m.id.vendor, m.id.family, m.id.model, m.status.PawnIO, m.pawnVer)

	m.gpuDev = findGPUDevice("VEN_10DE")
	if m.gpuDev != nil {
		m.gpuName = cleanGPUName(m.gpuDev.Name)
		log.Printf("NVIDIA device: %s (power state D%d)", m.gpuDev.Name, m.gpuDev.state()-1)
	} else {
		m.status.NVML = "none"
		m.gpuPDH = newGPUPDH()
		m.status.GPUSource = "pdh"
	}

	m.tracker = fps.NewTracker(w32.QPF(), 30)
	m.target = fps.NewTarget(m.tracker, uint32(syscallGetpid()))
	m.syncETW(m.opts.Load().FPSCapture)
}

func cleanGPUName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "NVIDIA ")
	s = strings.Replace(s, "GeForce ", "", 1)
	return s
}

// syncETW starts or stops the present-event session.
func (m *Manager) syncETW(want bool) {
	m.sessMu.Lock()
	defer m.sessMu.Unlock()
	switch {
	case want && m.session == nil:
		if !m.elevated {
			m.status.ETW = "denied"
			return
		}
		s, err := fps.Start(m.tracker, uint32(syscallGetpid()))
		if err != nil {
			m.status.ETW = "failed"
			m.etwErr = err.Error()
			log.Printf("ETW start failed: %v", err)
			return
		}
		m.session = s
		m.status.ETW = "ok"
	case !want && m.session != nil:
		m.session.Stop()
		m.session = nil
		m.status.ETW = "off"
	case !want:
		m.status.ETW = "off"
	}
}

func (m *Manager) ensureNVML() {
	if m.nv != nil || time.Since(m.nvTried) < 30*time.Second {
		return
	}
	m.nvTried = time.Now()
	nv, err := loadNVML()
	if err != nil {
		m.status.NVML = "failed"
		log.Printf("NVML: %v", err)
		if m.gpuPDH == nil {
			m.gpuPDH = newGPUPDH()
		}
		m.status.GPUSource = "pdh"
		return
	}
	m.nv = nv
	m.status.NVML = "ok"
	m.status.GPUSource = "nvml"
	if nv.name != "" {
		m.gpuName = cleanGPUName(nv.name)
	}
	log.Printf("NVML ready: %s bus=%d slot=%d", nv.name, nv.bus, nv.slot)
	if m.pawnOK || (m.elevated && m.pawnVer != "") {
		m.nvTherm = newNVThermal(nv.bus, nv.slot)
	}
}

func (m *Manager) sample() {
	o := m.opts.Load()
	s := data.NewSnapshot()
	s.CPUName = m.cpuName
	s.GPUName = m.gpuName
	s.Unsupported = map[string]bool{data.CPUVRM: true}
	if m.nv != nil && m.nv.powerMethod == "none" {
		s.Unsupported[data.GPUPower] = true
	}

	m.sessMu.Lock()
	sessionOn := m.session != nil
	m.sessMu.Unlock()
	if o.FPSCapture != sessionOn {
		m.syncETW(o.FPSCapture)
	}
	m.tracker.SetKeep(maxf(o.LowWindowSec, o.GraphSec) + 3)
	if o.FPSExclude != m.excludeKey {
		m.excludeKey = o.FPSExclude
		m.target.SetExclude(strings.Split(o.FPSExclude, "|"))
	}

	// CPU
	thermal, thermalOK := m.generic.read(s, o.ClockMode)
	switch {
	case m.intel != nil:
		m.intel.read(s, o.PerCoreTemps)
	case m.amd != nil:
		m.amd.read(s)
	}
	if _, ok := s.Get(data.CPUTemp); ok {
		m.status.CPUTempSource = "msr"
	} else if thermalOK {
		s.Set(data.CPUTemp, thermal)
		m.status.CPUTempSource = "acpi"
	} else {
		m.status.CPUTempSource = "none"
	}

	// GPU
	if m.gpuDev != nil {
		st := m.gpuDev.state()
		asleep := st >= 2 && !o.WakeGPU
		s.Readings = append(s.Readings, data.Reading{Group: "GPU", Name: "电源状态 D", Value: float64(max(st-1, 0)), Unit: ""})
		if asleep {
			s.GPUAsleep = true
		} else {
			m.ensureNVML()
			if m.nv != nil && m.nv.powerMethod == "none" && time.Since(m.nv.probedAt) > time.Minute {
				m.nv.reprobePower()
			}
			if m.nv != nil {
				m.nv.read(s)
				if m.nvTherm != nil && o.GPUThermals {
					m.nvTherm.read(s)
				}
			}
		}
	}
	if m.gpuPDH != nil && (m.nv == nil) && !s.GPUAsleep {
		m.gpuPDH.read(s)
	}
	s.GPUName = m.gpuName
	s.Unsupported = map[string]bool{data.CPUVRM: true}
	if m.nv != nil && m.nv.powerMethod == "none" {
		s.Unsupported[data.GPUPower] = true
	}

	// Memory
	if ms, ok := w32.GlobalMemoryStatus(); ok && ms.TotalPhys > 0 {
		used := float64(ms.TotalPhys-ms.AvailPhys) / (1 << 30)
		total := float64(ms.TotalPhys) / (1 << 30)
		s.Set(data.RAMUsed, used)
		s.Set("ram.total", total)
		s.Set(data.RAMUsage, float64(ms.MemoryLoad))
		s.Readings = append(s.Readings, data.Reading{Group: "内存", Name: "已用", Value: used, Unit: "GB"})
		s.Readings = append(s.Readings, data.Reading{Group: "内存", Name: "总量", Value: total, Unit: "GB"})
	}

	// Glance's own footprint (shown in settings so it can be verified).
	if cpu, priv, ws := w32.SelfUsage(); cpu > 0 {
		now := time.Now()
		if !m.selfAt.IsZero() && m.ncpu > 0 {
			dt := now.Sub(m.selfAt).Seconds()
			if dt > 0 {
				pct := float64(cpu-m.selfCPU) / 1e7 / dt / float64(m.ncpu) * 100
				s.Set("self.cpu", pct)
			}
		}
		m.selfCPU, m.selfAt = cpu, now
		s.Set("self.mem", float64(priv)/(1<<20))
		s.Set("self.ws", float64(ws)/(1<<20))
	}

	// FPS
	if sessionOn {
		now := w32.QPC()
		pid := m.target.Resolve(now)
		if pid != 0 {
			st := m.tracker.Query(pid, now, fps.Options{LowWindowSec: o.LowWindowSec, Method: o.LowMethod, GraphSec: o.GraphSec})
			if st.Active {
				s.FPSActive = true
				s.FPSPid = pid
				s.FPSProcess = m.target.Name(pid)
				s.FPSAPI = st.Src.String()
				s.FrameGen = st.FrameGen
				s.Set(data.FPS, st.FPS)
				s.Set(data.FrameTime, st.FrameTime)
				if st.HasLows {
					s.Set(data.FPSLow1, st.Low1)
					s.Set(data.FPSLow01, st.Low01)
				}
				s.FrameTimes = st.Graph
				s.FrameSpan = st.GraphSpan
			}
		}
		for i, p := range m.tracker.Presenting(now, 2.5) {
			if i >= 6 {
				break
			}
			s.Readings = append(s.Readings, data.Reading{Group: "帧率", Name: fmt.Sprintf("%s (%s)", m.target.Name(p.PID), p.Src), Value: float64(p.Rate), Unit: "FPS"})
		}
	}

	st := m.status
	st.VRM = "none"
	st.Notes = nil
	if m.etwErr != "" && st.ETW == "failed" {
		st.Notes = append(st.Notes, "帧率统计启动失败: "+m.etwErr)
	}
	s.Status = st
	m.samples++
	if m.samples <= 3 || m.samples%120 == 0 || (s.FPSActive && m.samples%15 == 0 && m.samples < 2400) {
		m.logSnapshot(s)
	}
	m.latest.Store(s)
	if m.notify != nil {
		m.notify()
	}
}

func (m *Manager) shutdown() {
	m.syncETW(false)
	if m.intel != nil {
		m.intel.close()
	}
	if m.amd != nil {
		m.amd.close()
	}
	if m.nvTherm != nil {
		m.nvTherm.close()
	}
	if m.nv != nil {
		m.nv.close()
	}
	if m.gpuDev != nil {
		m.gpuDev.close()
	}
	if m.gpuPDH != nil {
		m.gpuPDH.close()
	}
	m.generic.close()
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func (m *Manager) logSnapshot(s *data.Snapshot) {
	var b strings.Builder
	keys := []string{data.CPUUsage, data.CPUClock, data.CPUTemp, data.CPUTempMax, data.CPUPower,
		data.GPUUsage, data.GPUClock, data.GPUTemp, data.GPUHotspot, data.GPUMemTemp, data.GPUPower, data.GPUVRAM,
		data.FPS, data.FPSLow1, data.RAMUsage}
	for _, k := range keys {
		if v, ok := s.Get(k); ok {
			fmt.Fprintf(&b, "%s=%.1f ", k, v)
		}
	}
	fmt.Fprintf(&b, "gpuAsleep=%v fps=%v(%s %s) pawn=%s nvml=%s etw=%s",
		s.GPUAsleep, s.FPSActive, s.FPSProcess, s.FPSAPI, s.Status.PawnIO, s.Status.NVML, s.Status.ETW)
	if m.session != nil {
		fmt.Fprintf(&b, " etwEvents=%d", m.session.Events.Load())
	}
	for _, r := range s.Readings {
		if r.Group == "帧率" {
			fmt.Fprintf(&b, " [%s %.0f]", r.Name, r.Value)
		}
	}
	if v, ok := s.Get("self.mem"); ok {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		fmt.Fprintf(&b, " self: private=%.0fMiB goHeap=%.0fMiB goSys=%.0fMiB",
			v, float64(ms.HeapInuse)/(1<<20), float64(ms.Sys)/(1<<20))
	}
	log.Printf("sample #%d: %s", m.samples, b.String())
	if s.FPSActive {
		log.Printf("cadence %s: %s", s.FPSProcess, m.tracker.Debug(s.FPSPid, w32.QPC()))
	}
}

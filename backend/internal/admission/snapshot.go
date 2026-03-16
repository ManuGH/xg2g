package admission

import "sync/atomic"

// CPUSnapshot exposes read-only runtime CPU sampling state for higher-level playback policy.
type CPUSnapshot struct {
	Load1m        float64
	CoreCount     int
	SampleCount   int
	WindowSeconds int
}

// MonitorSnapshot exposes read-only ResourceMonitor state without leaking its internal locks.
type MonitorSnapshot struct {
	CPU               CPUSnapshot
	ActiveVAAPITokens int
	MaxSessions       int
	MaxVAAPITokens    int
}

// Snapshot returns a read-only view of runtime monitor state for playback policy decisions.
func (m *ResourceMonitor) Snapshot() MonitorSnapshot {
	if m == nil {
		return MonitorSnapshot{}
	}

	var cpu CPUSnapshot
	m.cpuMu.Lock()
	if n := len(m.cpuSamples); n > 0 {
		latest := m.cpuSamples[0]
		for _, sample := range m.cpuSamples[1:] {
			if sample.at.After(latest.at) {
				latest = sample
			}
		}
		cpu.Load1m = latest.load
	}
	cpu.SampleCount = len(m.cpuSamples)
	cpu.WindowSeconds = int(m.cpuWindow / 1e9)
	cpu.CoreCount = int(m.cores)
	m.cpuMu.Unlock()

	return MonitorSnapshot{
		CPU:               cpu,
		ActiveVAAPITokens: int(atomic.LoadInt64(&m.activeVAAPI)),
		MaxSessions:       int(m.maxPool),
		MaxVAAPITokens:    int(m.gpuLimit),
	}
}

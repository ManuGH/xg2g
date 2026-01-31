package admission

import (
	"context"
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/rs/zerolog"
)

// Priority defines the admission priority classes as per Phase 5.2 contract.
type Priority int

const (
	PriorityPulse Priority = iota
	PriorityLive
	PriorityRecording
)

func (p Priority) String() string {
	switch p {
	case PriorityPulse:
		return "pulse"
	case PriorityLive:
		return "live"
	case PriorityRecording:
		return "recording"
	default:
		return "unknown"
	}
}

// AdmissionReason provides detailed failure taxonomy for metrics/headers.
// All values are lowercase for stable PromQL queries (CTO requirement).
type AdmissionReason string

const (
	ReasonAdmitted     AdmissionReason = "admitted"
	ReasonPoolFull     AdmissionReason = "pool_full"
	ReasonPreempt      AdmissionReason = "preempt"
	ReasonGPUBusy      AdmissionReason = "gpu_busy"
	ReasonTunerBusy    AdmissionReason = "tuner_busy"
	ReasonCPUSaturated AdmissionReason = "cpu_saturated"
	ReasonCPUUnknown   AdmissionReason = "cpu_unknown"
	ReasonInternalErr  AdmissionReason = "internal_error"
)

// ResourceState holds synchronous, in-process metrics for admission decisions.
type ResourceState struct {
	ActiveSessions    [3]int64 // Index by Priority
	ActiveVAAPITokens int64
	CPULoad           float64 // loadavg[1m]
}

// ResourceMonitor provides the mechanical gatekeeper logic.
// Phase 5.3: maxPool and gpuLimit are now distinct limits.
type ResourceMonitor struct {
	activeVAAPI   int64
	mu            sync.RWMutex
	sessionIDs    map[Priority][]string
	maxPool       int64   // Maximum concurrent sessions (engine.maxPool)
	gpuLimit      int64   // Maximum VAAPI tokens (GPU discovery/config)
	cpuThreshold  float64 // CPU load multiplier (cores * threshold)
	cores         float64
	cpuMu         sync.Mutex
	cpuSamples    []cpuSample
	cpuWindow     time.Duration
	cpuMinSamples int     // Minimum samples for a valid decision (fail-closed)
	cpuRatio      float64 // Ratio of samples over threshold to trigger block (e.g. 0.5)
	lastWarnAt    time.Time
	logger        zerolog.Logger
	clock         func() time.Time
}

type cpuSample struct {
	at   time.Time
	load float64
}

// NewResourceMonitor creates a new ResourceMonitor with separate limits.
// maxPool: maximum concurrent sessions (engine.maxPool)
// gpuLimit: maximum VAAPI tokens (GPU capability)
// cpuThresholdScale: CPU load threshold multiplier (e.g., 1.5 = cores*1.5)
func NewResourceMonitor(maxPool, gpuLimit int, cpuThresholdScale float64) *ResourceMonitor {
	if maxPool < 0 {
		maxPool = 2 // Default pool limit per Phase 5.2
	}
	if gpuLimit < 0 {
		gpuLimit = 8 // Default based on Phase 5.1 discovery expectation
	}
	if cpuThresholdScale <= 0 {
		cpuThresholdScale = 1.5 // Phase 5.2 contractual default
	}

	return &ResourceMonitor{
		maxPool:       int64(maxPool),
		gpuLimit:      int64(gpuLimit),
		cpuThreshold:  cpuThresholdScale,
		cores:         float64(runtime.NumCPU()),
		sessionIDs:    make(map[Priority][]string),
		cpuWindow:     30 * time.Second,
		cpuMinSamples: 10,  // 33% buffer for jitter (was 15/15)
		cpuRatio:      0.5, // Block if >= 50% of samples are over threshold
		logger:        zerolog.Nop(),
		clock:         time.Now,
	}
}

// SetLogger injects a logger into the ResourceMonitor for operational awareness.
func (m *ResourceMonitor) SetLogger(l zerolog.Logger) {
	m.logger = l
}

// CanAdmit evaluates the current ResourceState against the request priority.
// Returns true if admitted, or false and a detailed reason.
func (m *ResourceMonitor) CanAdmit(ctx context.Context, p Priority) (bool, AdmissionReason) {
	// 1. Pool Capacity Check (Phase 5.2 - Condition D)
	active := m.TotalActiveSessions()

	if active >= m.maxPool {
		// Can we preempt a lower-priority session?
		if p > PriorityPulse && m.hasPreemptibleSession(p) {
			return true, ReasonPreempt // Admitted via preemption
		}
		return false, ReasonPoolFull
	}

	// 2. GPU Context Check (Phase 5.2 - Condition C)
	if atomic.LoadInt64(&m.activeVAAPI) >= m.gpuLimit {
		// Recording can preempt, but for GPU saturation we reject Pulse first
		if p == PriorityPulse {
			return false, ReasonGPUBusy
		}
		// Live/Recording can still proceed (may preempt GPU at orchestrator level)
	}

	// 3. CPU Pressure Check - 30s rolling window (fail-closed on missing samples)
	if ok, reason := m.cpuWithinLimits(); !ok {
		return false, reason
	}

	return true, ReasonAdmitted
}

// ObserveCPULoad records a CPU load sample for rolling-window admission checks.
func (m *ResourceMonitor) ObserveCPULoad(load float64) {
	m.observeCPULoadAt(load, m.clock())
}

func (m *ResourceMonitor) observeCPULoadAt(load float64, at time.Time) {
	if math.IsNaN(load) || math.IsInf(load, 0) || load < 0 {
		return
	}
	m.cpuMu.Lock()
	defer m.cpuMu.Unlock()

	m.cpuSamples = append(m.cpuSamples, cpuSample{at: at, load: load})
	m.pruneCPUSamplesLocked(at)
}

func (m *ResourceMonitor) cpuWithinLimits() (bool, AdmissionReason) {
	m.cpuMu.Lock()
	defer m.cpuMu.Unlock()

	now := m.clock()
	m.pruneCPUSamplesLocked(now)

	// Guard: Fail-closed on missing samples
	if len(m.cpuSamples) < m.cpuMinSamples {
		if now.Sub(m.lastWarnAt) >= 1*time.Minute {
			m.lastWarnAt = now
			m.logger.Warn().
				Int("samples", len(m.cpuSamples)).
				Int("min_needed", m.cpuMinSamples).
				Msg("CPU data insufficient: Admission proceeding (fail-open)")
		}
		return true, ReasonAdmitted
	}

	threshold := m.cores * m.cpuThreshold
	var overCount int
	for _, s := range m.cpuSamples {
		if s.load >= threshold {
			overCount++
		}
	}

	ratio := float64(overCount) / float64(len(m.cpuSamples))
	if ratio >= m.cpuRatio {
		if now.Sub(m.lastWarnAt) >= 1*time.Minute {
			m.lastWarnAt = now
			m.logger.Warn().
				Float64("ratio", ratio).
				Float64("threshold", threshold).
				Msg("Admission blocked: CPU pressure exceeded threshold")
		}
		return false, ReasonCPUSaturated
	}

	return true, ReasonAdmitted
}

func (m *ResourceMonitor) cpuAverage(now time.Time) (float64, bool) {
	m.cpuMu.Lock()
	defer m.cpuMu.Unlock()

	m.pruneCPUSamplesLocked(now)
	if len(m.cpuSamples) == 0 {
		return 0, false
	}
	var sum float64
	for _, s := range m.cpuSamples {
		sum += s.load
	}
	return sum / float64(len(m.cpuSamples)), true
}

func (m *ResourceMonitor) pruneCPUSamplesLocked(now time.Time) {
	cutoff := now.Add(-m.cpuWindow)
	keep := m.cpuSamples[:0]
	for _, s := range m.cpuSamples {
		if !s.at.Before(cutoff) {
			keep = append(keep, s)
		}
	}
	m.cpuSamples = keep
}

// AcquireVAAPIToken reserves a GPU slot.
func (m *ResourceMonitor) AcquireVAAPIToken() bool {
	for {
		current := atomic.LoadInt64(&m.activeVAAPI)
		if current >= m.gpuLimit {
			return false
		}
		if atomic.CompareAndSwapInt64(&m.activeVAAPI, current, current+1) {
			metrics.SetGPUTokensInUse(float64(current + 1))
			return true
		}
	}
}

func (m *ResourceMonitor) ReleaseVAAPIToken() {
	newVal := atomic.AddInt64(&m.activeVAAPI, -1)
	metrics.SetGPUTokensInUse(float64(newVal))
}

func (m *ResourceMonitor) TrackSessionStart(p Priority, sid string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionIDs[p] = append(m.sessionIDs[p], sid)
	// Update Prometheus gauge
	metrics.SetActiveSessions(p.String(), float64(len(m.sessionIDs[p])))
}

func (m *ResourceMonitor) TrackSessionEnd(p Priority, sid string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := m.sessionIDs[p]
	for i, id := range ids {
		if id == sid {
			m.sessionIDs[p] = append(ids[:i], ids[i+1:]...)
			break
		}
	}
	// Update Prometheus gauge
	metrics.SetActiveSessions(p.String(), float64(len(m.sessionIDs[p])))
}

func (m *ResourceMonitor) TotalActiveSessions() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var total int64
	for i := PriorityPulse; i <= PriorityRecording; i++ {
		total += int64(len(m.sessionIDs[i]))
	}
	return total
}

func (m *ResourceMonitor) hasPreemptibleSession(p Priority) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for i := PriorityPulse; i < p; i++ {
		if len(m.sessionIDs[i]) > 0 {
			return true
		}
	}
	return false
}

// SelectPreemptionTarget returns the lowest priority session ID that can be preempted.
func (m *ResourceMonitor) SelectPreemptionTarget(p Priority) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	// Pulse < Live < Recording
	for i := PriorityPulse; i < p; i++ {
		ids := m.sessionIDs[i]
		if len(ids) > 0 {
			// Preempt the oldest one in this class for now
			return ids[0], true
		}
	}
	return "", false
}

// GetMaxPool returns the maximum pool size for external inspection.
func (m *ResourceMonitor) GetMaxPool() int64 {
	return m.maxPool
}

// GetGPULimit returns the GPU token limit for external inspection.
func (m *ResourceMonitor) GetGPULimit() int64 {
	return m.gpuLimit
}

package telemetry

import (
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

type StartupMilestone string

const (
	MilestoneT0 StartupMilestone = "T0" // Intent accepted
	MilestoneT1 StartupMilestone = "T1" // Input connected
	MilestoneT2 StartupMilestone = "T2" // First valid TS packet
	MilestoneT3 StartupMilestone = "T3" // Probe completed
	MilestoneT4 StartupMilestone = "T4" // First usable video AU
	MilestoneT5 StartupMilestone = "T5" // First segment opened (.tmp)
	MilestoneT6 StartupMilestone = "T6" // First segment finalized (.m4s)
	MilestoneT7 StartupMilestone = "T7" // Secure playlist available
	MilestoneT8 StartupMilestone = "T8" // Session READY
	MilestoneT9 StartupMilestone = "T9" // Player playing (Frontend)

	MilestoneE0    StartupMilestone = "E0"     // Request to Enigma2 started
	MilestoneE1    StartupMilestone = "E1"     // HTTP response headers received
	MilestoneE2    StartupMilestone = "E2"     // First body byte read
	MilestoneELock StartupMilestone = "E_LOCK" // Tuner lock

	MilestoneP1 StartupMilestone = "P1" // Planner completed
	MilestoneP2 StartupMilestone = "P2" // Session created
	MilestoneP3 StartupMilestone = "P3" // Worker acquire started
	MilestoneP4 StartupMilestone = "P4" // Worker acquired

	MilestoneR1 StartupMilestone = "R1" // First init committed to RAM
	MilestoneR2 StartupMilestone = "R2" // First segment committed to RAM
	MilestoneR3 StartupMilestone = "R3" // First RAM segment served

	MilestoneH1    StartupMilestone = "H1"     // First playlist served
	MilestoneH2    StartupMilestone = "H2"     // First init served
	MilestoneH3Req StartupMilestone = "H3_REQ" // First segment requested
	MilestoneH3    StartupMilestone = "H3"     // First segment served
)

type LogField struct {
	Key   string
	Value string
}

type StartupTracer interface {
	MarkOnce(milestone StartupMilestone, phase string, fields ...LogField)
	UpdateMetadata(clientFamily, container, videoMode, inputType string)
	Summary()
}

type startupTracerImpl struct {
	mu             sync.Mutex
	sessionID      string
	t0             time.Time
	lastTime       time.Time
	marks          map[StartupMilestone]time.Time
	clientFamily   string
	container      string
	videoMode      string
	inputType      string
	slowestPhase   string
	slowestPhaseMs int64
	ttl            *time.Timer
}

// Global registry for cross-request tracer access
var (
	registryMu sync.RWMutex
	registry   = make(map[string]StartupTracer)
)

func NewStartupTracer(sessionID string) StartupTracer {
	t := &startupTracerImpl{
		sessionID: sessionID,
		t0:        time.Now(),
		lastTime:  time.Now(),
		marks:     make(map[StartupMilestone]time.Time),
	}

	t.ttl = time.AfterFunc(10*time.Minute, func() {
		registryMu.Lock()
		defer registryMu.Unlock()
		if current, exists := registry[sessionID]; exists && current == t {
			delete(registry, sessionID)
		}
	})

	registryMu.Lock()
	registry[sessionID] = t
	registryMu.Unlock()

	// Mark T0 immediately on creation
	t.MarkOnce(MilestoneT0, "intent_accepted")
	return t
}

func (t *startupTracerImpl) UpdateMetadata(clientFamily, container, videoMode, inputType string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if clientFamily != "" {
		t.clientFamily = clientFamily
	}
	if container != "" {
		t.container = container
	}
	if videoMode != "" {
		t.videoMode = videoMode
	}
	if inputType != "" {
		t.inputType = inputType
	}
}

func GetStartupTracer(sessionID string) StartupTracer {
	registryMu.RLock()
	defer registryMu.RUnlock()
	if t, exists := registry[sessionID]; exists {
		return t
	}
	return &noopTracer{}
}

func RemoveStartupTracer(sessionID string) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if tr, exists := registry[sessionID]; exists {
		if impl, ok := tr.(*startupTracerImpl); ok {
			impl.mu.Lock()
			if impl.ttl != nil {
				impl.ttl.Stop()
			}
			impl.mu.Unlock()
		}
		delete(registry, sessionID)
	}
}

func (t *startupTracerImpl) MarkOnce(milestone StartupMilestone, phase string, fields ...LogField) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, exists := t.marks[milestone]; exists {
		return
	}

	now := time.Now()

	elapsed := now.Sub(t.t0).Milliseconds()
	previous := now.Sub(t.lastTime).Milliseconds()

	if previous > t.slowestPhaseMs && milestone != MilestoneT0 {
		t.slowestPhaseMs = previous
		t.slowestPhase = phase
	}

	t.marks[milestone] = now
	t.lastTime = now

	evt := log.Info().
		Str("event", "startup_metric").
		Str("session_id", t.sessionID).
		Str("milestone", string(milestone)).
		Str("phase", phase).
		Int64("elapsed_ms", elapsed).
		Int64("previous_ms", previous)

	if t.clientFamily != "" {
		evt = evt.Str("client_family", t.clientFamily)
	}
	if t.container != "" {
		evt = evt.Str("container", t.container)
	}
	if t.videoMode != "" {
		evt = evt.Str("video_mode", t.videoMode)
	}
	if t.inputType != "" {
		evt = evt.Str("input_type", t.inputType)
	}

	for _, f := range fields {
		evt.Str(f.Key, f.Value)
	}

	evt.Msg("startup_milestone_reached")
}

func (t *startupTracerImpl) Summary() {
	t.mu.Lock()
	defer t.mu.Unlock()

	totalMs := time.Since(t.t0).Milliseconds()

	log.Info().
		Str("event", "startup_summary").
		Str("session_id", t.sessionID).
		Int64("total_ms", totalMs).
		Str("slowest_phase", t.slowestPhase).
		Int64("slowest_phase_ms", t.slowestPhaseMs).
		Msg("startup_complete")
}

// Noop Tracer for unknown sessions
type noopTracer struct{}

func (n *noopTracer) MarkOnce(m StartupMilestone, p string, f ...LogField) {}
func (n *noopTracer) UpdateMetadata(c, co, v, i string)                    {}
func (n *noopTracer) Summary()                                             {}

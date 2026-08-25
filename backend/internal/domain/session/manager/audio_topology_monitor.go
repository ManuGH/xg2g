package manager

import (
	"context"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/audiotopology"
	"github.com/rs/zerolog"
)

// AudioTopologyProbeFunc defines the signature for probing a live stream session.
type AudioTopologyProbeFunc func(ctx context.Context, sessionID, serviceRef, inputURL string) (audiotopology.AudioTopology, error)

// AudioTopologyMonitorConfig holds configuration for the session-scoped monitor.
type AudioTopologyMonitorConfig struct {
	PollInterval          time.Duration
	ProbeTimeout          time.Duration
	RequiredConfirmations int
}

// DefaultAudioTopologyMonitorConfig provides conservative defaults.
func DefaultAudioTopologyMonitorConfig() AudioTopologyMonitorConfig {
	return AudioTopologyMonitorConfig{
		PollInterval:          12 * time.Second,
		ProbeTimeout:          3 * time.Second,
		RequiredConfirmations: 2,
	}
}

// SessionAudioTopologyMonitor monitors audio topology evolution for an active live streaming session.
type SessionAudioTopologyMonitor struct {
	sessionID  string
	serviceRef string
	inputURL   string
	cfg        AudioTopologyMonitorConfig
	probeFn    AudioTopologyProbeFunc
	logger     zerolog.Logger

	mu                sync.RWMutex
	currentTopology   audiotopology.AudioTopology
	candidateTopology *audiotopology.AudioTopology
	candidateCount    int
	changeCallback    func(change audiotopology.TopologyChange, newTopo audiotopology.AudioTopology)
}

// NewSessionAudioTopologyMonitor constructs a session-scoped monitor.
func NewSessionAudioTopologyMonitor(
	sessionID string,
	serviceRef string,
	inputURL string,
	initialTopo audiotopology.AudioTopology,
	cfg AudioTopologyMonitorConfig,
	probeFn AudioTopologyProbeFunc,
	logger zerolog.Logger,
) *SessionAudioTopologyMonitor {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 12 * time.Second
	}
	if cfg.ProbeTimeout <= 0 {
		cfg.ProbeTimeout = 3 * time.Second
	}
	if cfg.RequiredConfirmations <= 0 {
		cfg.RequiredConfirmations = 2
	}

	return &SessionAudioTopologyMonitor{
		sessionID:       sessionID,
		serviceRef:      serviceRef,
		inputURL:        inputURL,
		currentTopology: initialTopo,
		cfg:             cfg,
		probeFn:         probeFn,
		logger:          logger.With().Str("component", "audio_topology_monitor").Str("session_id", sessionID).Logger(),
	}
}

// SetChangeCallback allows attaching a listener when a stabilized change occurs.
func (m *SessionAudioTopologyMonitor) SetChangeCallback(cb func(change audiotopology.TopologyChange, newTopo audiotopology.AudioTopology)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.changeCallback = cb
}

// CurrentTopology returns the current active topology snapshot.
func (m *SessionAudioTopologyMonitor) CurrentTopology() audiotopology.AudioTopology {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentTopology
}

// Start launches the background monitoring loop, bounded by the session ctx.
func (m *SessionAudioTopologyMonitor) Start(ctx context.Context) {
	go m.run(ctx)
}

func (m *SessionAudioTopologyMonitor) run(ctx context.Context) {
	m.logger.Debug().
		Str("service_ref", m.serviceRef).
		Dur("poll_interval", m.cfg.PollInterval).
		Uint64("initial_structural_revision", m.currentTopology.StructuralRevision).
		Msg("session audio topology monitor started")

	ticker := time.NewTicker(m.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.logger.Debug().Msg("session audio topology monitor stopped")
			return
		case <-ticker.C:
			m.pollOnce(ctx)
		}
	}
}

// PollOnce executes a single inspection tick (public for deterministic unit testing).
func (m *SessionAudioTopologyMonitor) PollOnce(ctx context.Context) {
	m.pollOnce(ctx)
}

func (m *SessionAudioTopologyMonitor) pollOnce(parentCtx context.Context) {
	if m.probeFn == nil {
		return
	}

	probeCtx, cancel := context.WithTimeout(parentCtx, m.cfg.ProbeTimeout)
	defer cancel()

	newTopo, err := m.probeFn(probeCtx, m.sessionID, m.serviceRef, m.inputURL)
	if err != nil {
		m.logger.Debug().Err(err).Msg("periodic audio topology probe returned error")
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	diff := audiotopology.DiffTopologies(m.currentTopology, newTopo)
	if !diff.HasChange {
		// Stable state matches current: reset any transient candidate
		m.candidateTopology = nil
		m.candidateCount = 0
		return
	}

	// 1. Metadata-only change: adopt immediately without debouncing
	if diff.IsMetadataOnly {
		oldMeta := m.currentTopology.MetadataRevision
		m.currentTopology = newTopo
		m.candidateTopology = nil
		m.candidateCount = 0

		m.logger.Info().
			Str("service_ref", m.serviceRef).
			Uint64("old_metadata_revision", oldMeta).
			Uint64("new_metadata_revision", newTopo.MetadataRevision).
			Str("summary", diff.Summary).
			Msg("audio topology metadata updated")

		if m.changeCallback != nil {
			m.changeCallback(diff, newTopo)
		}
		return
	}

	// 2. Structural change: requires consecutive confirmations
	if diff.IsStructural {
		if m.candidateTopology != nil && m.candidateTopology.StructuralRevision == newTopo.StructuralRevision {
			m.candidateCount++
		} else {
			cand := newTopo
			m.candidateTopology = &cand
			m.candidateCount = 1
			m.logger.Debug().
				Str("service_ref", m.serviceRef).
				Uint64("candidate_structural_revision", newTopo.StructuralRevision).
				Int("candidate_count", m.candidateCount).
				Str("summary", diff.Summary).
				Msg("structural change candidate observed; waiting for confirmation probe")
		}

		if m.candidateCount >= m.cfg.RequiredConfirmations {
			oldRev := m.currentTopology.StructuralRevision
			newRev := newTopo.StructuralRevision
			m.currentTopology = newTopo
			m.candidateTopology = nil
			m.candidateCount = 0

			m.logger.Info().
				Str("service_ref", m.serviceRef).
				Uint64("old_structural_revision", oldRev).
				Uint64("new_structural_revision", newRev).
				Int("confirmed_probes", m.cfg.RequiredConfirmations).
				Str("summary", diff.Summary).
				Msg("audio topology structural change stabilized and observed")

			if m.changeCallback != nil {
				m.changeCallback(diff, newTopo)
			}
		}
	}
}

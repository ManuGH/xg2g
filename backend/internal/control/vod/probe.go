package vod

import (
	"context"
	"errors"
)

// Probe delegates to the infra prober
func (m *Manager) Probe(ctx context.Context, path string) (*StreamInfo, error) {
	if m.prober == nil {
		return nil, errors.New("prober not configured")
	}
	return m.prober.Probe(ctx, path)
}

// enqueueProbe attempts to add a probe request to the queue without blocking.
// Returns true if enqueued, false if dropped.
func (m *Manager) enqueueProbe(req probeRequest) bool {
	select {
	case m.probeCh <- req:
		probeQueueLength.Set(float64(len(m.probeCh)))
		return true
	default:
		probeDropped.Inc()
		return false
	}
}

// TriggerProbe initiates an async background probe for a recording.
func (m *Manager) TriggerProbe(id string, input string) {
	m.mu.Lock()
	meta, ok := m.metadata[id]
	if ok && meta.State == ArtifactStatePreparing {
		m.mu.Unlock()
		return // Already in progress
	}

	// Immediate state transition to PREPARING
	if !ok {
		meta = Metadata{State: ArtifactStateUnknown} // Initialize if missing
	}

	previousState := meta.State
	if !ok { // Handle new items correctly
		previousState = ArtifactStateUnknown
	}

	if input != "" {
		meta.ResolvedPath = input
	}
	if input == "" && meta.ResolvedPath == "" && m.pathMapper == nil {
		meta.State = ArtifactStateFailed
		meta.Error = "missing input path for probe"
		m.touch(&meta)
		m.metadata[id] = meta
		m.mu.Unlock()
		return
	}

	meta.State = ArtifactStatePreparing
	m.touch(&meta)
	m.metadata[id] = meta
	capturedGen := meta.StateGen
	resolvedPath := meta.ResolvedPath // Capture under lock
	m.mu.Unlock()

	// Enqueue background processing
	if !m.enqueueProbe(probeRequest{ServiceRef: id, InputPath: resolvedPath}) {
		// Queue full - revert state to avoid stuck PREPARING
		m.revertStateGuard(id, capturedGen, previousState)
	}
}

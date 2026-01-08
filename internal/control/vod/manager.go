package vod

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Manager handles VOD build jobs.
type Manager struct {
	runner Runner
	prober Prober
	mu     sync.Mutex
	jobs   map[string]Handle
}

func NewManager(runner Runner, prober Prober) *Manager {
	return &Manager{
		runner: runner,
		prober: prober,
		jobs:   make(map[string]Handle),
	}
}

// Probe delegates to the infra prober
func (m *Manager) Probe(ctx context.Context, path string) (*StreamInfo, error) {
	if m.prober == nil {
		return nil, errors.New("prober not configured")
	}
	return m.prober.Probe(ctx, path)
}

// StartBuild initiates a VOD build.
// Returns a Monitor/Handle to track it, or error if strict policy fails.
func (m *Manager) StartBuild(ctx context.Context, id, input, workDir, outputTemp string, profile Profile) (Handle, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if h, exists := m.jobs[id]; exists {
		// Already exists - strict check if running?
		// For now simple idempotency: return existing handle
		return h, nil
	}

	spec := Spec{
		Input:      input,
		WorkDir:    workDir,
		OutputTemp: outputTemp,
		Profile:    profile,
	}

	// Start via Infra
	h, err := m.runner.Start(ctx, spec)
	if err != nil {
		return nil, err
	}

	m.jobs[id] = h
	return h, nil
}

// Get returns the status of a job if it exists.
// Logic: Stub implementation returning DTO.
func (m *Manager) Get(ctx context.Context, id string) (*JobStatus, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, exists := m.jobs[id]
	if !exists {
		return nil, false
	}

	// TODO: Map Handle state to JobStatus
	// For compilation stub:
	return &JobStatus{
		State: JobStateBuilding, // Dummy
	}, true
}

// EnsureSpec validates context and prepares a Spec, serving as a gateway.
// Logic: Delegating to StartBuild for idempotency.
func (m *Manager) EnsureSpec(ctx context.Context, id, input, workDir, outputTemp string, profile Profile) (Spec, error) {
	_, err := m.StartBuild(ctx, id, input, workDir, outputTemp, profile)
	if err != nil {
		return Spec{}, err
	}
	// Return the Spec that was used/created
	return Spec{
		Input:      input,
		WorkDir:    workDir,
		OutputTemp: outputTemp,
		Profile:    profile,
	}, nil
}

// Prober interface to be injected into API if needed, or Manager exposes Probe?
// For Gate A compliance, Control must handle Probing via Infra.
// Prober interface to be injected into API
type Prober interface {
	Probe(ctx context.Context, path string) (*StreamInfo, error)
}

// CancelAll stops all active jobs.
func (m *Manager) CancelAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, h := range m.jobs {
		_ = h.Stop(2*time.Second, 5*time.Second)
		delete(m.jobs, id)
	}
}

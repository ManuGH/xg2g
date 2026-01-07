package vod

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// Manager handles concurrent VOD builds with exactly-once semantics
type Manager struct {
	mu   sync.Mutex
	runs map[string]*Run
	exec Exec
	log  zerolog.Logger
}

// NewManager creates a new VOD Manager
func NewManager(exec Exec, log zerolog.Logger) *Manager {
	// zerolog.Logger is a value type, cannot check for nil.
	// Caller receives Nop from log.L() if not init, or we can force Nop if empty?
	// Zero value of zerolog.Logger is usable (disabled).
	return &Manager{
		runs: make(map[string]*Run),
		exec: exec,
		log:  log,
	}
}

// Ensure guarantees that a background job for the given ID is running.
// If a job is already active, it returns the existing Run handle (isNew=false).
// If not, it starts a new job using the provided work function and returns the new handle (isNew=true).
//
// Stable API P0: This method remains unchanged and delegates to EnsureSpec.
func (m *Manager) Ensure(ctx context.Context, id string, work WorkFunc) (*Run, bool) {
	spec := JobSpec{
		ID:   id,
		Kind: "legacy",
	}
	workSpec := func(ctx context.Context, s JobSpec) error {
		return work(ctx)
	}
	return m.EnsureSpec(ctx, spec, workSpec)
}

// EnsureSpec provides structured job orchestration with observability (Phase C)
func (m *Manager) EnsureSpec(ctx context.Context, spec JobSpec, work WorkFuncSpec) (*Run, bool) {
	// 0. Fail-fast if context already canceled (P1)
	if err := ctx.Err(); err != nil {
		m.log.Debug().Str("id", spec.ID).Err(err).Msg("EnsureSpec: context already canceled")
		return nil, false
	}

	m.mu.Lock()

	// 1. Check existing & Stale Handle Detection (P0)
	if run, exists := m.runs[spec.ID]; exists {
		select {
		case <-run.Done:
			// Run is done but not yet deleted from map (stale).
			// Recreate it as a new run.
			m.log.Debug().Str("id", spec.ID).Msg("EnsureSpec: cleaning stale run")
			delete(m.runs, spec.ID)
		default:
			// Run is still active.
			m.mu.Unlock()
			m.log.Debug().Str("id", spec.ID).Msg("EnsureSpec: return existing run")
			return run, false
		}
	}

	// 2. Create new Run
	runCtx, cancel := context.WithCancel(context.Background())

	run := &Run{
		ID:        spec.ID,
		StartedAt: time.Now(),
		Done:      make(chan struct{}),
		Cancel:    cancel,
	}

	m.runs[spec.ID] = run
	m.log.Info().
		Str("id", spec.ID).
		Str("serviceRef", spec.ServiceRef).
		Str("kind", spec.Kind).
		Msg("EnsureSpec: started new run")

	// 3. Start Execute Goroutine (Unlocking BEFORE start - P1)
	m.mu.Unlock()
	go m.executeRunSpec(runCtx, run, spec, work)

	return run, true
}

// Get returns the active run for the given ID, or nil if not found
func (m *Manager) Get(id string) *Run {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runs[id]
}

// Cancel stops the run for the given ID
func (m *Manager) Cancel(id string) {
	m.mu.Lock()
	run, exists := m.runs[id]
	m.mu.Unlock()

	if exists {
		m.log.Info().Str("id", id).Msg("Cancel: stopping run")
		run.Cancel()
	}
}

// CancelAll stops all active runs (P9 Graceful Shutdown)
func (m *Manager) CancelAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.log.Info().Int("count", len(m.runs)).Msg("CancelAll: stopping all runs")
	for id, run := range m.runs {
		m.log.Debug().Str("id", id).Msg("CancelAll: canceling run")
		run.Cancel()
	}
}

// executeRunSpec is the worker goroutine
// Phase C: Uses JobSpec and thread-safe error setter
func (m *Manager) executeRunSpec(ctx context.Context, run *Run, spec JobSpec, work WorkFuncSpec) {
	defer func() {
		// Recover from panic to ensure cleanup (P1)
		if r := recover(); r != nil {
			m.log.Error().
				Str("id", run.ID).
				Interface("panic", r).
				Msg("executeRunSpec panicked")
			run.setError(fmt.Errorf("panic: %v", r))
		}

		// Close Done channel
		close(run.Done)

		// Remove from map
		m.mu.Lock()
		delete(m.runs, run.ID)
		m.mu.Unlock()

		m.log.Info().
			Str("id", run.ID).
			Err(run.Error()).
			Msg("executeRunSpec: cleanup complete")
	}()

	// Execute Work
	err := work(ctx, spec)
	if err != nil {
		run.setError(err)
	}
}

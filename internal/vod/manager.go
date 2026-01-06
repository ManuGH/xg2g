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
func (m *Manager) Ensure(ctx context.Context, id string, work WorkFunc) (*Run, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1. Check existing
	if run, exists := m.runs[id]; exists {
		m.log.Debug().Str("id", id).Msg("Ensure: return existing run")
		return run, false
	}

	// 2. Create new Run
	// Note: We use a detached context for the run itself so it survives the caller's request context
	// unless explicitly cancelled by manager.
	runCtx, cancel := context.WithCancel(context.Background())

	run := &Run{
		ID:        id,
		StartedAt: time.Now(),
		Done:      make(chan struct{}),
		Cancel:    cancel,
	}

	m.runs[id] = run
	m.log.Info().Str("id", id).Msg("Ensure: started new run")

	// 3. Start Execute Goroutine
	go m.executeRun(runCtx, run, work)

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

// executeRun is the worker goroutine
// Guardrail: Must ensure cleanup (delete from map) on exit
func (m *Manager) executeRun(ctx context.Context, run *Run, work WorkFunc) {
	// Guardrail 1: Guarantee cleanup
	defer func() {
		// Recover from panic to ensure cleanup
		if r := recover(); r != nil {
			m.log.Error().Str("id", run.ID).Interface("panic", r).Msg("executeRun panicked")
			run.Err = fmt.Errorf("panic: %v", r)
		}

		// Close Done channel
		close(run.Done)

		// Remove from map
		m.mu.Lock()
		delete(m.runs, run.ID)
		m.mu.Unlock()

		m.log.Info().Str("id", run.ID).Err(run.Err).Msg("executeRun: cleanup complete")
	}()

	// Execute Work
	// The work function encapsulates the logic (HLS build or MP4 remux)
	err := work(ctx)
	if err != nil {
		run.Err = err
	}
}

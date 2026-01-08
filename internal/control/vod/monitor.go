package vod

import (
	"context"
	"sync"
	"sync/atomic"
)

// BuildMonitorConfig configures a build monitor instance.
type BuildMonitorConfig struct {
	JobID     string
	Spec      Spec
	FinalPath string // Final output path for atomic publish
	Runner    Runner
	Clock     Clock
	FS        FS // Filesystem operations (injectable for tests)
}

// BuildMonitor supervises a single VOD build job with deterministic timeout enforcement.
type BuildMonitor struct {
	mu        sync.RWMutex
	jobID     string
	spec      Spec
	finalPath string
	runner    Runner
	clock     Clock
	fs        FS

	state  State
	reason FailureReason
	handle Handle

	stopOnce sync.Once // Ensures Stop is called at most once
}

// NewBuildMonitor creates a new build monitor.
func NewBuildMonitor(cfg BuildMonitorConfig) *BuildMonitor {
	fs := cfg.FS
	if fs == nil {
		fs = RealFS{} // Default to real filesystem
	}

	return &BuildMonitor{
		jobID:     cfg.JobID,
		spec:      cfg.Spec,
		finalPath: cfg.FinalPath,
		runner:    cfg.Runner,
		clock:     cfg.Clock,
		fs:        fs,
		state:     StateIdle,
	}
}

// Run executes the monitoring loop until terminal state.
func (m *BuildMonitor) Run(ctx context.Context) {
	//  Ensure cleanup on all exit paths
	defer m.cleanup()

	// Transition to Building and start the process
	m.setState(StateBuilding, "")

	handle, err := m.runner.Start(ctx, m.spec)
	if err != nil {
		m.setState(StateFailed, ReasonStartFail)
		return
	}

	m.mu.Lock()
	m.handle = handle
	m.mu.Unlock()

	// Monitor loop with heartbeat timeout
	lastSeen := m.clock.Now()
	timeoutCh := m.clock.After(HeartbeatTimeout)

	// Atomic flag to prevent double-stop
	var stopCalled atomic.Bool

	for {
		select {
		case <-ctx.Done():
			m.stopHandle(&stopCalled)
			m.setState(StateCanceled, ReasonCanceled)
			return

		case <-handle.Progress():
			// Progress received - reset timeout
			lastSeen = m.clock.Now()
			timeoutCh = m.clock.After(HeartbeatTimeout)

		case <-timeoutCh:
			// Heartbeat timeout - stall detected
			elapsed := m.clock.Now().Sub(lastSeen)
			if elapsed >= HeartbeatTimeout {
				m.stopHandle(&stopCalled)
				m.setState(StateFailed, ReasonStall)
				return
			}
			// False alarm, re-arm
			timeoutCh = m.clock.After(HeartbeatTimeout)

		case err := <-m.waitAsync(handle):
			// Process exited
			if err != nil {
				m.stopHandle(&stopCalled)
				m.setState(StateFailed, ReasonCrash)
				return
			}

			// Success - atomically publish
			if m.finalPath != "" {
				m.setState(StateFinalizing, "")
				if err := m.publish(); err != nil {
					m.setState(StateFailed, ReasonInternal)
					return
				}
			}

			m.setState(StateSucceeded, "")
			return
		}
	}
}

// cleanup attempts to remove WorkDir on failure (best-effort).
func (m *BuildMonitor) cleanup() {
	status := m.GetStatus()
	if status.State == StateSucceeded {
		// On success, keep WorkDir or let caller decide
		// (or optionally clean temp files, but not final output)
		return
	}

	// On any failure/cancel, attempt to clean WorkDir
	if m.spec.WorkDir != "" {
		_ = m.fs.RemoveAll(m.spec.WorkDir)
	}
}

// publish atomically moves OutputTemp to FinalPath.
func (m *BuildMonitor) publish() error {
	oldPath := m.spec.WorkDir + "/" + m.spec.OutputTemp
	return m.fs.Rename(oldPath, m.finalPath)
}

// waitAsync wraps Wait() in a goroutine to make it select-able.
func (m *BuildMonitor) waitAsync(h Handle) <-chan error {
	ch := make(chan error, 1)
	go func() {
		ch <- h.Wait()
	}()
	return ch
}

// stopHandle calls Stop() exactly once using sync.Once pattern.
func (m *BuildMonitor) stopHandle(called *atomic.Bool) {
	if called.Swap(true) {
		return // Already stopped
	}

	m.mu.RLock()
	h := m.handle
	m.mu.RUnlock()

	if h != nil {
		_ = h.Stop(StopGrace, KillDelay)
	}
}

// setState transitions to a new state atomically.
func (m *BuildMonitor) setState(state State, reason FailureReason) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = state
	m.reason = reason
}

// MonitorStatus represents internal monitor state (not the public JobStatus DTO).
type MonitorStatus struct {
	State  State
	Reason FailureReason
}

// GetStatus returns the current monitor status.
func (m *BuildMonitor) GetStatus() MonitorStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return MonitorStatus{
		State:  m.state,
		Reason: m.reason,
	}
}

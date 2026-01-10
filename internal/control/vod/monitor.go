package vod

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// BuildMonitorConfig configures a build monitor instance.
type BuildMonitorConfig struct {
	JobID     string
	Spec      Spec
	FinalPath string // Final output path for atomic publish
	Runner    Runner
	Clock     Clock
	FS        FS // Filesystem operations (injectable for tests)

	// Completion callbacks (optional). Must be fast, no I/O, and non-blocking.
	// Called exactly once per Run() execution path.
	OnSucceeded func(jobID string, spec Spec, finalPath string)
	OnFailed    func(jobID string, spec Spec, finalPath string, reason string)
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

	stopOnce    sync.Once // Ensures Stop is called at most once
	notifyOnce  sync.Once // Ensures completion callbacks called at most once
	onSucceeded func(jobID string, spec Spec, finalPath string)
	onFailed    func(jobID string, spec Spec, finalPath string, reason string)
}

// NewBuildMonitor creates a new build monitor.
func NewBuildMonitor(cfg BuildMonitorConfig) *BuildMonitor {
	fs := cfg.FS
	if fs == nil {
		fs = RealFS{} // Default to real filesystem
	}

	return &BuildMonitor{
		jobID:       cfg.JobID,
		spec:        cfg.Spec,
		finalPath:   cfg.FinalPath,
		runner:      cfg.Runner,
		clock:       cfg.Clock,
		fs:          fs,
		state:       StateIdle,
		onSucceeded: cfg.OnSucceeded,
		onFailed:    cfg.OnFailed,
	}
}

// notifySuccess calls the success callback exactly once.
func (m *BuildMonitor) notifySuccess() {
	log.Debug().Str("jobId", m.jobID).Str("finalPath", m.finalPath).Msg("VOD monitor: notifySuccess called")
	m.notifyOnce.Do(func() {
		log.Debug().Str("jobId", m.jobID).Bool("hasCallback", m.onSucceeded != nil).Msg("VOD monitor: notifySuccess executing")
		if m.onSucceeded != nil {
			m.onSucceeded(m.jobID, m.spec, m.finalPath)
			log.Debug().Str("jobId", m.jobID).Msg("VOD monitor: success callback completed")
		}
	})
}

// notifyFailure calls the failure callback exactly once.
func (m *BuildMonitor) notifyFailure(reason string) {
	log.Debug().Str("jobId", m.jobID).Str("reason", reason).Msg("VOD monitor: notifyFailure called")
	m.notifyOnce.Do(func() {
		log.Debug().Str("jobId", m.jobID).Bool("hasCallback", m.onFailed != nil).Msg("VOD monitor: notifyFailure executing")
		if m.onFailed != nil {
			m.onFailed(m.jobID, m.spec, m.finalPath, reason)
			log.Debug().Str("jobId", m.jobID).Msg("VOD monitor: failure callback completed")
		}
	})
}

// Run executes the monitoring loop until terminal state.
func (m *BuildMonitor) Run(ctx context.Context) {
	//  Ensure cleanup on all exit paths
	defer m.cleanup()

	// Ensure work directory exists (monitor-owned; runner contract is OutputTemp).
	if m.spec.WorkDir != "" {
		if err := m.fs.MkdirAll(m.spec.WorkDir, 0755); err != nil {
			m.setState(StateFailed, "MKDIR_FAIL")
			m.notifyFailure("MKDIR_FAIL")
			return
		}
	}

	handle, err := m.runner.Start(ctx, m.spec)
	if err != nil {
		log.Error().Err(err).Str("jobId", m.jobID).Msg("VOD build runner failed to start")
		m.setState(StateFailed, ReasonStartFail)
		m.notifyFailure(string(ReasonStartFail))
		return
	}

	m.mu.Lock()
	m.handle = handle
	m.mu.Unlock()

	if err := m.waitForRunnerArtifacts(ctx, BuildStartTimeout); err != nil {
		if errors.Is(err, context.Canceled) {
			_ = m.StopGracefully()
			m.setState(StateCanceled, ReasonCanceled)
			m.notifyFailure(string(ReasonCanceled))
			return
		}
		log.Error().Err(err).Str("jobId", m.jobID).Msg("VOD build runner contract violation")
		_ = m.StopGracefully()
		m.setState(StateFailed, ReasonContract)
		m.notifyFailure(string(ReasonContract))
		return
	}

	// Transition to Building only after runner artifacts exist
	m.setState(StateBuilding, "")
	log.Debug().Str("jobId", m.jobID).Str("input", m.spec.Input).Msg("VOD build monitor started")

	// Monitor loop with heartbeat timeout
	lastSeen := m.clock.Now()
	timeoutCh := m.clock.After(HeartbeatTimeout)
	waitCh := m.waitAsync(handle) // Call once before loop to avoid creating new channels on each iteration

	log.Debug().Str("jobId", m.jobID).Msg("VOD monitor: entering select loop")

	for {
		select {
		case <-ctx.Done():
			_ = m.StopGracefully()
			m.setState(StateCanceled, ReasonCanceled)
			m.notifyFailure(string(ReasonCanceled))
			return

		case <-handle.Progress():
			// Progress received - reset timeout
			log.Debug().Str("jobId", m.jobID).Msg("VOD monitor: progress event received")
			lastSeen = m.clock.Now()
			timeoutCh = m.clock.After(HeartbeatTimeout)

		case <-timeoutCh:
			// Heartbeat timeout - stall detected
			elapsed := m.clock.Now().Sub(lastSeen)
			if elapsed >= HeartbeatTimeout {
				_ = m.StopGracefully()
				m.setState(StateFailed, ReasonStall)
				m.notifyFailure(string(ReasonStall))
				return
			}
			// False alarm, re-arm
			timeoutCh = m.clock.After(HeartbeatTimeout)

		case err := <-waitCh:
			// Process exited
			log.Debug().Str("jobId", m.jobID).Err(err).Msg("VOD monitor: process exited, examining error")
			if err != nil {
				log.Error().Err(err).Str("jobId", m.jobID).Strs("diagnostics", handle.Diagnostics()).Msg("VOD build process failed")
				_ = m.StopGracefully()
				m.setState(StateFailed, ReasonCrash)
				m.notifyFailure(string(ReasonCrash))
				return
			}

			log.Info().Str("jobId", m.jobID).Msg("VOD build process completed successfully")

			// Success - atomically publish
			if m.finalPath != "" {
				log.Debug().Str("jobId", m.jobID).Str("finalPath", m.finalPath).Msg("VOD monitor: finalPath set, attempting publish")
				m.setState(StateFinalizing, "")
				if err := m.publish(); err != nil {
					log.Error().Err(err).Str("jobId", m.jobID).Str("finalPath", m.finalPath).Msg("VOD build publish failed")
					m.setState(StateFailed, ReasonInternal)
					m.notifyFailure(string(ReasonInternal))
					return
				}
				log.Info().Str("jobId", m.jobID).Str("finalPath", m.finalPath).Msg("VOD build published successfully")
			} else {
				log.Debug().Str("jobId", m.jobID).Msg("VOD monitor: finalPath empty, skipping publish")
			}

			log.Debug().Str("jobId", m.jobID).Msg("VOD monitor: about to call setState(StateSucceeded)")
			m.setState(StateSucceeded, "")
			log.Debug().Str("jobId", m.jobID).Msg("VOD monitor: about to call notifySuccess()")
			m.notifySuccess()
			log.Debug().Str("jobId", m.jobID).Msg("VOD monitor: notifySuccess() returned, exiting Run()")
			return
		}
	}
}

func (m *BuildMonitor) waitForRunnerArtifacts(ctx context.Context, timeout time.Duration) error {
	if strings.TrimSpace(m.spec.WorkDir) == "" || strings.TrimSpace(m.spec.OutputTemp) == "" {
		return errors.New("runner contract violation: workdir/output temp empty")
	}

	outputPath := filepath.Join(m.spec.WorkDir, m.spec.OutputTemp)
	deadline := time.NewTimer(timeout)
	ticker := time.NewTicker(25 * time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()

	for {
		ready, err := m.runnerArtifactsReady(outputPath)
		if err != nil {
			return err
		}
		if ready {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("runner contract violation: output temp missing")
		case <-ticker.C:
		}
	}
}

func (m *BuildMonitor) runnerArtifactsReady(outputPath string) (bool, error) {
	if _, err := m.fs.Stat(m.spec.WorkDir); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	if _, err := m.fs.Stat(outputPath); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	return true, nil
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
	oldPath := filepath.Join(m.spec.WorkDir, m.spec.OutputTemp)
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

// Stop terminates the build job gracefully then forcefully.
func (m *BuildMonitor) Stop(grace, kill time.Duration) error {
	m.stopOnce.Do(func() {
		m.mu.RLock()
		h := m.handle
		m.mu.RUnlock()

		if h != nil {
			_ = h.Stop(grace, kill)
		}
	})
	return nil
}

// StopGracefully is a helper for default cleanup.
func (m *BuildMonitor) StopGracefully() error {
	return m.Stop(StopGrace, KillDelay)
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

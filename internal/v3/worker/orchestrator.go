// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/v3/bus"
	"github.com/ManuGH/xg2g/internal/v3/exec"
	"github.com/ManuGH/xg2g/internal/v3/model"
	"github.com/ManuGH/xg2g/internal/v3/store"
	"github.com/google/uuid"
)

// Phase 9-4: Metrics defined in metrics.go
// jobsTotal -> tunerBusyTotal (subset)
// fsmTransitions -> fsmTransitions (kept)
// leaseLostTotal -> leaseLostTotalLegacy (kept/aliased)

// Orchestrator consumes intents and drives pipelines. It is intentionally side-effecting,
// and MUST be out-of-band from HTTP request paths.
//
// MVP:
//   - acquire a single-writer lease per serviceKey
//   - transition Session: STARTING -> READY/FAILED
//   - (placeholder) for receiver tuning + ffmpeg lifecycle
type Orchestrator struct {
	Store store.StateStore
	Bus   bus.Bus

	LeaseTTL       time.Duration
	HeartbeatEvery time.Duration
	VirtualMode    bool   // If true, mocks hardware/ffmpeg
	Owner          string // Stable worker identity
	TunerSlots     []int  // Available hardware slots
	HLSRoot        string // Root directory for HLS segments
	Sweeper        SweeperConfig

	ExecFactory  exec.Factory
	LeaseKeyFunc func(model.StartSessionEvent) string

	// Phase 9-2: Lifecycle Management
	mu     sync.Mutex
	active map[string]context.CancelFunc
}

func (o *Orchestrator) Run(ctx context.Context) error {
	if o.LeaseTTL <= 0 {
		o.LeaseTTL = 30 * time.Second
	}
	if o.HeartbeatEvery <= 0 {
		o.HeartbeatEvery = 10 * time.Second
	}
	if o.Owner == "" {
		host, _ := os.Hostname()
		o.Owner = fmt.Sprintf("%s-%d-%s", host, os.Getpid(), uuid.New().String())
	}
	if o.ExecFactory == nil {
		o.ExecFactory = &exec.StubFactory{}
	}
	if o.LeaseKeyFunc == nil {
		o.LeaseKeyFunc = func(e model.StartSessionEvent) string {
			return LeaseKeyService(e.ServiceRef)
		}
	}

	if o.active == nil {
		o.active = make(map[string]context.CancelFunc)
	}

	// Subscribe to Start AND Stop events
	// Note: In a real bus, we might need multiple subscriptions or a wildcard.
	// Assuming local bus supports strict topic match, we subscribe to both.
	subStart, err := o.Bus.Subscribe(ctx, string(model.EventStartSession))
	if err != nil {
		return err
	}
	defer func() { _ = subStart.Close() }()

	subStop, err := o.Bus.Subscribe(ctx, string(model.EventStopSession))
	if err != nil {
		return err
	}
	defer func() { _ = subStop.Close() }()

	// Phase 7B-3: Recovery Sweep on Startup
	if err := o.recoverStaleLeases(ctx); err != nil {
		// Log but don't crash? Or crash to protect integrity?
		// Plan: "RecoverStaleLeases(owner, now)"
		// If DB scan fails, maybe we should retry or fail.
		// For now, logging error is safest to avoid boot loops if store is flaky,
		// but failure means likely DB issues anyway.
		// Let's return error to be safe (fail fast).
		return fmt.Errorf("recovery sweep failed: %w", err)
	}

	// Launch Background Sweeper (PR 9-3)
	sweeper := &Sweeper{Orch: o, Conf: o.Sweeper}
	if sweeper.Conf.Interval == 0 {
		if sweeper.Conf.IdleTimeout > 0 {
			sweeper.Conf.Interval = sweeper.Conf.IdleTimeout / 2
			if sweeper.Conf.Interval < 10*time.Second {
				sweeper.Conf.Interval = 10 * time.Second
			}
		} else {
			sweeper.Conf.Interval = 5 * time.Minute
		}
	}
	if sweeper.Conf.SessionRetention == 0 {
		sweeper.Conf.SessionRetention = 24 * time.Hour
	}
	go sweeper.Run(ctx)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-subStart.C():
			if !ok {
				return errors.New("event channel closed")
			}
			if evt, ok := msg.(model.StartSessionEvent); ok {
				// Async handle to allow multiple concurrent sessions
				// Fix 1: Use derived context from Run
				go func(e model.StartSessionEvent) {
					if err := o.handleStart(ctx, e); err != nil {
						log.L().Error().Err(err).Str("sid", e.SessionID).Msg("session start failed")
					}
				}(evt)
			}
		case msg, ok := <-subStop.C():
			if !ok {
				return errors.New("event channel closed")
			}
			if evt, ok := msg.(model.StopSessionEvent); ok {
				// Fix 1: Use derived context from Run
				go func(e model.StopSessionEvent) {
					if err := o.handleStop(ctx, e); err != nil {
						log.L().Error().Err(err).Str("sid", e.SessionID).Msg("session stop failed")
					}
				}(evt)
			}
		}
	}
}

func (o *Orchestrator) handleStart(ctx context.Context, e model.StartSessionEvent) (retErr error) {
	started := false

	// 0. Unified Finalization (Always Runs)
	// Critical Fix 9-4: Must run even if !started (e.g. tune fail, lease fail).
	defer func() {
		// Guard: If we returned nil (no error) but never started,
		// it means we decided to backoff (Busy/Dedup).
		// In this case, LEAVE STATE AS NEW. Do not finalize.
		if retErr == nil && !started {
			return
		}
		// Calculate final metrics
		// Determine Outcome
		finalState := model.SessionFailed
		reason := model.RProcessEnded
		detail := ""

		if retErr == nil {
			// Clean exit of Wait() logic
			if ctx.Err() != nil {
				// Context cancelled -> Expected Stop (Client Stop or Timeout)
				finalState = model.SessionStopped
				reason = model.RClientStop
			} else {
				// Context valid -> Spontaneous Exit (e.g. End of Stream, Crash, or Early Exit)
				// Fix 11-2: Treat unrequested exit as abnormal termination.
				finalState = model.SessionFailed
				reason = model.RProcessEnded // "Process ended unexpectedly"
			}
		} else {
			if errors.Is(retErr, context.Canceled) {
				finalState = model.SessionStopped
				reason = model.RClientStop
			} else if errors.Is(retErr, context.DeadlineExceeded) {
				reason = model.RTuneTimeout
				detail = retErr.Error()
			} else {
				// Fallback to generic or specific based on string
				// MVP: If error is not nil, it's likely a failure.
				// If we haven't mapped it, using RUnknown or keeping RFFmpegExited default?
				// RFFmpegExited implies process ran. If we failed before that (Tune), it is wrong.
				// Use RGenericError or similar? Or RUnknown.
				// User requested: "Tune-Fehler => RTuneFailed".
				// We can check string?
				if detail == "" {
					detail = retErr.Error()
				}
				reason = model.RUnknown // Or R_EXECUTION_FAILED?
				// Let's use RUnknown as safe default, or RTuneFailed if we are in tuning phase?
				// Hard to know phase here easily without state.
				// But we are in finalizer.
			}
		}

		// Update Store
		_, _ = o.Store.UpdateSession(context.Background(), e.SessionID, func(r *model.SessionRecord) error {
			// If already terminal (e.g. STOPPED via handleStop?), respect it?
			// But handleStop only sets STOPPING.
			if r.State.IsTerminal() && r.State != model.SessionStopping {
				return nil
			}

			// Metric: From -> To
			o.recordTransition(r.State, finalState)

			// Update
			r.State = finalState
			if finalState == model.SessionFailed {
				r.PipelineState = model.PipeFail
			} else {
				r.PipelineState = model.PipeStopped
			}
			// Don't overwrite granular reason if already set?
			if r.Reason == "" || r.Reason == model.RNone || r.Reason == model.RUnknown {
				r.Reason = reason
			}
			if detail != "" {
				r.ReasonDetail = detail
			}
			r.UpdatedAtUnix = time.Now().Unix()
			return nil
		})

		// PR 9-3: On-Stop Cleanup
		o.cleanupFiles(e.SessionID)

		// Phase 9-4: Golden Signals (Session End)
		mode := o.modeLabel()
		sessionEndTotal.WithLabelValues(string(reason), e.ProfileID, mode).Inc()

		logEvt := log.L().Info().
			Str("event", "hls.session_end").
			Str("sid", e.SessionID).
			Str("reason", string(reason)).
			Str("profile", e.ProfileID).
			Str("mode", mode)

		if detail != "" {
			logEvt.Str("detail", detail)
		}
		// If we had a transcoder, we might have exit code, bytes, etc.
		// For now, MVP log.
		logEvt.Msg("session ended")
	}()

	// 1. Dedup Lock (ServiceRef) - Transient (Phase 8-2)
	// We acquire this to prevent stampede during startup, but we don't hold it long-term.
	dedupKey := o.LeaseKeyFunc(e)
	dedupLease, ok, err := o.Store.TryAcquireLease(ctx, dedupKey, o.Owner, o.LeaseTTL)
	if err != nil {
		return err
	}
	if !ok {
		// jobsTotal.WithLabelValues("lease_conflict", o.modeLabel()).Inc()
		return nil
	}
	// Release Dedup Lock immediately after critical section (Transient)
	// We only hold it to linearize setup for the same service.
	// Fix: Do NOT defer to function end (session end).
	releaseDedup := func() {
		ctxRel, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = o.Store.ReleaseLease(ctxRel, dedupLease.Key(), dedupLease.Owner())
	}
	defer releaseDedup() // Safety fallback (idempotent)

	// We will call releaseDedup() explicitly once we have successfully transitioned or failed.

	// 2. Resource Lock (Tuner Slot) - Persistent (Phase 8-2)
	slot, tunerLease, ok, err := o.acquireTunerLease(ctx, o.TunerSlots)
	if err != nil {
		return err
	}
	if !ok {
		// All slots busy
		tunerBusyTotal.WithLabelValues(o.modeLabel()).Inc()
		// We do NOT fail the session, we just don't start it yet.
		// It remains in NEW state (or QUEUED if we had it).
		// Client/Shadow will retry.
		return nil
	}
	defer func() {
		ctxRel, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = o.Store.ReleaseLease(ctxRel, tunerLease.Key(), tunerLease.Owner())
	}()

	// Heartbeat loop: Renew TUNER Lease explicitly
	hbCtx, hbCancel := context.WithCancel(ctx)
	// Phase 9-2: Lifecycle Management (Store CancelFunc)
	o.registerActive(e.SessionID, hbCancel)
	defer o.unregisterActive(e.SessionID)
	// We also defer hbCancel to ensure resources are freed if we panic or return early
	defer hbCancel()

	go func() {
		t := time.NewTicker(o.HeartbeatEvery)
		defer t.Stop()
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-t.C:
				// Renew Tuner Lease (Risk 8-2: Must probe correct lease)
				_, ok, err := o.Store.RenewLease(hbCtx, tunerLease.Key(), tunerLease.Owner(), o.LeaseTTL)
				if err != nil {
					log.L().Warn().Err(err).Msg("heartbeat renewal error")
				} else if !ok {
					log.L().Warn().Str("lease", tunerLease.Key()).Str("sid", e.SessionID).Msg("tuner lease lost, aborting")
					leaseLostTotalLegacy.WithLabelValues(o.modeLabel()).Inc()

					// Fix 11-3: Lease Robustness
					// Explicitly attempt to mark FAILED before cancelling context.
					// This ensures the session is terminal even if finalizer fails later (e.g. race).
					// Best-effort push.
					_, _ = o.Store.UpdateSession(hbCtx, e.SessionID, func(r *model.SessionRecord) error {
						if !r.State.IsTerminal() {
							r.State = model.SessionFailed
							r.Reason = model.RLeaseExpired
							r.UpdatedAtUnix = time.Now().Unix()
						}
						return nil
					})

					hbCancel()
					return
				}
			}
		}
	}()

	// 1. Transition to STARTING (Store Tuner Slot)
	o.recordTransition(model.SessionUnknown, model.SessionStarting)
	_, err = o.Store.UpdateSession(ctx, e.SessionID, func(r *model.SessionRecord) error {
		// Guard: If somebody (handleStop) already marked it STOPPING or Terminal, abort start
		if r.State.IsTerminal() || r.State == model.SessionStopping {
			return fmt.Errorf("session state %s, aborting start", r.State)
		}
		r.State = model.SessionStarting
		r.UpdatedAtUnix = time.Now().Unix()
		if r.ContextData == nil {
			r.ContextData = make(map[string]string)
		}
		r.ContextData[model.CtxKeyTunerSlot] = strconv.Itoa(slot)
		return nil
	})
	if err != nil {
		jobsTotal.WithLabelValues("failed_starting", o.modeLabel()).Inc()
		return err
	}
	// started = true // Removed in Phase 9-4

	// 2. Perform Work (Execution Contracts)
	tuner, err := o.ExecFactory.NewTuner(slot)
	if err != nil {
		// jobsTotal.WithLabelValues("exec_error", o.modeLabel()).Inc()
		return err
	}
	defer func() { _ = tuner.Close() }()

	// Measure Ready Duration
	readyStart := time.Now()
	log.L().Info().Str("ref", e.ServiceRef).Msg("worker: starting tune")
	tuneErr := tuner.Tune(hbCtx, e.ServiceRef)
	readyDurationVal := time.Since(readyStart).Seconds()

	// Classify Outcome
	var outcome string
	if tuneErr == nil {
		outcome = "success"
		log.L().Info().Msg("worker: tune success")
	} else {
		outcome = "failure"
		log.L().Error().Err(tuneErr).Msg("worker: tune failed")
		// Classify failure for counter
		failReason := "other"
		if errors.Is(tuneErr, context.DeadlineExceeded) {
			failReason = "timeout"
		} else if errors.Is(tuneErr, context.Canceled) {
			failReason = "canceled"
		}
		// Detailed breakdown
		readyOutcomeTotal.WithLabelValues(failReason, o.modeLabel()).Inc()
	}
	readyDuration.WithLabelValues(outcome, o.modeLabel()).Observe(readyDurationVal)

	if tuneErr != nil {
		// Tuner failed, but we still run finalizer.
		// We should explicitly set retErr so finalizer sees failure.
		// (It is set by return)
		// jobsTotal.WithLabelValues("tune_failed", o.modeLabel()).Inc()
		return tuneErr
	}

	// Ready Success Counter
	readyOutcomeTotal.WithLabelValues("success", o.modeLabel()).Inc()

	transcoder, err := o.ExecFactory.NewTranscoder()
	if err != nil {
		return err
	}

	// Fetch session to get ProfileSpec with DVR window configuration
	session, err := o.Store.GetSession(ctx, e.SessionID)
	if err != nil {
		return fmt.Errorf("failed to get session for profile: %w", err)
	}
	if session == nil {
		return fmt.Errorf("session not found: %s", e.SessionID)
	}

	ffmpegStartTime := time.Now()
	if err := transcoder.Start(hbCtx, e.SessionID, e.ServiceRef, session.Profile); err != nil {
		return err
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = transcoder.Stop(stopCtx)
	}()

	// 3. Wait for Playlist to be Servable
	// Fix: Gate READY transition on playlist existence to avoid race condition
	// where clients see READY state but get 404 on playlist requests.
	//
	// Background: FFmpeg needs time to:
	// - Connect to upstream and probe stream (~2s analyzeduration)
	// - Generate first HLS segment (~2s hls_time)
	// - Write index.m3u8.tmp and have sync loop promote it to index.m3u8 (~200ms poll)
	//
	// We wait for index.m3u8 (not .tmp) because that's what clients request.
	// Skip this check if HLSRoot is not configured (e.g., in tests with stub factory)
	if o.HLSRoot != "" {
		playlistPath := filepath.Join(o.HLSRoot, "sessions", e.SessionID, "index.m3u8")
		playlistReadyTimeout := 10 * time.Second
		playlistPollInterval := 200 * time.Millisecond
		playlistDeadline := time.Now().Add(playlistReadyTimeout)

		log.L().Info().
			Str("session_id", e.SessionID).
			Str("service_ref", e.ServiceRef).
			Str("playlist_path", playlistPath).
			Dur("timeout", playlistReadyTimeout).
			Msg("waiting for playlist to become ready")

		for {
			// Success condition: file exists, non-empty, and contains #EXTM3U header
			if info, statErr := os.Stat(playlistPath); statErr == nil && info.Size() > 0 {
				// Sanity check: validate it's a real HLS manifest
				// #nosec G304 -- playlistPath is constructed from trusted hlsDir, not user input
				if content, readErr := os.ReadFile(playlistPath); readErr == nil {
					if strings.Contains(string(content), "#EXTM3U") {
						break // Playlist ready!
					}
					log.L().Debug().
						Str("session_id", e.SessionID).
						Msg("playlist exists but missing #EXTM3U header, continuing to poll")
				}
			}

			// Termination condition: timeout
			if time.Now().After(playlistDeadline) {
				elapsedMs := time.Since(ffmpegStartTime).Milliseconds()
				log.L().Error().
					Str("session_id", e.SessionID).
					Str("service_ref", e.ServiceRef).
					Str("playlist_path", playlistPath).
					Dur("timeout", playlistReadyTimeout).
					Int64("elapsed_ms", elapsedMs).
					Msg("playlist not ready after timeout - failing session")
				// Return error to trigger finalizer which will set FAILED state
				return fmt.Errorf("playlist not ready after %s (upstream dead or no packets received)", playlistReadyTimeout)
			}

			// Termination condition: context cancelled
			select {
			case <-hbCtx.Done():
				return hbCtx.Err()
			case <-time.After(playlistPollInterval):
				// Continue polling
			}
		}

		playlistReadyDuration := time.Since(ffmpegStartTime)
		log.L().Info().
			Str("session_id", e.SessionID).
			Str("service_ref", e.ServiceRef).
			Int64("elapsed_ms", playlistReadyDuration.Milliseconds()).
			Msg("playlist ready - transitioning to READY state")
	}

	// 4. Update READY
	// Now it's safe to declare READY because playlist is servable
	o.recordTransition(model.SessionStarting, model.SessionReady)
	_, err = o.Store.UpdateSession(ctx, e.SessionID, func(r *model.SessionRecord) error {
		r.State = model.SessionReady
		r.UpdatedAtUnix = time.Now().Unix()
		r.LastAccessUnix = time.Now().Unix()
		return nil
	})
	if err != nil {
		return err
	}
	started = true // Session is now active/starting.

	// 4. Wait
	// Fix 8-2: Release Setup Lock *before* blocking.
	// We are now in a stable state (READY).
	releaseDedup()

	_, waitErr := transcoder.Wait(hbCtx)
	return waitErr
}

func (o *Orchestrator) handleStop(ctx context.Context, e model.StopSessionEvent) error {
	// 1. Always attempt store update (Idempotency)
	var shortCircuitCleanup bool
	_, err := o.Store.UpdateSession(ctx, e.SessionID, func(r *model.SessionRecord) error {
		if r.State.IsTerminal() {
			return nil
		}
		// Fix 1: Hard logic gap. If session is NEW (never started), we can finalize it immediately.
		if r.State == model.SessionNew {
			r.State = model.SessionStopped
			r.PipelineState = model.PipeStopped
			r.Reason = e.Reason
			r.UpdatedAtUnix = time.Now().Unix()
			shortCircuitCleanup = true
			return nil
		}

		// Move to STOPPING. The active worker (if any) will see this and exit.
		r.State = model.SessionStopping
		r.PipelineState = model.PipeStopRequested
		r.Reason = e.Reason
		r.UpdatedAtUnix = time.Now().Unix()
		return nil
	})

	if shortCircuitCleanup {
		o.cleanupFiles(e.SessionID)
	}
	if err != nil {
		return err
	}

	// 2. Trigger Cancellation if active locally
	o.mu.Lock()
	cancel, ok := o.active[e.SessionID]
	o.mu.Unlock()

	if ok {
		cancel()
	}

	return nil
}

func (o *Orchestrator) registerActive(id string, cancel context.CancelFunc) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.active == nil {
		o.active = make(map[string]context.CancelFunc)
	}
	o.active[id] = cancel
}

func (o *Orchestrator) unregisterActive(id string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.active, id)
}

func (o *Orchestrator) acquireTunerLease(ctx context.Context, slots []int) (slot int, lease store.Lease, ok bool, err error) {
	for _, s := range slots {
		k := LeaseKeyTunerSlot(s)
		l, got, e := o.Store.TryAcquireLease(ctx, k, o.Owner, o.LeaseTTL)
		if e != nil {
			return 0, nil, false, e
		}
		if got {
			return s, l, true, nil
		}
	}
	return 0, nil, false, nil
}

func (o *Orchestrator) modeLabel() string {
	if o.VirtualMode {
		return "virtual"
	}
	return "standard"
}

func (o *Orchestrator) recordTransition(from, to model.SessionState) {
	fsmTransitions.WithLabelValues(string(from), string(to), o.modeLabel()).Inc()
}

func (o *Orchestrator) cleanupFiles(sid string) {
	if o.HLSRoot == "" {
		return
	}
	if !model.IsSafeSessionID(sid) {
		log.L().Warn().Str("sid", sid).Msg("refusing to cleanup unsafe session ID")
		return
	}
	// Path confinement check
	targetDir := filepath.Join(o.HLSRoot, "sessions", sid)
	// We trust filepath.Join to not escape if inputs are safe, but checking Abs/Clean is good practice.
	// Since we regex validated sid to alphanumeric, directory traversal is impossible.

	// Check if exists before removing? RemoveAll handles non-existence fine.
	if err := os.RemoveAll(targetDir); err != nil {
		log.L().Error().Err(err).Str("path", targetDir).Msg("failed to remove session directory")
	}
}

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
	"github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/ManuGH/xg2g/internal/pipeline/exec"
	"github.com/ManuGH/xg2g/internal/pipeline/lease"
	"github.com/ManuGH/xg2g/internal/pipeline/model"
	"github.com/ManuGH/xg2g/internal/pipeline/store"
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
	Owner          string // Stable worker identity
	TunerSlots     []int  // Available hardware slots
	HLSRoot        string // Root directory for HLS segments
	Sweeper        SweeperConfig

	ExecFactory  exec.Factory
	LeaseKeyFunc func(model.StartSessionEvent) string

	FFmpegKillTimeout time.Duration

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
	if o.LeaseKeyFunc == nil {
		o.LeaseKeyFunc = func(e model.StartSessionEvent) string {
			return lease.LeaseKeyService(e.ServiceRef)
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

	// Phase 8-2b: Flush Stale Leases (Restart Handling)
	// Since we are the exclusive worker (using file-lock on DB), any existing leases are from dead processes.
	// We must clear them to avoid "stiff arming" ourselves for TTL duration.
	if count, err := o.Store.DeleteAllLeases(ctx); err != nil {
		log.L().Error().Err(err).Msg("failed to flush old leases on startup, continuing but may block for TTL")
	} else if count > 0 {
		log.L().Info().Int("cleared_leases", count).Msg("startup: flushed stale leases")
	}

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
						log.L().Error().Err(err).Str("sid", e.SessionID).Str("correlation_id", e.CorrelationID).Msg("session start failed")
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
						log.L().Error().Err(err).Str("sid", e.SessionID).Str("correlation_id", e.CorrelationID).Msg("session stop failed")
					}
				}(evt)
			}
		}
	}
}

func (o *Orchestrator) handleStart(ctx context.Context, e model.StartSessionEvent) (retErr error) {
	correlationID, session, ctx, err := o.resolveSession(ctx, e)
	if err != nil {
		return err
	}
	leaseOwner := e.SessionID

	startTime := time.Now()
	startRecorded := false

	recordStart := func(result string, reason model.ReasonCode) {
		if startRecorded {
			return
		}
		startRecorded = true
		recordSessionStartOutcome(result, reason, e.ProfileID)
	}
	startResultForReason := func(reason model.ReasonCode) string {
		switch reason {
		case model.RLeaseBusy:
			return "busy"
		case model.RClientStop, model.RCancelled:
			return "cancel"
		default:
			return "fail"
		}
	}

	logger := log.WithContext(ctx, log.WithComponent("worker"))
	logger = logger.With().Str("sid", e.SessionID).Logger()

	// CTO-CRITICAL: Initialize with safe defaults BEFORE defer
	sessionCtx := &sessionContext{
		Mode:       model.ModeLive,
		ServiceRef: e.ServiceRef,
		IsVOD:      false,
	}

	// 0. Unified Finalization (Always Runs)
	// Critical Fix 9-4: Must run even if tune/lease fail.
	// CTO: Extracted to finalizeDeferred() for complexity reduction
	defer o.finalizeDeferred(ctx, e, &session, sessionCtx, logger, &startRecorded, recordStart, startResultForReason, &retErr)

	if session == nil {
		return newReasonError(model.RNotFound, "session not found", nil)
	}

	// Step 3.1 (PURE): Extract parsing/normalization
	sessionCtx, err = o.buildSessionContext(session, e)
	if err != nil {
		return err
	}

	// Re-add behavior-critical logging that was in buildSessionContext block
	if sessionCtx.Mode == model.ModeRecording {
		sourceType := ""
		if session.ContextData != nil {
			sourceType = strings.TrimSpace(session.ContextData[model.CtxKeySourceType])
		}
		logger.Info().
			Str("source_type", sourceType).
			Str("source", sessionCtx.ServiceRef).
			Msg("recording playback source selected")
	}

	// 1 & 2. Lease Acquisition (Dedup & Tuner) - Phase 8-2
	leases, err := o.acquireLeases(ctx, sessionCtx, e, leaseOwner, logger)
	if err != nil {
		return err
	}
	defer leases.ReleaseDedup()
	defer leases.HBCancel()
	defer o.unregisterActive(e.SessionID)

	// 1. Transition to STARTING (Store Tuner Slot)
	if err := o.transitionStarting(ctx, e, sessionCtx, leases.Slot); err != nil {
		return err
	}
	// Refresh session record so finalizeDeferred sees the tuner slot for cleanup
	if sess, err := o.Store.GetSession(ctx, e.SessionID); err == nil && sess != nil {
		session = sess
	}

	// 2. Perform Work (Execution Contracts)
	tuner, err := o.setupTuner(leases.Slot, sessionCtx)
	if err != nil {
		return err
	}
	if tuner != nil {
		defer func() { _ = tuner.Close() }()
	}

	tuneErr := o.tunePlaybackSource(leases.HBCtx, e, sessionCtx, tuner, logger)
	if tuneErr != nil {
		return tuneErr
	}

	// Ready Success Counter
	readyOutcomeTotal.WithLabelValues("success").Inc()

	// Fix 12: Hybrid Repair Policy (Retry Loop)
	transcoder, finalProfile, err := o.runExecutionLoop(ctx, leases.HBCtx, e, sessionCtx, session, startTime, logger, recordStart)
	if err != nil {
		return err
	}

	// Defer unified final stop
	defer func() {
		stopBaseCtx := context.Background()
		if correlationID != "" {
			stopBaseCtx = log.ContextWithCorrelationID(stopBaseCtx, correlationID)
		}
		stopCtx, cancel := context.WithTimeout(stopBaseCtx, o.ffmpegStopTimeout())
		defer cancel()
		_ = transcoder.Stop(stopCtx)
	}()

	playlistReadyDuration := time.Since(startTime)
	logger.Info().
		Str("session_id", e.SessionID).
		Str("profile", finalProfile.Name).
		Int64("elapsed_ms", playlistReadyDuration.Milliseconds()).
		Msg("playlist ready - transitioning to READY state")

	// 4. Update READY
	if err := o.transitionReady(ctx, e); err != nil {
		return err
	}
	recordStart("success", model.RNone)

	// 5. Wait
	leases.ReleaseDedup()

	_, waitErr := transcoder.Wait(leases.HBCtx)
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

func (o *Orchestrator) acquireTunerLease(ctx context.Context, slots []int, owner string) (slot int, l store.Lease, ok bool, err error) {
	for _, s := range slots {
		k := lease.LeaseKeyTunerSlot(s)
		l, got, e := o.Store.TryAcquireLease(ctx, k, owner, o.LeaseTTL)
		if e != nil {
			return 0, nil, false, e
		}
		if got {
			return s, l, true, nil
		}
	}
	return 0, nil, false, nil
}

func (o *Orchestrator) ffmpegStopTimeout() time.Duration {
	if o.FFmpegKillTimeout > 0 {
		return o.FFmpegKillTimeout
	}
	return 5 * time.Second
}

func (o *Orchestrator) recordTransition(from, to model.SessionState) {
	fsmTransitions.WithLabelValues(string(from), string(to)).Inc()
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

// ForceReleaseLeases attempts to release all possible leases for a session.
// It is idempotent and safe to call on sessions that may not hold leases.
func (o *Orchestrator) ForceReleaseLeases(ctx context.Context, sid, ref string, s *model.SessionRecord) {
	// 1. Dedup Lease (ServiceRef)
	// Key reconstruction: We need the ServiceRef.
	// If 'ref' is passed, use it. If not, try to get from SessionRecord.
	serviceRef := ref
	if serviceRef == "" && s != nil {
		serviceRef = s.ServiceRef
	}
	if serviceRef != "" {
		// We reconstruct the key manually or use LeaseKeyFunc?
		// We need an event for LeaseKeyFunc... but LeaseKeyFunc typically just uses ServiceRef.
		// Let's assume standard behavior or use the helper if available.
		// But LeaseKeyFunc is a field on Orchestrator.
		// We can mock a StartSessionEvent.
		evt := model.StartSessionEvent{ServiceRef: serviceRef}
		key := o.LeaseKeyFunc(evt)
		_ = o.Store.ReleaseLease(ctx, key, sid)
	}

	// 2. Tuner Lease (Slot)
	// Only if we can determine the slot.
	if s != nil && s.ContextData != nil {
		if raw := s.ContextData[model.CtxKeyTunerSlot]; raw != "" {
			if slot, err := strconv.Atoi(raw); err == nil {
				key := lease.LeaseKeyTunerSlot(slot)
				_ = o.Store.ReleaseLease(ctx, key, sid)
			}
		}
	}
}

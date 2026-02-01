// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package manager

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/lifecycle"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/metrics"
	platformnet "github.com/ManuGH/xg2g/internal/platform/net"
)

// Orchestrator consumes intents and drives pipelines.
type Orchestrator struct {
	Store store.StateStore
	Bus   ports.Bus

	LeaseTTL       time.Duration
	HeartbeatEvery time.Duration
	Owner          string // Stable worker identity
	TunerSlots     []int  // Available hardware slots
	HLSRoot        string // Root directory for HLS segments
	Sweeper        SweeperConfig

	Pipeline        ports.MediaPipeline
	Platform        ports.Platform         // OS/FS operations
	HeartbeatSource SegmentHeartbeatSource // PR-P3-2: Pluggable truth source
	LeaseKeyFunc    func(model.StartSessionEvent) string

	PipelineStopTimeout time.Duration
	OutboundPolicy      platformnet.OutboundPolicy

	// Concurrency Control
	StartConcurrency int
	StopConcurrency  int

	// Phase 9-2: Lifecycle Management
	mu       sync.Mutex
	active   map[string]context.CancelFunc
	startSem chan struct{}
	stopSem  chan struct{}
}

func (o *Orchestrator) Run(ctx context.Context) error {
	// Validation: Concurrency limits must be set
	if o.StartConcurrency <= 0 {
		return fmt.Errorf("StartConcurrency must be > 0, got %d", o.StartConcurrency)
	}
	if o.StopConcurrency <= 0 {
		return fmt.Errorf("StopConcurrency must be > 0, got %d", o.StopConcurrency)
	}

	// Validation: Required fields must be set
	if o.LeaseTTL <= 0 {
		return fmt.Errorf("LeaseTTL must be > 0, got %v", o.LeaseTTL)
	}
	if o.HeartbeatEvery <= 0 {
		return fmt.Errorf("HeartbeatEvery must be > 0, got %v", o.HeartbeatEvery)
	}
	if o.PipelineStopTimeout <= 0 {
		return fmt.Errorf("PipelineStopTimeout must be > 0, got %v", o.PipelineStopTimeout)
	}
	if o.Owner == "" {
		return errors.New("owner must be set")
	}
	if o.LeaseKeyFunc == nil {
		o.LeaseKeyFunc = func(e model.StartSessionEvent) string {
			return model.LeaseKeyService(e.ServiceRef)
		}
	}

	if o.active == nil {
		o.active = make(map[string]context.CancelFunc)
	}

	// Initialize semaphores for bounded concurrency
	o.startSem = make(chan struct{}, o.StartConcurrency)
	o.stopSem = make(chan struct{}, o.StopConcurrency)

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

	// CTO Hardening: Startup Guard
	// Prevents split-brain by ensuring we are the only active instance.
	guardKey := "system:orchestrator:guard_lock"
	if _, acquired, err := o.Store.TryAcquireLease(ctx, guardKey, o.Owner, o.LeaseTTL); err != nil {
		return fmt.Errorf("failed to check guard lease: %w", err)
	} else if !acquired {
		// Verify ownership strictly (Fatal on Ambiguity)
		held, ok, err := o.Store.GetLease(ctx, guardKey)
		if err != nil {
			return fmt.Errorf("fatal: failed to verify guard lease ownership (store error): %w", err)
		}
		if !ok || held == nil {
			return fmt.Errorf("fatal: guard lease acquisition failed but lease not found (ambiguous state); refusing to start")
		}
		if held.Owner() != o.Owner {
			return fmt.Errorf("fatal: orchestrator guard lock held by %q; refusing to start (single-writer constraint)", held.Owner())
		}
		// We own it (restarting). Safe to proceed.
	}

	// Safe to wipe (we are leader or restarting)
	if count, err := o.Store.DeleteAllLeases(ctx); err != nil {
		log.L().Error().Err(err).Msg("failed to flush old leases on startup, continuing but may block for TTL")
	} else if count > 0 {
		log.L().Info().Int("cleared_leases", count).Msg("startup: flushed stale leases")
	}

	// Re-acquire guard immediately and maintain it
	if _, acquired, err := o.Store.TryAcquireLease(ctx, guardKey, o.Owner, o.LeaseTTL); err != nil {
		return fmt.Errorf("failed to re-acquire guard lease: %w", err)
	} else if !acquired {
		// CTO Stop-the-line: If we can't acquire after deleting all, something is very wrong (race or store failure).
		// We must not proceed without the guard.
		return fmt.Errorf("fatal: failed to acquire guard lease after wipe; split-brain risk")
	}

	guardFail := make(chan error, 1)
	go o.maintainGuardLease(ctx, guardKey, guardFail)

	// Best-effort release on shutdown
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = o.Store.ReleaseLease(releaseCtx, guardKey, o.Owner)
	}()

	if err := o.recoverStaleLeases(ctx); err != nil {
		return fmt.Errorf("recovery sweep failed: %w", err)
	}

	// CTO Fix #5: Reconcile Tuner Gauge on Startup (Truth Snapshot)
	if err := o.reconcileTunerMetrics(ctx); err != nil {
		return fmt.Errorf("tuner metric reconciliation failed: %w", err)
	}

	// Validation: Sweeper config must be set
	if o.Sweeper.Interval <= 0 {
		return fmt.Errorf("Sweeper.Interval must be > 0, got %v", o.Sweeper.Interval)
	}
	if o.Sweeper.SessionRetention <= 0 {
		return fmt.Errorf("Sweeper.SessionRetention must be > 0, got %v", o.Sweeper.SessionRetention)
	}

	sweeper := &Sweeper{Orch: o, Conf: o.Sweeper}
	go sweeper.Run(ctx)

	for {
		select {
		case err := <-guardFail:
			// CTO Stop-the-line: Guard lease lost!
			return fmt.Errorf("fatal: guard lease lost (split-brain risk): %w", err)
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-subStart.C():
			if !ok {
				return errors.New("event channel closed")
			}
			if evt, ok := msg.(model.StartSessionEvent); ok {
				// Check cancellation first before acquiring semaphore
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}
				// Acquire semaphore (blocking, cancellable)
				select {
				case o.startSem <- struct{}{}:
					go func(e model.StartSessionEvent) {
						defer func() { <-o.startSem }()
						if err := o.handleStart(ctx, e); err != nil {
							log.L().Error().Err(err).Str("sid", e.SessionID).Str("correlation_id", e.CorrelationID).Msg("session start failed")
						}
					}(evt)
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		case msg, ok := <-subStop.C():
			if !ok {
				return errors.New("event channel closed")
			}
			if evt, ok := msg.(model.StopSessionEvent); ok {
				// Check cancellation first before acquiring semaphore
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}
				// Acquire semaphore (blocking, cancellable)
				select {
				case o.stopSem <- struct{}{}:
					go func(e model.StopSessionEvent) {
						defer func() { <-o.stopSem }()
						if err := o.handleStop(ctx, e); err != nil {
							log.L().Error().Err(err).Str("sid", e.SessionID).Str("correlation_id", e.CorrelationID).Msg("session stop failed")
						}
					}(evt)
				case <-ctx.Done():
					return ctx.Err()
				}
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

	sessionCtx := &sessionContext{
		Mode:       model.ModeLive,
		ServiceRef: e.ServiceRef,
		IsVOD:      false,
	}

	defer func() {
		if retErr != nil {
			retErr = wrapWithReasonClass(retErr)
		}
	}()
	defer o.finalizeDeferred(ctx, e, &session, sessionCtx, logger, &startRecorded, recordStart, startResultForReason, &retErr)

	if session == nil {
		return newReasonError(model.RNotFound, "session not found", nil)
	}

	sessionCtx, err = o.buildSessionContext(session, e)
	if err != nil {
		return err
	}

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

	leases, err := o.acquireLeases(ctx, sessionCtx, e, leaseOwner, logger)
	if err != nil {
		return err
	}
	defer leases.ReleaseDedup()
	defer leases.HBCancel()
	defer o.unregisterActive(e.SessionID)

	// Phase 5.3: Tuner Gauge Truth (Option A - Session Manager)
	if leases.Slot >= 0 {
		metrics.IncTunersInUse()
		defer metrics.DecTunersInUse()
	}

	if err := o.transitionStarting(ctx, e, sessionCtx, leases.Slot); err != nil {
		return err
	}

	if sess, err := o.Store.GetSession(ctx, e.SessionID); err == nil && sess != nil {
		session = sess
	}

	// EXECUTION LOOP (Step 4.2 Port First)
	// Guard: Ensure we are admitted (Defensive Coding / Invariant Protection)
	// EXECUTION LOOP (Step 4.2 Port First)

	runHandle, finalProfile, err := o.runExecutionLoop(ctx, leases.HBCtx, e, sessionCtx, session, startTime, logger, recordStart, leases.Slot)
	if err != nil {
		return err
	}

	defer func() {
		stopBaseCtx := context.Background()
		if correlationID != "" {
			stopBaseCtx = log.ContextWithCorrelationID(stopBaseCtx, correlationID)
		}
		stopCtx, cancel := context.WithTimeout(stopBaseCtx, o.PipelineStopTimeout)
		defer cancel()
		// Use Port Stop
		if runHandle != "" {
			_ = o.Pipeline.Stop(stopCtx, runHandle)
		}
	}()

	playlistReadyDuration := time.Since(startTime)
	logger.Info().
		Str("session_id", e.SessionID).
		Str("profile", finalProfile.Name).
		Int64("elapsed_ms", playlistReadyDuration.Milliseconds()).
		Msg("playlist ready - transitioning to READY state")

	if err := o.transitionReady(ctx, e); err != nil {
		return err
	}
	recordStart("success", model.RNone)

	leases.ReleaseDedup()

	// Wait Loop (Polling Port via waitHelper)
	waitErr := o.waitForProcessExit(leases.HBCtx, runHandle)
	return waitErr
}

func (o *Orchestrator) handleStop(ctx context.Context, e model.StopSessionEvent) error {
	var shortCircuitCleanup bool
	_, err := o.Store.UpdateSession(ctx, e.SessionID, func(r *model.SessionRecord) error {
		if r.State.IsTerminal() {
			return nil
		}
		if r.State == model.SessionNew {
			if e.Reason == model.RIdleTimeout {
				_, err := lifecycle.Dispatch(r, lifecycle.PhaseFromState(r.State), lifecycle.Event{Kind: lifecycle.EvSweeperForcedStop}, nil, false, time.Now())
				if err != nil {
					return err
				}
			} else {
				_, err := lifecycle.Dispatch(r, lifecycle.PhaseFromState(r.State), lifecycle.Event{Kind: lifecycle.EvTerminalize}, nil, true, time.Now())
				if err != nil {
					return err
				}
			}
			shortCircuitCleanup = true
			return nil
		}

		_, err := lifecycle.Dispatch(r, lifecycle.PhaseFromState(r.State), lifecycle.Event{Kind: lifecycle.EvStopRequested, Reason: e.Reason}, nil, false, time.Now())
		if err != nil {
			return err
		}
		r.PipelineState = model.PipeStopRequested
		return nil
	})

	if shortCircuitCleanup {
		o.cleanupFiles(e.SessionID)
	}
	if err != nil {
		return err
	}

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
		k := model.LeaseKeyTunerSlot(s)
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
	// Use Platform port for OS/FS operations
	targetDir := o.Platform.Join(o.HLSRoot, "sessions", sid)
	if err := o.Platform.RemoveAll(targetDir); err != nil {
		log.L().Error().Err(err).Str("path", targetDir).Msg("failed to remove session directory")
	}
}
func (o *Orchestrator) ForceReleaseLeases(ctx context.Context, sid, ref string, s *model.SessionRecord) {
	logger := log.FromContext(ctx)
	serviceRef := ref
	if serviceRef == "" && s != nil {
		serviceRef = s.ServiceRef
	}
	if serviceRef != "" && o.LeaseKeyFunc != nil {
		evt := model.StartSessionEvent{ServiceRef: serviceRef}
		key := o.LeaseKeyFunc(evt)
		if err := o.Store.ReleaseLease(ctx, key, sid); err != nil {
			logger.Error().Err(err).
				Str("lease_key", key).
				Str("session_id", sid).
				Msg("failed to release dedup lease during cleanup")
		}
	}

	if s != nil && s.ContextData != nil {
		if raw := s.ContextData[model.CtxKeyTunerSlot]; raw != "" {
			if slot, err := strconv.Atoi(raw); err == nil {
				key := model.LeaseKeyTunerSlot(slot)
				if err := o.Store.ReleaseLease(ctx, key, sid); err != nil {
					logger.Error().Err(err).
						Str("lease_key", key).
						Int("tuner_slot", slot).
						Str("session_id", sid).
						Msg("failed to release tuner lease during cleanup")
				}
			}
		}
	}
}

// reconcileTunerMetrics computes the truth snapshot of tuners in use
// based on currently held leases/context data, and updates the gauge.
func (o *Orchestrator) reconcileTunerMetrics(ctx context.Context) error {
	// List all sessions (Control Plane Truth)
	sessions, err := o.Store.ListSessions(ctx)
	if err != nil {
		return err
	}

	tunerCount := 0
	countedSlots := make(map[int]bool)

	for _, s := range sessions {
		// Only counting active sessions that hold a tuner slot
		if s.State.IsTerminal() {
			continue
		}
		if s.ContextData != nil {
			if slotStr := s.ContextData[model.CtxKeyTunerSlot]; slotStr != "" {
				// Tenant Truth: Session says "I have slot X"
				// Store Truth: Lease for slot X must belong to Session
				if slot, err := strconv.Atoi(slotStr); err == nil {
					if countedSlots[slot] {
						log.L().Warn().Str("sid", s.SessionID).Int("slot", slot).Msg("invariant violation: duplicate slot claim detected")
						metrics.RecordInvariantViolation("duplicate_slot_claim")
						continue
					}

					key := model.LeaseKeyTunerSlot(slot)
					l, ok, err := o.Store.GetLease(ctx, key)
					if err != nil {
						log.L().Error().Err(err).Str("key", key).Msg("failed to check lease during reconciliation")
						continue
					}
					if ok && l != nil && l.Owner() == s.SessionID {
						tunerCount++
						countedSlots[slot] = true
					} else {
						log.L().Warn().Str("sid", s.SessionID).Int("slot", slot).Msg("session claims slot but lease not held (drift/orphan)")
					}
				}
			}
		}
	}

	// Reconcile Gauge
	metrics.SetTunersInUse(float64(tunerCount))
	log.L().Info().Int("count", tunerCount).Msg("reconciled tuner metrics from store truth")
	return nil
}

func (o *Orchestrator) maintainGuardLease(ctx context.Context, key string, fail chan<- error) {
	ticker := time.NewTicker(o.LeaseTTL / 2)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// CTO Stop-the-line: Must enable fail-closed behavior
			_, ok, err := o.Store.RenewLease(ctx, key, o.Owner, o.LeaseTTL)
			if err != nil {
				fail <- fmt.Errorf("renew failed: %w", err)
				return
			}
			if !ok {
				fail <- fmt.Errorf("lease stolen or expired")
				return
			}
		}
	}
}

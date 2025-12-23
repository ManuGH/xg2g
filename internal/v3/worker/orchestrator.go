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
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/v3/bus"
	"github.com/ManuGH/xg2g/internal/v3/exec"
	"github.com/ManuGH/xg2g/internal/v3/model"
	"github.com/ManuGH/xg2g/internal/v3/store"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	jobsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "v3_worker_jobs_total",
			Help: "Total worker jobs processed",
		},
		[]string{"result", "mode"},
	)
	fsmTransitions = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "v3_fsm_transitions_total",
			Help: "FSM state transitions",
		},
		[]string{"state_from", "state_to", "mode"},
	)
	recoveryTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "v3_worker_recovery_total",
			Help: "Total sessions recovered",
		},
		[]string{"action", "mode"},
	)
	leaseLostTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "v3_worker_lease_lost_total",
			Help: "Total leases lost during heartbeat",
		},
		[]string{"mode"},
	)
)

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
	defer subStart.Close()

	subStop, err := o.Bus.Subscribe(ctx, string(model.EventStopSession))
	if err != nil {
		return err
	}
	defer subStop.Close()

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
		sweeper.Conf.Interval = 5 * time.Minute
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
	// 0. Unified Finalization (Always Runs)
	defer func() {
		// Determine Outcome
		finalState := model.SessionFailed
		reason := model.RFFmpegExited
		detail := ""

		if retErr == nil {
			// Success (process exit code 0)
			finalState = model.SessionStopped
			reason = model.RFFmpegExited
		} else {
			if errors.Is(retErr, context.Canceled) {
				finalState = model.SessionStopped
				reason = model.RClientStop
			} else {
				detail = retErr.Error()
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
	}()

	// 1. Dedup Lock (ServiceRef) - Transient (Phase 8-2)
	// We acquire this to prevent stampede during startup, but we don't hold it long-term.
	dedupKey := o.LeaseKeyFunc(e)
	dedupLease, ok, err := o.Store.TryAcquireLease(ctx, dedupKey, o.Owner, o.LeaseTTL)
	if err != nil {
		return err
	}
	if !ok {
		jobsTotal.WithLabelValues("lease_conflict", o.modeLabel()).Inc()
		return nil
	}
	defer o.Store.ReleaseLease(ctx, dedupLease.Key(), dedupLease.Owner())

	// 2. Resource Lock (Tuner Slot) - Persistent (Phase 8-2)
	slot, tunerLease, ok, err := o.acquireTunerLease(ctx, o.TunerSlots)
	if err != nil {
		return err
	}
	if !ok {
		// All slots busy
		jobsTotal.WithLabelValues("tuner_busy", o.modeLabel()).Inc()
		// We do NOT fail the session, we just don't start it yet.
		// It remains in NEW state (or QUEUED if we had it).
		// Client/Shadow will retry.
		return nil
	}
	defer o.Store.ReleaseLease(ctx, tunerLease.Key(), tunerLease.Owner())

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
					log.L().Warn().Str("lease", tunerLease.Key()).Msg("tuner lease lost, aborting")
					leaseLostTotal.WithLabelValues(o.modeLabel()).Inc()
					hbCancel()
					return
				}
			}
		}
	}()

	// 1. Transition to STARTING (Store Tuner Slot)
	o.recordTransition(model.SessionUnknown, model.SessionStarting)
	_, err = o.Store.UpdateSession(ctx, e.SessionID, func(r *model.SessionRecord) error {
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

	// 2. Perform Work (Execution Contracts)
	tuner, err := o.ExecFactory.NewTuner(slot)
	if err != nil {
		jobsTotal.WithLabelValues("exec_error", o.modeLabel()).Inc()
		return err
	}
	defer tuner.Close()

	if err := tuner.Tune(hbCtx, e.ServiceRef); err != nil {
		jobsTotal.WithLabelValues("tune_failed", o.modeLabel()).Inc()
		return err
	}

	transcoder, err := o.ExecFactory.NewTranscoder()
	if err != nil {
		jobsTotal.WithLabelValues("exec_error", o.modeLabel()).Inc()
		return err
	}

	// e is the StartIntent payload (unmarshaled directly into method args? No, e is *model.SessionStartRequest? or similar)
	// View file shows: func (o *Orchestrator) handleStart(ctx context.Context, e model.IntentStart) error {
	// So variable is `e`.
	// e has SessionID, ServiceRef, ProfileID.
	if err := transcoder.Start(hbCtx, e.SessionID, e.ServiceRef, model.ProfileID(e.ProfileID)); err != nil {
		jobsTotal.WithLabelValues("start_failed", o.modeLabel()).Inc()
		return err
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = transcoder.Stop(stopCtx)
	}()

	// 3. Update READY
	o.recordTransition(model.SessionStarting, model.SessionReady)
	_, err = o.Store.UpdateSession(ctx, e.SessionID, func(r *model.SessionRecord) error {
		r.State = model.SessionReady
		r.UpdatedAtUnix = time.Now().Unix()
		return nil
	})
	if err != nil {
		jobsTotal.WithLabelValues("failed_ready", o.modeLabel()).Inc()
		return err
	}

	jobsTotal.WithLabelValues("processed", o.modeLabel()).Inc()

	// 4. Wait
	_, waitErr := transcoder.Wait(hbCtx)
	return waitErr
}

func (o *Orchestrator) handleStop(ctx context.Context, e model.StopSessionEvent) error {
	// 1. Always attempt store update (Idempotency)
	_, err := o.Store.UpdateSession(ctx, e.SessionID, func(r *model.SessionRecord) error {
		if r.State.IsTerminal() {
			return nil
		}
		// Move to STOPPING. The active worker (if any) will see this and exit,
		// or the finalized block will eventually set STOPPED.
		r.State = model.SessionStopping
		r.PipelineState = model.PipeStopRequested
		r.Reason = e.Reason
		r.UpdatedAtUnix = time.Now().Unix()
		return nil
	})
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

var safeIDRe = regexp.MustCompile(`^[a-zA-Z0-9_\-]+$`)

func (o *Orchestrator) cleanupFiles(sid string) {
	if o.HLSRoot == "" {
		return
	}
	if !safeIDRe.MatchString(sid) {
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
	} else {
		// Log cleanup success? (Verbose)
		// log.L().Info().Str("sid", sid).Msg("cleaned up session files")
	}
}

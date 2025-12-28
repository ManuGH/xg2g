// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package worker

import (
	"bufio"
	"bytes"
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
	"github.com/ManuGH/xg2g/internal/v3/lease"
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
	correlationID := e.CorrelationID
	leaseOwner := e.SessionID
	var session *model.SessionRecord
	if o.Store != nil {
		if sess, err := o.Store.GetSession(ctx, e.SessionID); err == nil && sess != nil {
			session = sess
			if correlationID == "" {
				correlationID = sess.CorrelationID
			}
		}
	}
	if correlationID != "" {
		ctx = log.ContextWithCorrelationID(ctx, correlationID)
	}

	mode := o.modeLabel()
	startTime := time.Now()
	startRecorded := false
	ttfpRecorded := false
	vodMode := false

	recordStart := func(result string, reason model.ReasonCode) {
		if startRecorded {
			return
		}
		startRecorded = true
		recordSessionStartOutcome(result, reason, e.ProfileID, mode)
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

	// 0. Unified Finalization (Always Runs)
	// Critical Fix 9-4: Must run even if tune/lease fail.
	defer func() {
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
			} else if vodMode {
				finalState = model.SessionDraining
				reason = model.RNone
				detail = "recording completed"
			} else {
				// Context valid -> Spontaneous Exit (e.g. End of Stream, Crash, or Early Exit)
				// Fix 11-2: Treat unrequested exit as abnormal termination.
				finalState = model.SessionFailed
				reason = model.RProcessEnded // "Process ended unexpectedly"
			}
		} else {
			reason, detail = classifyReason(retErr)
			if reason == model.RClientStop {
				finalState = model.SessionStopped
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

		// PR 9-3: On-Stop Cleanup (skip for VOD recordings to keep cached assets)
		if !vodMode || finalState == model.SessionFailed {
			o.cleanupFiles(e.SessionID)
		}

		// Phase 9-4: Golden Signals (Session End)
		sessionEndTotal.WithLabelValues(string(reason), e.ProfileID, mode).Inc()

		logEvt := logger.Info().
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

		if !startRecorded {
			recordStart(startResultForReason(reason), reason)
		}
	}()

	if session == nil {
		return newReasonError(model.RNotFound, "session not found", nil)
	}

	sessionMode := model.ModeLive
	if session.ContextData != nil {
		if raw := strings.TrimSpace(session.ContextData[model.CtxKeyMode]); raw != "" {
			sessionMode = strings.ToUpper(raw)
		}
	}
	if sessionMode != model.ModeLive && sessionMode != model.ModeRecording {
		sessionMode = model.ModeLive
	}
	playbackSource := e.ServiceRef
	if sessionMode == model.ModeRecording {
		if session.ContextData != nil {
			playbackSource = strings.TrimSpace(session.ContextData[model.CtxKeySource])
		}
		if playbackSource == "" {
			return newReasonError(model.RInvariantViolation, "missing recording source", nil)
		}
		sourceType := ""
		if session.ContextData != nil {
			sourceType = strings.TrimSpace(session.ContextData[model.CtxKeySourceType])
		}
		logger.Info().
			Str("source_type", sourceType).
			Str("source", playbackSource).
			Msg("recording playback source selected")
	}
	vodMode = session.Profile.VOD || sessionMode == model.ModeRecording

	// 1. Dedup Lock (ServiceRef) - Transient (Phase 8-2)
	// We acquire this to prevent stampede during startup, but we don't hold it long-term.
	releaseDedup := func() {}
	if sessionMode == model.ModeLive {
		dedupKey := o.LeaseKeyFunc(e)
		dedupLease, ok, err := o.Store.TryAcquireLease(ctx, dedupKey, leaseOwner, o.LeaseTTL)
		if err != nil {
			return err
		}
		if !ok {
			// jobsTotal.WithLabelValues("lease_conflict", o.modeLabel()).Inc()
			return newReasonError(model.RLeaseBusy, "dedup lease held", nil)
		}
		// Release Dedup Lock immediately after critical section (Transient)
		// We only hold it to linearize setup for the same service.
		// Fix: Do NOT defer to function end (session end).
		releaseDedup = func() {
			ctxRel, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = o.Store.ReleaseLease(ctxRel, dedupLease.Key(), dedupLease.Owner())
		}
		defer releaseDedup() // Safety fallback (idempotent)
	}

	// We will call releaseDedup() explicitly once we have successfully transitioned or failed.

	// 2. Resource Lock (Tuner Slot) - Persistent (Phase 8-2)
	slot := -1
	var tunerLease store.Lease
	if sessionMode == model.ModeLive {
		var ok bool
		var err error
		slot, tunerLease, ok, err = o.acquireTunerLease(ctx, o.TunerSlots, leaseOwner)
		if err != nil {
			return err
		}
		if !ok {
			// All slots busy
			tunerBusyTotal.WithLabelValues(o.modeLabel()).Inc()
			return newReasonError(model.RLeaseBusy, "no tuner slots available", nil)
		}
		defer func() {
			ctxRel, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = o.Store.ReleaseLease(ctxRel, tunerLease.Key(), tunerLease.Owner())
		}()
	}

	// Heartbeat loop: Renew TUNER Lease explicitly
	hbCtx, hbCancel := context.WithCancel(ctx)
	// Phase 9-2: Lifecycle Management (Store CancelFunc)
	o.registerActive(e.SessionID, hbCancel)
	defer o.unregisterActive(e.SessionID)
	// We also defer hbCancel to ensure resources are freed if we panic or return early
	defer hbCancel()

	if sessionMode == model.ModeLive {
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
						logger.Warn().Err(err).Msg("heartbeat renewal error")
					} else if !ok {
						logger.Warn().Str("lease", tunerLease.Key()).Str("sid", e.SessionID).Msg("tuner lease lost, aborting")
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
	}

	// 1. Transition to STARTING (Store Tuner Slot)
	o.recordTransition(model.SessionUnknown, model.SessionStarting)
	_, err := o.Store.UpdateSession(ctx, e.SessionID, func(r *model.SessionRecord) error {
		// Guard: If somebody (handleStop) already marked it STOPPING or Terminal, abort start
		if r.State.IsTerminal() || r.State == model.SessionStopping {
			return fmt.Errorf("session state %s, aborting start", r.State)
		}
		r.State = model.SessionStarting
		r.UpdatedAtUnix = time.Now().Unix()
		if r.ContextData == nil {
			r.ContextData = make(map[string]string)
		}
		if sessionMode == model.ModeLive && slot >= 0 {
			r.ContextData[model.CtxKeyTunerSlot] = strconv.Itoa(slot)
		}
		return nil
	})
	if err != nil {
		jobsTotal.WithLabelValues("failed_starting", o.modeLabel()).Inc()
		return err
	}
	// started = true // Removed in Phase 9-4

	// 2. Perform Work (Execution Contracts)
	var tuner exec.Tuner
	if sessionMode == model.ModeLive {
		var err error
		tuner, err = o.ExecFactory.NewTuner(slot)
		if err != nil {
			// jobsTotal.WithLabelValues("exec_error", o.modeLabel()).Inc()
			return err
		}
		defer func() { _ = tuner.Close() }()
	}

	// Measure Ready Duration
	readyStart := time.Now()
	var tuneErr error
	if sessionMode == model.ModeRecording {
		logger.Info().Str("source", playbackSource).Msg("worker: recording mode, skipping tune")
	} else if len(e.ServiceRef) > 0 && e.ServiceRef[0] == '/' {
		logger.Info().Str("ref", e.ServiceRef).Msg("worker: skipping tune for local file")
	} else {
		logger.Info().Str("ref", e.ServiceRef).Msg("worker: starting tune")
		tuneErr = tuner.Tune(hbCtx, e.ServiceRef)
	}
	readyDurationVal := time.Since(readyStart).Seconds()

	// Classify Outcome
	var outcome string
	if tuneErr == nil {
		outcome = "success"
		logger.Info().Msg("worker: tune success")
	} else {
		outcome = "failure"
		logger.Error().Err(tuneErr).Msg("worker: tune failed")
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
		// jobsTotal.WithLabelValues("tune_failed", o.modeLabel()).Inc()
		if errors.Is(tuneErr, context.Canceled) {
			return tuneErr
		}
		if errors.Is(tuneErr, context.DeadlineExceeded) {
			return newReasonError(model.RTuneTimeout, "", tuneErr)
		}
		return newReasonError(model.RTuneFailed, "", tuneErr)
	}

	// Ready Success Counter
	readyOutcomeTotal.WithLabelValues("success", o.modeLabel()).Inc()

	transcoder, err := o.ExecFactory.NewTranscoder()
	if err != nil {
		return newReasonError(model.RFFmpegStartFailed, "transcoder init failed", err)
	}

	// Fetch session to get ProfileSpec with DVR window configuration
	profileSpec := session.Profile
	if vodMode {
		profileSpec.VOD = true
	}

	ffmpegStartTime := time.Now()
	if err := transcoder.Start(hbCtx, e.SessionID, playbackSource, profileSpec, e.StartMs); err != nil {
		return newReasonError(model.RFFmpegStartFailed, "", err)
	}
	o.recordTransition(model.SessionStarting, model.SessionPriming)
	_, err = o.Store.UpdateSession(ctx, e.SessionID, func(r *model.SessionRecord) error {
		if r.State.IsTerminal() || r.State == model.SessionStopping {
			return fmt.Errorf("session state %s, aborting priming", r.State)
		}
		r.State = model.SessionPriming
		r.UpdatedAtUnix = time.Now().Unix()
		return nil
	})
	if err != nil {
		return err
	}
	if o.HLSRoot != "" {
		sessionDir := filepath.Join(o.HLSRoot, "sessions", e.SessionID)
		go observeFirstSegment(hbCtx, sessionDir, startTime, e.ProfileID, mode)
	}
	defer func() {
		stopBaseCtx := context.Background()
		if correlationID != "" {
			stopBaseCtx = log.ContextWithCorrelationID(stopBaseCtx, correlationID)
		}
		stopCtx, cancel := context.WithTimeout(stopBaseCtx, 5*time.Second)
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
		if vodMode {
			playlistReadyTimeout = 2 * time.Minute
		}
		playlistPollInterval := 200 * time.Millisecond
		playlistDeadline := time.Now().Add(playlistReadyTimeout)

		logger.Info().
			Str("session_id", e.SessionID).
			Str("service_ref", e.ServiceRef).
			Str("playlist_path", playlistPath).
			Dur("timeout", playlistReadyTimeout).
			Bool("vod_mode", vodMode).
			Msg("waiting for playlist to become ready")

		for {
			// Success condition: file exists, non-empty, and contains #EXTM3U header
			if info, statErr := os.Stat(playlistPath); statErr == nil && info.Size() > 0 {
				// Sanity check: validate it's a real HLS manifest
				// #nosec G304 -- playlistPath is constructed from trusted hlsDir, not user input
				if content, readErr := os.ReadFile(playlistPath); readErr == nil {
					contentText := string(content)
					if strings.Contains(contentText, "#EXTM3U") {
						if vodMode && !strings.Contains(contentText, "#EXT-X-ENDLIST") {
							logger.Debug().
								Str("session_id", e.SessionID).
								Msg("VOD playlist missing ENDLIST, continuing to poll")
						} else {
							segmentURI := firstSegmentFromPlaylist(content)
							if vodMode {
								if lastSegment := lastSegmentFromPlaylist(content); lastSegment != "" {
									segmentURI = lastSegment
								}
							}
							if segmentURI != "" {
								segmentPath := filepath.Join(filepath.Dir(playlistPath), segmentURI)
								if segInfo, segErr := os.Stat(segmentPath); segErr == nil && segInfo.Size() > 0 {
									if !ttfpRecorded {
										observeTTFP(e.ProfileID, mode, startTime)
										ttfpRecorded = true
									}
									break // Playlist + segment ready!
								}
							}
							logger.Debug().
								Str("session_id", e.SessionID).
								Msg("playlist ready but segment not yet available, continuing to poll")
						}
					}
					logger.Debug().
						Str("session_id", e.SessionID).
						Msg("playlist exists but missing #EXTM3U header, continuing to poll")
				}
			}

			// Termination condition: timeout
			if time.Now().After(playlistDeadline) {
				elapsedMs := time.Since(ffmpegStartTime).Milliseconds()
				logger.Error().
					Str("session_id", e.SessionID).
					Str("service_ref", e.ServiceRef).
					Str("playlist_path", playlistPath).
					Dur("timeout", playlistReadyTimeout).
					Int64("elapsed_ms", elapsedMs).
					Msg("playlist not ready after timeout - failing session")
				return newReasonError(
					model.RPackagerFailed,
					fmt.Sprintf("playlist not ready after %s", playlistReadyTimeout),
					nil,
				)
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
		logger.Info().
			Str("session_id", e.SessionID).
			Str("service_ref", e.ServiceRef).
			Int64("elapsed_ms", playlistReadyDuration.Milliseconds()).
			Msg("playlist ready - transitioning to READY state")
	}

	// 4. Update READY
	// Now it's safe to declare READY because playlist is servable
	o.recordTransition(model.SessionPriming, model.SessionReady)
	_, err = o.Store.UpdateSession(ctx, e.SessionID, func(r *model.SessionRecord) error {
		r.State = model.SessionReady
		r.UpdatedAtUnix = time.Now().Unix()
		r.LastAccessUnix = time.Now().Unix()
		return nil
	})
	if err != nil {
		return err
	}
	recordStart("success", model.RNone)
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

func firstSegmentFromPlaylist(content []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, "..") || filepath.IsAbs(line) {
			continue
		}
		return line
	}
	return ""
}

func lastSegmentFromPlaylist(content []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(content))
	var last string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, "..") || filepath.IsAbs(line) {
			continue
		}
		last = line
	}
	return last
}

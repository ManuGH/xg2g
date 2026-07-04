package manager

import (
	"context"
	"fmt"
	"github.com/ManuGH/xg2g/internal/domain/session/lifecycle"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"github.com/rs/zerolog"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func detectTerminationCause(ctx context.Context, retErr error) terminationCause {
	if retErr == nil {
		if ctx.Err() != nil {
			return terminationCause{ContextCancelled: true}
		}
		return terminationCause{IsClean: true}
	}
	return terminationCause{Error: retErr}
}

func (o *Orchestrator) waitForProcessExit(ctx context.Context, handle ports.RunHandle) error {
	// Polling wait loop with exponential backoff (no max timeout for live sessions).
	const initialInterval = 500 * time.Millisecond
	const maxInterval = 5 * time.Second

	status := o.Pipeline.Health(ctx, handle)
	sessionID := sessionIDFromRunHandle(handle)
	o.updatePlaybackRuntimeDiagnosticsBestEffort(ctx, sessionID, status)
	if !status.Healthy {
		// Process already exited.
		return nil
	}

	interval := initialInterval
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			status := o.Pipeline.Health(ctx, handle)
			o.updatePlaybackRuntimeDiagnosticsBestEffort(ctx, sessionID, status)
			if !status.Healthy {
				// Process exited
				return nil
			}

			// Exponential backoff
			interval = min(interval*2, maxInterval)
			ticker.Reset(interval)
		}
	}
}

func (o *Orchestrator) transitionStarting(ctx context.Context, e model.StartSessionEvent, sessionCtx *sessionContext, slot int) error {
	o.recordTransition(ctx, e.SessionID, model.SessionUnknown, model.SessionStarting, "")
	_, err := o.Store.UpdateSession(ctx, e.SessionID, func(r *model.SessionRecord) error {
		if r.State.IsTerminal() {
			if !canRestartTerminalFallback(r) {
				return fmt.Errorf("session state %s, aborting start: %w", r.State, ErrSessionCanceled)
			}
			resetForFallbackRestart(r, time.Now())
		}
		if r.State == model.SessionStopping {
			return fmt.Errorf("session state %s, aborting start: %w", r.State, ErrSessionCanceled)
		}
		_, err := lifecycle.Dispatch(r, lifecycle.PhaseFromState(r.State), lifecycle.Event{Kind: lifecycle.EvStartRequested}, nil, false, time.Now())
		if err != nil {
			return err
		}
		if r.ContextData == nil {
			r.ContextData = make(map[string]string)
		}
		inputKind := sessionInputKind(sessionCtx)
		if inputKind != "" {
			r.ContextData[model.CtxKeySourceType] = inputKind
		}
		if sessionCtx.ServiceRef != "" {
			r.ContextData[model.CtxKeySource] = sessionCtx.ServiceRef
		}
		if sessionCtx.Mode == model.ModeLive && slot >= 0 {
			r.ContextData[model.CtxKeyTunerSlot] = strconv.Itoa(slot)
		}
		trace := ensurePlaybackTrace(r)
		if trace.RequestProfile == "" {
			trace.RequestProfile = profiles.PublicProfileName(e.ProfileID)
		}
		if trace.ClientPath == "" {
			trace.ClientPath = strings.TrimSpace(r.ContextData[model.CtxKeyClientPath])
		}
		trace.InputKind = inputKind
		applyTracePolicyProfile(trace, r.Profile)
		trace.TargetProfile = model.TraceTargetProfileFromProfile(r.Profile)
		if trace.TargetProfile != nil {
			trace.TargetProfileHash = trace.TargetProfile.Hash()
		}
		return nil
	})
	if err != nil {
		jobsTotal.WithLabelValues("failed_starting").Inc()
		return err
	}
	return nil
}

func (o *Orchestrator) transitionReady(ctx context.Context, e model.StartSessionEvent) error {
	o.recordTransition(ctx, e.SessionID, model.SessionPriming, model.SessionReady, "")
	_, err := o.Store.UpdateSession(ctx, e.SessionID, func(r *model.SessionRecord) error {
		if r.State == model.SessionStopping || r.State.IsTerminal() {
			reason := r.Reason
			if reason == "" {
				reason = model.RCancelled
			}
			return newReasonError(reason, "stop requested before ready", nil)
		}
		_, err := lifecycle.Dispatch(r, lifecycle.PhaseFromState(r.State), lifecycle.Event{Kind: lifecycle.EvReady}, nil, false, time.Now())
		if err != nil {
			return err
		}
		now := time.Now()
		r.PlaylistPublishedAt = now // PR-P3-2: Truth for buffering/active derivation
		r.LastAccessUnix = now.Unix()
		trace := ensurePlaybackTrace(r)
		if trace.FirstFrameAtUnix == 0 {
			trace.FirstFrameAtUnix = firstFrameUnixFromArtifacts(o.HLSRoot, e.SessionID)
		}
		return nil
	})
	return err
}

func (o *Orchestrator) runExecutionLoop(
	ctx context.Context,
	hbCtx context.Context,
	e model.StartSessionEvent,
	sessionCtx *sessionContext,
	session *model.SessionRecord,
	startTime time.Time,
	logger zerolog.Logger,
	recordStart func(string, model.ReasonCode),
	tunerSlot int,
) (ports.RunHandle, model.ProfileSpec, error) {
	initialProfileSpec := session.Profile
	if sessionCtx.IsVOD {
		initialProfileSpec.VOD = true
	}
	currentProfileSpec := initialProfileSpec
	ttfpRecorded := false

	var handle ports.RunHandle
	var failReason model.ReasonCode
	var failDetail string

	o.recordTransition(ctx, e.SessionID, model.SessionStarting, model.SessionPriming, "")
	_, err := o.Store.UpdateSession(ctx, e.SessionID, func(r *model.SessionRecord) error {
		if r.State.IsTerminal() || r.State == model.SessionStopping {
			return fmt.Errorf("session state %s, aborting priming: %w", r.State, ErrSessionCanceled)
		}
		_, err := lifecycle.Dispatch(r, lifecycle.PhaseFromState(r.State), lifecycle.Event{Kind: lifecycle.EvPrimingStarted}, nil, false, time.Now())
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return "", model.ProfileSpec{}, err
	}
	if o.HLSRoot != "" {
		// PR-P3-2: Start continuous heartbeat monitor (interim FS polling)
		_ = o.goSessionWorker(func() {
			o.startHeartbeatMonitor(hbCtx, e.SessionID)
		})
	}

	playlistPath := ""
	if o.HLSRoot != "" {
		playlistPath = filepath.Join(o.HLSRoot, "sessions", e.SessionID, "index.m3u8")
	}

	for attempt := 0; attempt <= defaultStartupProcessRetryLimit; attempt++ {
		var effectiveProfile model.ProfileSpec
		handle, effectiveProfile, err = o.startPipeline(hbCtx, e, sessionCtx, currentProfileSpec, tunerSlot)
		if err != nil {
			return "", model.ProfileSpec{}, err
		}
		currentProfileSpec = effectiveProfile

		playlistReadyResult := false
		if playlistPath != "" {
			var waitReason model.ReasonCode
			var waitDetail string

			playlistReadyResult, waitReason, waitDetail = o.waitForReady(
				ctx, hbCtx, e, currentProfileSpec, handle,
				playlistPath, sessionCtx.IsVOD,
				startTime, logger, &ttfpRecorded,
			)

			if !playlistReadyResult {
				failReason = waitReason
				failDetail = waitDetail
			}
		} else {
			playlistReadyResult = true
		}

		if playlistReadyResult {
			return handle, currentProfileSpec, nil
		}

		nextProfileSpec, promoteProfile := startupRecoveryProfile(currentProfileSpec, failReason, failDetail)
		if attempt < defaultStartupProcessRetryLimit && (shouldRetryStartupWaitFailure(failReason, failDetail, attempt) || promoteProfile) {
			if promoteProfile {
				if err := o.persistStartupRecoveryProfile(ctx, e.SessionID, currentProfileSpec, nextProfileSpec); err != nil {
					o.stopPipelineHandle(ctx, handle)
					return "", model.ProfileSpec{}, err
				}
			}

			logger.Warn().
				Str("session_id", e.SessionID).
				Int("attempt", attempt+1).
				Int("max_retries", defaultStartupProcessRetryLimit).
				Str("reason", string(failReason)).
				Str("detail", failDetail).
				Str("from_profile", currentProfileSpec.Name).
				Str("to_profile", func() string {
					if promoteProfile {
						return nextProfileSpec.Name
					}
					return currentProfileSpec.Name
				}()).
				Msg("startup failed before ready; retrying once")

			o.stopPipelineHandle(ctx, handle)
			if o.HLSRoot != "" {
				o.cleanupFiles(e.SessionID)
			}
			if promoteProfile {
				currentProfileSpec = nextProfileSpec
			}
			ttfpRecorded = false
			continue
		}

		o.stopPipelineHandle(ctx, handle)
		return "", model.ProfileSpec{}, newReasonError(failReason, failDetail, nil)
	}

	return "", model.ProfileSpec{}, newReasonError(failReason, failDetail, nil)
}

func (o *Orchestrator) finalizeDeferred(
	ctx context.Context,
	event model.StartSessionEvent,
	sessionPtr **model.SessionRecord,
	leasesPtr **leaseAcquisition,
	sessionCtx *sessionContext,
	logger zerolog.Logger,
	startRecorded *bool,
	recordStart func(string, model.ReasonCode),
	startResultForReason func(model.ReasonCode) string,
	retErr *error,
) {
	session := *sessionPtr
	cause := detectTerminationCause(ctx, *retErr)
	if cause.Error == nil && cause.ContextCancelled {
		cause.Error = context.Canceled
	}
	var outcome finalOutcome
	var traceSnapshot *model.PlaybackTrace

	// Use bounded timeout context for finalization instead of Background
	finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	_, err := o.Store.UpdateSession(finalizeCtx, event.SessionID, func(r *model.SessionRecord) error {
		if r.State.IsTerminal() && r.State != model.SessionStopping {
			return nil
		}

		stopIntent := r.State == model.SessionStopping || r.Reason == model.RClientStop || r.Reason == model.RIdleTimeout
		errForOutcome := cause.Error
		if errForOutcome == nil && cause.ContextCancelled {
			errForOutcome = context.Canceled
		}
		phase := lifecycle.PhaseFromState(r.State)
		if cause.IsClean && sessionCtx.IsVOD {
			phase = lifecycle.PhaseVODComplete
		}
		fromState := r.State
		tr, err := lifecycle.Dispatch(r, phase, lifecycle.Event{Kind: lifecycle.EvTerminalize, Reason: r.Reason}, errForOutcome, stopIntent, time.Now())
		if err != nil {
			return err
		}
		outcome = finalOutcome{State: tr.To, Reason: tr.Reason, DetailDebug: tr.DetailDebug}

		o.recordTransition(finalizeCtx, event.SessionID, fromState, outcome.State, outcome.Reason)

		r.UpdatedAtUnix = time.Now().Unix()
		trace := ensurePlaybackTrace(r)
		if trace.RequestProfile == "" {
			trace.RequestProfile = profiles.PublicProfileName(event.ProfileID)
		}
		if trace.ClientPath == "" && r.ContextData != nil {
			trace.ClientPath = strings.TrimSpace(r.ContextData[model.CtxKeyClientPath])
		}
		if trace.InputKind == "" {
			trace.InputKind = sessionInputKindFromRecord(r)
		}
		if trace.PolicyModeHint == "" || trace.PolicyModeHint == ports.RuntimeModeUnknown {
			trace.PolicyModeHint = tracePolicyModeHint(r.Profile)
		}
		if trace.EffectiveRuntimeMode == "" || trace.EffectiveRuntimeMode == ports.RuntimeModeUnknown {
			trace.EffectiveRuntimeMode = traceEffectiveRuntimeMode(r.Profile)
		}
		if trace.EffectiveModeSource == "" || trace.EffectiveModeSource == ports.RuntimeModeSourceUnknown {
			trace.EffectiveModeSource = traceEffectiveModeSource(r.Profile)
		}
		if trace.TargetProfile == nil {
			trace.TargetProfile = model.TraceTargetProfileFromProfile(r.Profile)
		}
		if trace.TargetProfile != nil && trace.TargetProfileHash == "" {
			trace.TargetProfileHash = trace.TargetProfile.Hash()
		}
		if trace.FirstFrameAtUnix == 0 {
			trace.FirstFrameAtUnix = firstFrameUnixFromArtifacts(o.HLSRoot, event.SessionID)
		}
		trace.StopReason = string(outcome.Reason)
		trace.StopClass = model.TraceStopClassFromReason(outcome.Reason)
		traceSnapshot = trace.Clone()
		return nil
	})
	if err != nil {
		logger.Error().Err(err).
			Str("session_id", event.SessionID).
			Str("outcome_state", string(outcome.State)).
			Msg("failed to update session during finalization")
	}

	if !sessionCtx.IsVOD || outcome.State == model.SessionFailed {
		o.cleanupFiles(event.SessionID)
	}

	sessionEndTotal.WithLabelValues(string(outcome.Reason), event.ProfileID).Inc()

	logEvt := logger.Info().
		Str("event", "hls.session_end").
		Str("sid", event.SessionID).
		Str("reason", string(outcome.Reason)).
		Str("profile", event.ProfileID)
	if traceSnapshot != nil {
		if traceSnapshot.RequestProfile != "" {
			logEvt = logEvt.Str("request_profile", traceSnapshot.RequestProfile)
		}
		if traceSnapshot.ClientPath != "" {
			logEvt = logEvt.Str("client_path", traceSnapshot.ClientPath)
		}
		if traceSnapshot.InputKind != "" {
			logEvt = logEvt.Str("input_kind", traceSnapshot.InputKind)
		}
		if traceSnapshot.TargetProfileHash != "" {
			logEvt = logEvt.Str("target_profile_hash", traceSnapshot.TargetProfileHash)
		}
		if traceSnapshot.FirstFrameAtUnix > 0 {
			logEvt = logEvt.Int64("first_frame_at_unix", traceSnapshot.FirstFrameAtUnix)
		}
		if len(traceSnapshot.Fallbacks) > 0 {
			logEvt = logEvt.Int("fallback_count", len(traceSnapshot.Fallbacks)).
				Str("last_fallback_reason", traceSnapshot.Fallbacks[len(traceSnapshot.Fallbacks)-1].Reason)
		}
		if traceSnapshot.StopReason != "" {
			logEvt = logEvt.Str("stop_reason", traceSnapshot.StopReason)
		}
		if traceSnapshot.StopClass != "" {
			logEvt = logEvt.Str("stop_class", string(traceSnapshot.StopClass))
		}
	}

	if outcome.DetailDebug != "" {
		logEvt.Str("detail", outcome.DetailDebug)
	}
	logEvt.Msg("session ended")

	// Authoritative tuner-lease release for the session this process started: it uses the
	// in-memory lease handle, NOT the ContextData mirror (B), so it frees the slot even when
	// start aborted before transitionStarting persisted B (the leak window). It runs here,
	// after the terminalization UpdateSession above committed, so the lease is never freed
	// while the session is still in an intermediate state — preserving the recovery probe's
	// invariant (B set ∧ intermediate ⟹ lease held ⟹ session alive).
	if leases := *leasesPtr; leases != nil && leases.ReleaseTuner != nil {
		leases.ReleaseTuner()
	}

	// ForceReleaseLeases is retained and ADDITIVE: it releases the dedup/service lease, and
	// it is the ONLY release path for the sweeper's cold janitor (sweeper.go), which cleans
	// terminal sessions it did not start and therefore has no in-memory handle — it must read
	// B. Its tuner release via B is redundant here (idempotent + owner-scoped, so the double
	// release is a no-op) and a no-op anyway in the leak case where B is empty.
	o.ForceReleaseLeases(finalizeCtx, event.SessionID, event.ServiceRef, session)

	if !*startRecorded {
		recordStart(startResultForReason(outcome.Reason), outcome.Reason)
	}
}

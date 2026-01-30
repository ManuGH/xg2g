// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package manager

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
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/lifecycle"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/log"
	platformnet "github.com/ManuGH/xg2g/internal/platform/net"
	"github.com/rs/zerolog"
)

type sessionContext struct {
	Mode       string
	ServiceRef string
	IsVOD      bool
}

type terminationCause struct {
	IsClean          bool
	ContextCancelled bool
	Error            error
}

type finalOutcome struct {
	State       model.SessionState
	Reason      model.ReasonCode
	DetailDebug string
}

func (o *Orchestrator) resolveSession(ctx context.Context, e model.StartSessionEvent) (string, *model.SessionRecord, context.Context, error) {
	correlationID := e.CorrelationID
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
	return correlationID, session, ctx, nil
}

func (o *Orchestrator) buildSessionContext(session *model.SessionRecord, e model.StartSessionEvent) (*sessionContext, error) {
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
			return nil, newReasonError(model.RInvariantViolation, "missing recording source", nil)
		}
	}

	return &sessionContext{
		Mode:       sessionMode,
		ServiceRef: playbackSource,
		IsVOD:      session.Profile.VOD || sessionMode == model.ModeRecording,
	}, nil
}

func detectTerminationCause(ctx context.Context, retErr error) terminationCause {
	if retErr == nil {
		if ctx.Err() != nil {
			return terminationCause{ContextCancelled: true}
		}
		return terminationCause{IsClean: true}
	}
	return terminationCause{Error: retErr}
}

func mapOutcome(out lifecycle.Outcome) finalOutcome {
	return finalOutcome{
		State:       out.State,
		Reason:      out.Reason,
		DetailDebug: out.DetailDebug,
	}
}

type leaseAcquisition struct {
	Slot         int
	TunerLease   store.Lease
	DedupLease   store.Lease
	HBCancel     context.CancelFunc
	HBCtx        context.Context
	ReleaseDedup func()
}

func (o *Orchestrator) acquireLeases(
	ctx context.Context,
	sessionCtx *sessionContext,
	event model.StartSessionEvent,
	leaseOwner string,
	logger zerolog.Logger,
) (*leaseAcquisition, error) {
	res := &leaseAcquisition{
		Slot:         -1,
		ReleaseDedup: func() {},
		HBCancel:     func() {},
	}

	if sessionCtx.Mode == model.ModeLive {
		dedupKey := o.LeaseKeyFunc(event)
		dedupLease, ok, err := o.Store.TryAcquireLease(ctx, dedupKey, leaseOwner, o.LeaseTTL)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, newReasonError(model.RLeaseBusy, DedupLeaseHeldDetail, nil)
		}
		res.DedupLease = dedupLease
		res.ReleaseDedup = func() {
			// Use parent context with timeout instead of Background to respect cancellation
			ctxRel, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			if err := o.Store.ReleaseLease(ctxRel, dedupLease.Key(), dedupLease.Owner()); err != nil {
				logger.Error().Err(err).
					Str("lease_key", dedupLease.Key()).
					Str("owner", dedupLease.Owner()).
					Msg("failed to release dedup lease")
			}
		}
	}

	if sessionCtx.Mode == model.ModeLive {
		slot, tunerLease, ok, err := o.acquireTunerLease(ctx, o.TunerSlots, leaseOwner)
		if err != nil {
			res.ReleaseDedup()
			return nil, err
		}
		if !ok {
			res.ReleaseDedup()
			tunerBusyTotal.WithLabelValues().Inc()
			return nil, newReasonError(model.RLeaseBusy, "no tuner slots available", nil)
		}
		res.Slot = slot
		res.TunerLease = tunerLease
	}

	hbCtx, hbCancel := context.WithCancel(ctx)
	res.HBCancel = hbCancel
	res.HBCtx = hbCtx

	o.registerActive(event.SessionID, hbCancel)

	if sessionCtx.Mode == model.ModeLive && o.HeartbeatEvery > 0 {
		go func() {
			t := time.NewTicker(o.HeartbeatEvery)
			defer t.Stop()
			for {
				select {
				case <-hbCtx.Done():
					return
				case <-t.C:
					_, ok, err := o.Store.RenewLease(hbCtx, res.TunerLease.Key(), res.TunerLease.Owner(), o.LeaseTTL)
					if err != nil {
						logger.Warn().Err(err).Msg("heartbeat renewal error")
					} else if !ok {
						logger.Warn().Str("lease", res.TunerLease.Key()).Str("sid", event.SessionID).Msg("tuner lease lost, aborting")
						leaseLostTotalLegacy.WithLabelValues().Inc()
						_, _ = o.Store.UpdateSession(hbCtx, event.SessionID, func(r *model.SessionRecord) error {
							if !r.State.IsTerminal() {
								cause := lifecycle.NewReasonError(model.RLeaseExpired, "", nil)
								_, _ = lifecycle.Dispatch(r, lifecycle.PhaseFromState(r.State), lifecycle.Event{Kind: lifecycle.EvTerminalize}, cause, false, time.Now())
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

	return res, nil
}

// startPipeline uses the new MediaPipeline Port (Step 4.2).
func (o *Orchestrator) startPipeline(
	hbCtx context.Context,
	e model.StartSessionEvent,
	sessionCtx *sessionContext,
	currentProfileSpec model.ProfileSpec,
	tunerSlot int,
) (ports.RunHandle, error) {
	// Build StreamSpec (Domain Object)
	spec := ports.StreamSpec{
		SessionID: e.SessionID,
		Mode:      ports.ModeLive, // Default
		Format:    ports.FormatHLS,
		Quality:   ports.QualityStandard, // Hardcoded for simplified ProfileSpec mapping for now
		Source: ports.StreamSource{
			ID:        sessionCtx.ServiceRef,
			Type:      ports.SourceTuner, // Default assumes Tuner/Ref
			TunerSlot: tunerSlot,
		},
	}

	if sessionCtx.Mode == model.ModeRecording {
		spec.Mode = ports.ModeRecording
		spec.Source.Type = ports.SourceFile // Recording builds from file source usually? Or Tuner?
		// "Recording Mode" in Orchestrator meant processing a recording (viewing).
		// Wait, "ModeRecording" in Orchestrator logic meant "Viewing a Recording".
		// In that case SourceType is File.
		spec.Source.Type = ports.SourceFile
	} else if u, ok := platformnet.ParseDirectHTTPURL(sessionCtx.ServiceRef); ok {
		normalized, err := platformnet.ValidateOutboundURL(hbCtx, u.String(), o.OutboundPolicy)
		if err != nil {
			return "", newReasonError(model.RBadRequest, "outbound url rejected by policy", err)
		}
		spec.Source.Type = ports.SourceURL
		spec.Source.ID = normalized
	}

	// Profiles: map currentProfileSpec to Quality?
	// For now, Adapter builder handles details (or ignores quality spec).

	handle, err := o.Pipeline.Start(hbCtx, spec)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return "", newReasonErrorWithDetail(model.RCancelled, model.DContextCanceled, "", err)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return "", newReasonErrorWithDetail(model.RTuneTimeout, model.DDeadlineExceeded, "", err)
		}
		if errors.Is(err, ports.ErrNoValidTS) {
			detail := "preflight failed no valid ts"
			var pErr *ports.PreflightError
			if errors.As(err, &pErr) && pErr.Reason != "" {
				detail = "preflight failed no valid ts: " + pErr.Reason
			}
			return "", newReasonError(model.RPipelineStartFailed, detail, err)
		}
		return "", newReasonError(model.RPipelineStartFailed, "pipeline start failed", err)
	}

	return handle, nil
}

func (o *Orchestrator) waitForReady(
	ctx context.Context,
	hbCtx context.Context,
	e model.StartSessionEvent,
	currentProfileSpec model.ProfileSpec,
	handle ports.RunHandle,
	playlistPath string,
	vodMode bool,
	repairAttempted bool,
	startTime time.Time,
	logger zerolog.Logger,
	ttfpRecorded *bool,
) (ready bool, reason model.ReasonCode, detail string) {
	playlistReadyTimeout := 60 * time.Second
	if repairAttempted {
		playlistReadyTimeout = 20 * time.Second
	}
	if vodMode {
		playlistReadyTimeout = 2 * time.Minute
	}
	playlistPollInterval := 200 * time.Millisecond
	playlistDeadline := time.Now().Add(playlistReadyTimeout)
	ticker := time.NewTicker(playlistPollInterval)
	defer ticker.Stop()

	logger.Info().
		Str("session_id", e.SessionID).
		Str("service_ref", e.ServiceRef).
		Str("profile", currentProfileSpec.Name).
		Bool("repair_mode", repairAttempted).
		Dur("timeout", playlistReadyTimeout).
		Msg("waiting for playlist to become ready")

	for {
		// Check process health first
		status := o.Pipeline.Health(ctx, handle)
		if !status.Healthy {
			return false, model.RProcessEnded, "process died during startup: " + status.Message
		}

		ready, err := o.checkPlaylistReady(playlistPath, vodMode, ttfpRecorded, e.ProfileID, startTime)
		if err == nil && ready {
			return true, "", ""
		}

		if time.Now().After(playlistDeadline) {
			// reason, detail := o.classifyFailure(...) // Removed for now due to complexity of mapping logs
			return false, model.RPackagerFailed, "playlist not ready timeout"
		}

		select {
		case <-hbCtx.Done():
			return false, model.RClientStop, ""
		case <-ticker.C:
			// continue
		}
	}
}

func (o *Orchestrator) waitForProcessExit(ctx context.Context, handle ports.RunHandle) error {
	// Polling wait loop with exponential backoff (no max timeout for live sessions).
	const initialInterval = 500 * time.Millisecond
	const maxInterval = 5 * time.Second

	status := o.Pipeline.Health(ctx, handle)
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
			if !status.Healthy {
				// Process exited
				return nil
			}

			// Exponential backoff
			interval = interval * 2
			if interval > maxInterval {
				interval = maxInterval
			}
			ticker.Reset(interval)
		}
	}
}

func (o *Orchestrator) checkPlaylistReady(
	playlistPath string,
	vodMode bool,
	ttfpRecorded *bool,
	profileID string,
	startTime time.Time,
) (bool, error) {
	ready, err := o.checkPlaylistReadyAt(playlistPath, vodMode, ttfpRecorded, profileID, startTime)
	if ready {
		return true, nil
	}

	legacyPlaylistPath := ""
	if filepath.Base(playlistPath) == "index.m3u8" {
		sessionDir := filepath.Dir(playlistPath)
		sessionsDir := filepath.Dir(sessionDir)
		if filepath.Base(sessionsDir) == "sessions" {
			legacyPlaylistPath = filepath.Join(filepath.Dir(sessionsDir), filepath.Base(sessionDir), "stream.m3u8")
		}
	}
	if legacyPlaylistPath == "" {
		return false, err
	}

	legacyReady, legacyErr := o.checkPlaylistReadyAt(legacyPlaylistPath, vodMode, ttfpRecorded, profileID, startTime)
	if legacyReady {
		return true, nil
	}
	if err == nil {
		err = legacyErr
	}
	return false, err
}

func (o *Orchestrator) checkPlaylistReadyAt(
	playlistPath string,
	vodMode bool,
	ttfpRecorded *bool,
	profileID string,
	startTime time.Time,
) (bool, error) {
	info, err := os.Stat(playlistPath)
	if err != nil || info.Size() == 0 {
		return false, err
	}
	// #nosec G304
	content, err := os.ReadFile(filepath.Clean(playlistPath))
	if err != nil {
		return false, err
	}
	contentText := string(content)
	if !strings.Contains(contentText, "#EXTM3U") {
		return false, nil
	}
	if vodMode && !strings.Contains(contentText, "#EXT-X-ENDLIST") {
		return false, nil
	}
	segmentURI := firstSegmentFromPlaylist(content)
	if vodMode {
		if lastSegment := lastSegmentFromPlaylist(content); lastSegment != "" {
			segmentURI = lastSegment
		}
	}
	if segmentURI == "" {
		return false, nil
	}
	segmentPath := filepath.Join(filepath.Dir(playlistPath), segmentURI)
	segInfo, segErr := os.Stat(segmentPath)
	if segErr == nil && segInfo.Size() > 0 {
		if !*ttfpRecorded {
			observeTTFP(profileID, startTime)
			*ttfpRecorded = true
		}
		return true, nil
	}
	return false, nil
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

func (o *Orchestrator) transitionStarting(ctx context.Context, e model.StartSessionEvent, sessionCtx *sessionContext, slot int) error {
	o.recordTransition(model.SessionUnknown, model.SessionStarting)
	_, err := o.Store.UpdateSession(ctx, e.SessionID, func(r *model.SessionRecord) error {
		if r.State.IsTerminal() || r.State == model.SessionStopping {
			return fmt.Errorf("session state %s, aborting start", r.State)
		}
		_, err := lifecycle.Dispatch(r, lifecycle.PhaseFromState(r.State), lifecycle.Event{Kind: lifecycle.EvStartRequested}, nil, false, time.Now())
		if err != nil {
			return err
		}
		if r.ContextData == nil {
			r.ContextData = make(map[string]string)
		}
		if sessionCtx.Mode == model.ModeLive && slot >= 0 {
			r.ContextData[model.CtxKeyTunerSlot] = strconv.Itoa(slot)
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
	o.recordTransition(model.SessionPriming, model.SessionReady)
	_, err := o.Store.UpdateSession(ctx, e.SessionID, func(r *model.SessionRecord) error {
		_, err := lifecycle.Dispatch(r, lifecycle.PhaseFromState(r.State), lifecycle.Event{Kind: lifecycle.EvReady}, nil, false, time.Now())
		if err != nil {
			return err
		}
		r.PlaylistPublishedAt = time.Now() // PR-P3-2: Truth for buffering/active derivation
		r.LastAccessUnix = time.Now().Unix()
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
	repairAttempted := false
	ttfpRecorded := false

	var handle ports.RunHandle
	var failReason model.ReasonCode
	var failDetail string

	// Simplified Retry Logic (Step 4.2 Decoupling)
	// We stripped classifyFailure (ffmpeg logs) so we treat all failures as "unknown" or "packager".
	// For now, we abort after 1 attempt or blind retry.
	// If fails, we just return error.

	var err error
	handle, err = o.startPipeline(hbCtx, e, sessionCtx, currentProfileSpec, tunerSlot)
	if err != nil {
		return "", model.ProfileSpec{}, err
	}

	o.recordTransition(model.SessionStarting, model.SessionPriming)
	_, err = o.Store.UpdateSession(ctx, e.SessionID, func(r *model.SessionRecord) error {
		if r.State.IsTerminal() || r.State == model.SessionStopping {
			return fmt.Errorf("session state %s, aborting priming", r.State)
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
		go o.startHeartbeatMonitor(hbCtx, e.SessionID)
	}

	playlistReadyResult := false
	if o.HLSRoot != "" {
		playlistPath := filepath.Join(o.HLSRoot, "sessions", e.SessionID, "index.m3u8")
		var waitReason model.ReasonCode
		var waitDetail string

		playlistReadyResult, waitReason, waitDetail = o.waitForReady(
			ctx, hbCtx, e, currentProfileSpec, handle,
			playlistPath, sessionCtx.IsVOD, repairAttempted,
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

	// Failure Handling
	stopCtx, stopCancel := context.WithTimeout(context.Background(), o.PipelineStopTimeout)
	_ = o.Pipeline.Stop(stopCtx, handle)
	stopCancel()

	return "", model.ProfileSpec{}, newReasonError(failReason, failDetail, nil)
}

func (o *Orchestrator) finalizeDeferred(
	ctx context.Context,
	event model.StartSessionEvent,
	sessionPtr **model.SessionRecord,
	sessionCtx *sessionContext,
	logger zerolog.Logger,
	startRecorded *bool,
	recordStart func(string, model.ReasonCode),
	startResultForReason func(model.ReasonCode) string,
	retErr *error,
) {
	session := *sessionPtr
	cause := detectTerminationCause(ctx, *retErr)
	errForOutcome := cause.Error
	if errForOutcome == nil && cause.ContextCancelled {
		errForOutcome = context.Canceled
	}
	reason, _, detailDebug := lifecycle.ClassifyReason(errForOutcome)
	if reason == model.RLeaseBusy && detailDebug == DedupLeaseHeldDetail {
		return
	}
	var outcome finalOutcome

	// Use bounded timeout context for finalization instead of Background
	finalizeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := o.Store.UpdateSession(finalizeCtx, event.SessionID, func(r *model.SessionRecord) error {
		if r.State.IsTerminal() && r.State != model.SessionStopping {
			return nil
		}

		stopIntent := r.State == model.SessionStopping || r.Reason == model.RClientStop
		errForOutcome := cause.Error
		if errForOutcome == nil && cause.ContextCancelled {
			errForOutcome = context.Canceled
		}
		phase := lifecycle.PhaseFromState(r.State)
		if cause.IsClean && sessionCtx.IsVOD {
			phase = lifecycle.PhaseVODComplete
		}
		fromState := r.State
		tr, err := lifecycle.Dispatch(r, phase, lifecycle.Event{Kind: lifecycle.EvTerminalize}, errForOutcome, stopIntent, time.Now())
		if err != nil {
			return err
		}
		outcome = finalOutcome{State: tr.To, Reason: tr.Reason, DetailDebug: tr.DetailDebug}

		o.recordTransition(fromState, outcome.State)

		r.UpdatedAtUnix = time.Now().Unix()
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

	if outcome.DetailDebug != "" {
		logEvt.Str("detail", outcome.DetailDebug)
	}
	logEvt.Msg("session ended")

	o.ForceReleaseLeases(finalizeCtx, event.SessionID, event.ServiceRef, session)

	if !*startRecorded {
		recordStart(startResultForReason(outcome.Reason), outcome.Reason)
	}
}

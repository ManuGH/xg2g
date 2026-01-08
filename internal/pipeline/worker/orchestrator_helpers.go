// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

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
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/pipeline/exec"
	"github.com/ManuGH/xg2g/internal/pipeline/model"
	"github.com/ManuGH/xg2g/internal/pipeline/store"
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
	State  model.SessionState
	Reason model.ReasonCode
	Detail string
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

func (o *Orchestrator) mapCauseToOutcome(cause terminationCause, vodMode bool) finalOutcome {
	finalState := model.SessionFailed
	reason := model.RProcessEnded
	detail := ""

	if cause.IsClean {
		if vodMode {
			finalState = model.SessionDraining
			reason = model.RNone
			detail = "recording completed"
		} else {
			finalState = model.SessionFailed
			reason = model.RProcessEnded
		}
	} else if cause.ContextCancelled {
		finalState = model.SessionStopped
		reason = model.RClientStop
	} else {
		reason, detail = classifyReason(cause.Error)
		if reason == model.RClientStop {
			finalState = model.SessionStopped
		}
	}

	return finalOutcome{
		State:  finalState,
		Reason: reason,
		Detail: detail,
	}
}

// finalizeDeferred handles session finalization logic (extracted from defer block).
// CTO-CRITICAL: This is a 1:1 wrapper of the original defer block.
// Order MUST be preserved: outcome detection → store update → cleanup → metrics → logging → lease release → start recording
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

	// 1. Dedup Lock (ServiceRef) - Transient (Phase 8-2)
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
			ctxRel, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = o.Store.ReleaseLease(ctxRel, dedupLease.Key(), dedupLease.Owner())
		}
	}

	// 2. Resource Lock (Tuner Slot) - Persistent (Phase 8-2)
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

	// Heartbeat loop: Renew TUNER Lease explicitly
	hbCtx, hbCancel := context.WithCancel(ctx)
	res.HBCancel = hbCancel
	res.HBCtx = hbCtx

	// Phase 9-2: Lifecycle Management (Store CancelFunc)
	o.registerActive(event.SessionID, hbCancel)

	if sessionCtx.Mode == model.ModeLive {
		go func() {
			t := time.NewTicker(o.HeartbeatEvery)
			defer t.Stop()
			for {
				select {
				case <-hbCtx.Done():
					return
				case <-t.C:
					// Renew Tuner Lease (Risk 8-2: Must probe correct lease)
					_, ok, err := o.Store.RenewLease(hbCtx, res.TunerLease.Key(), res.TunerLease.Owner(), o.LeaseTTL)
					if err != nil {
						logger.Warn().Err(err).Msg("heartbeat renewal error")
					} else if !ok {
						logger.Warn().Str("lease", res.TunerLease.Key()).Str("sid", event.SessionID).Msg("tuner lease lost, aborting")
						leaseLostTotalLegacy.WithLabelValues().Inc()

						// Fix 11-3: Lease Robustness
						_, _ = o.Store.UpdateSession(hbCtx, event.SessionID, func(r *model.SessionRecord) error {
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

	return res, nil
}

func (o *Orchestrator) startPipeline(
	hbCtx context.Context,
	e model.StartSessionEvent,
	sessionCtx *sessionContext,
	currentProfileSpec model.ProfileSpec,
) (exec.Transcoder, error) {
	// Create fresh transcoder instance (Runner is one-shot)
	transcoder, err := o.ExecFactory.NewTranscoder()
	if err != nil {
		return nil, newReasonError(model.RFFmpegStartFailed, "transcoder init failed", err)
	}

	// 2a. Start Transcoder
	if err := transcoder.Start(hbCtx, e.SessionID, sessionCtx.ServiceRef, currentProfileSpec, e.StartMs); err != nil {
		return nil, newReasonError(model.RFFmpegStartFailed, "", err)
	}

	return transcoder, nil
}

func (o *Orchestrator) waitForReady(
	ctx context.Context,
	hbCtx context.Context,
	e model.StartSessionEvent,
	currentProfileSpec model.ProfileSpec,
	transcoder exec.Transcoder,
	playlistPath string,
	vodMode bool,
	repairAttempted bool,
	startTime time.Time,
	ffmpegStartTime time.Time,
	logger zerolog.Logger,
	ttfpRecorded *bool,
) (ready bool, reason model.ReasonCode, detail string) {
	playlistReadyTimeout := 45 * time.Second
	if repairAttempted {
		playlistReadyTimeout = 20 * time.Second
	}
	if vodMode {
		playlistReadyTimeout = 2 * time.Minute
	}
	playlistPollInterval := 200 * time.Millisecond
	playlistDeadline := time.Now().Add(playlistReadyTimeout)

	logger.Info().
		Str("session_id", e.SessionID).
		Str("service_ref", e.ServiceRef).
		Str("profile", currentProfileSpec.Name).
		Bool("repair_mode", repairAttempted).
		Dur("timeout", playlistReadyTimeout).
		Msg("waiting for playlist to become ready")

	for {
		// Success condition
		ready, err := o.checkPlaylistReady(playlistPath, vodMode, ttfpRecorded, e.ProfileID, startTime)
		if err == nil && ready {
			return true, "", ""
		}

		// Timeout condition
		if time.Now().After(playlistDeadline) {
			reason, detail := o.classifyFailure(playlistPath, transcoder, ffmpegStartTime, playlistReadyTimeout)
			return false, reason, detail
		}

		select {
		case <-hbCtx.Done():
			return false, model.RClientStop, "context canceled"
		case <-time.After(playlistPollInterval):
			// continue
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
	info, err := os.Stat(playlistPath)
	if err != nil || info.Size() == 0 {
		return false, err
	}
	content, err := os.ReadFile(playlistPath)
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

func (o *Orchestrator) classifyFailure(
	playlistPath string,
	transcoder exec.Transcoder,
	ffmpegStartTime time.Time,
	timeout time.Duration,
) (model.ReasonCode, string) {
	sessionDir := filepath.Dir(playlistPath)
	entries, _ := os.ReadDir(sessionDir)
	ffmpegLogs := transcoder.LastLogLines(20)

	reason := model.RPackagerFailed
	detail := fmt.Sprintf("playlist not ready after %s", timeout)

	corruptSignatures := []string{
		"decode_slice_header error", "no frame!", "non-existing PPS", "non-existing SPS",
		"mmco: unref short failure", "number of reference frames",
	}
	signatureFound := false
	for _, line := range ffmpegLogs {
		for _, sig := range corruptSignatures {
			if strings.Contains(line, sig) {
				signatureFound = true
				break
			}
		}
		if signatureFound {
			break
		}
	}

	hasSegment := false
	for _, ent := range entries {
		name := ent.Name()
		if strings.HasSuffix(name, ".ts") || strings.HasSuffix(name, ".m4s") {
			hasSegment = true
			break
		}
	}

	if signatureFound && !hasSegment {
		reason = model.RUpstreamCorrupt
		detail = "upstream stream corrupt or missing keyframes"
	}

	return reason, detail
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
		r.State = model.SessionStarting
		r.UpdatedAtUnix = time.Now().Unix()
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
		r.State = model.SessionReady
		r.UpdatedAtUnix = time.Now().Unix()
		r.LastAccessUnix = time.Now().Unix()
		return nil
	})
	return err
}

func (o *Orchestrator) setupTuner(slot int, sessionCtx *sessionContext) (exec.Tuner, error) {
	if sessionCtx.Mode != model.ModeLive {
		return nil, nil
	}
	tuner, err := o.ExecFactory.NewTuner(slot)
	if err != nil {
		return nil, err
	}
	return tuner, nil
}

func (o *Orchestrator) tunePlaybackSource(
	hbCtx context.Context,
	e model.StartSessionEvent,
	sessionCtx *sessionContext,
	tuner exec.Tuner,
	logger zerolog.Logger,
) error {
	readyStart := time.Now()
	var tuneErr error
	if sessionCtx.Mode == model.ModeRecording {
		logger.Info().Str("source", sessionCtx.ServiceRef).Msg("worker: recording mode, skipping tune")
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
		failReason := "other"
		if errors.Is(tuneErr, context.DeadlineExceeded) {
			failReason = "timeout"
		} else if errors.Is(tuneErr, context.Canceled) {
			failReason = "canceled"
		}
		readyOutcomeTotal.WithLabelValues(failReason).Inc()
	}
	readyDuration.WithLabelValues(outcome).Observe(readyDurationVal)

	if tuneErr != nil {
		if errors.Is(tuneErr, context.Canceled) {
			return tuneErr
		}
		if errors.Is(tuneErr, context.DeadlineExceeded) {
			return newReasonError(model.RTuneTimeout, "", tuneErr)
		}
		return newReasonError(model.RTuneFailed, "", tuneErr)
	}
	return nil
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
) (exec.Transcoder, model.ProfileSpec, error) {
	initialProfileSpec := session.Profile
	if sessionCtx.IsVOD {
		initialProfileSpec.VOD = true
	}
	currentProfileSpec := initialProfileSpec
	repairAttempted := false
	ttfpRecorded := false

	var transcoder exec.Transcoder
	var failReason model.ReasonCode
	var failDetail string

	for attempt := 1; attempt <= 2; attempt++ {
		ffmpegStartTime := time.Now()

		var err error
		transcoder, err = o.startPipeline(hbCtx, e, sessionCtx, currentProfileSpec)
		if err != nil {
			return nil, model.ProfileSpec{}, err
		}

		if attempt == 1 {
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
				return nil, model.ProfileSpec{}, err
			}
			if o.HLSRoot != "" {
				sessionDir := filepath.Join(o.HLSRoot, "sessions", e.SessionID)
				go observeFirstSegment(hbCtx, sessionDir, startTime, e.ProfileID)
			}
		}

		playlistReadyResult := false
		if o.HLSRoot != "" {
			playlistPath := filepath.Join(o.HLSRoot, "sessions", e.SessionID, "index.m3u8")
			var waitReason model.ReasonCode
			var waitDetail string
			playlistReadyResult, waitReason, waitDetail = o.waitForReady(
				ctx, hbCtx, e, currentProfileSpec, transcoder,
				playlistPath, sessionCtx.IsVOD, repairAttempted,
				startTime, ffmpegStartTime, logger, &ttfpRecorded,
			)

			if !playlistReadyResult {
				failReason = waitReason
				failDetail = waitDetail
			}
		} else {
			playlistReadyResult = true
		}

		if playlistReadyResult {
			return transcoder, currentProfileSpec, nil
		}

		// Failure Handling
		stopCtx, stopCancel := context.WithTimeout(context.Background(), o.ffmpegStopTimeout())
		_ = transcoder.Stop(stopCtx)
		stopCancel()

		retry, nextProfile, nextRepair := o.handleExecutionFailure(failReason, currentProfileSpec, initialProfileSpec, sessionCtx.IsVOD, repairAttempted, logger, e.SessionID)
		if retry {
			currentProfileSpec = nextProfile
			repairAttempted = nextRepair
			continue
		}
		return nil, model.ProfileSpec{}, newReasonError(failReason, failDetail, nil)
	}
	return nil, model.ProfileSpec{}, newReasonError(model.RUnknown, "execution loop failed", nil)
}

func (o *Orchestrator) handleExecutionFailure(
	failReason model.ReasonCode,
	currentProfile model.ProfileSpec,
	initialProfile model.ProfileSpec,
	isVOD bool,
	repairAttempted bool,
	logger zerolog.Logger,
	sid string,
) (bool, model.ProfileSpec, bool) {
	if repairAttempted || failReason != model.RUpstreamCorrupt || isVOD {
		return false, currentProfile, repairAttempted
	}

	if currentProfile.TranscodeVideo {
		logger.Warn().Str("session_id", sid).Str("reason", string(failReason)).Msg("initiating fallback switch: disabling video transcoding (copy + AAC)")
		nextProfile := initialProfile
		nextProfile.Name = "copy"
		nextProfile.TranscodeVideo = false
		if nextProfile.AudioBitrateK == 0 {
			nextProfile.AudioBitrateK = 192
		}
		o.cleanupFiles(sid)
		return true, nextProfile, true
	}

	logger.Warn().Str("session_id", sid).Str("reason", string(failReason)).Msg("initiating repair switch: enabling transcoding")
	nextProfile := initialProfile
	nextProfile.Name = "repair"
	nextProfile.TranscodeVideo = true
	nextProfile.VideoCRF = 24
	nextProfile.Deinterlace = false
	nextProfile.AudioBitrateK = 192
	o.cleanupFiles(sid)
	return true, nextProfile, true
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
	// Determine Outcome via pure functions
	cause := detectTerminationCause(ctx, *retErr)
	outcome := o.mapCauseToOutcome(cause, sessionCtx.IsVOD)

	// Fix 11-4: Dedup Race Condition (Robust)
	if outcome.Reason == model.RLeaseBusy && outcome.Detail == DedupLeaseHeldDetail {
		logger.Debug().
			Bool("dedup_replay", true).
			Str("service_ref", event.ServiceRef).
			Str("sid", event.SessionID).
			Str("correlation_id", event.CorrelationID).
			Msg("dedup busy replay: skipping finalizer side effects")
		return
	}

	// Update Store
	// CTO-CRITICAL: Uses context.Background() as per original
	_, _ = o.Store.UpdateSession(context.Background(), event.SessionID, func(r *model.SessionRecord) error {
		if r.State.IsTerminal() && r.State != model.SessionStopping {
			return nil
		}

		o.recordTransition(r.State, outcome.State)

		r.State = outcome.State
		if outcome.State == model.SessionFailed {
			r.PipelineState = model.PipeFail
		} else {
			r.PipelineState = model.PipeStopped
		}
		if r.Reason == "" || r.Reason == model.RNone || r.Reason == model.RUnknown {
			r.Reason = outcome.Reason
		}
		if outcome.Detail != "" {
			r.ReasonDetail = outcome.Detail
		}
		r.UpdatedAtUnix = time.Now().Unix()
		return nil
	})

	// PR 9-3: On-Stop Cleanup
	if !sessionCtx.IsVOD || outcome.State == model.SessionFailed {
		o.cleanupFiles(event.SessionID)
	}

	// Phase 9-4: Golden Signals
	sessionEndTotal.WithLabelValues(string(outcome.Reason), event.ProfileID).Inc()

	logEvt := logger.Info().
		Str("event", "hls.session_end").
		Str("sid", event.SessionID).
		Str("reason", string(outcome.Reason)).
		Str("profile", event.ProfileID)

	if outcome.Detail != "" {
		logEvt.Str("detail", outcome.Detail)
	}
	logEvt.Msg("session ended")

	// Fix 17: Force Release Leases
	o.ForceReleaseLeases(context.Background(), event.SessionID, event.ServiceRef, session)

	if !*startRecorded {
		recordStart(startResultForReason(outcome.Reason), outcome.Reason)
	}
}

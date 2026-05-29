// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/go-chi/chi/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/rs/zerolog"

	v3sessions "github.com/ManuGH/xg2g/internal/control/http/v3/sessions"
	"github.com/ManuGH/xg2g/internal/control/recordings/capreg"
	"github.com/ManuGH/xg2g/internal/control/recordings/runtimepolicy"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/log"
	pipelineapi "github.com/ManuGH/xg2g/internal/pipeline/api"
	"github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

// Responsibility: Handles Session status, debugging, and client feedback/reporting.
// Non-goals: Initializing streams (see intents).

const (
	fallbackRestartPollInterval = 50 * time.Millisecond
	fallbackRestartTimeout      = 5 * time.Second
	playbackErrorCodeDecode     = 3
	playbackErrorCodeHlsStalled = 4
)

const safariFallbackBrowserUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.4 Safari/605.1.15"

// handleV3SessionsDebug dumps all sessions from the store (Admin only).
// Authorization: Requires v3:admin scope (enforced by route middleware).
// Supports pagination via query parameters: offset (default 0) and limit (default 100, max 1000).
func (s *Server) handleV3SessionsDebug(w http.ResponseWriter, r *http.Request) {
	offset, limit := parsePaginationParams(r)
	result, err := s.sessionsProcessor().ListSessionsDebug(r.Context(), v3sessions.ListSessionsDebugRequest{
		Offset: offset,
		Limit:  limit,
	})
	if err != nil {
		writeSessionsDebugServiceError(w, r, err)
		return
	}

	writeSessionsDebugResponse(w, result)
}

// handleV3SessionState returns a single session state.
// Authorization: Requires v3:read scope (enforced by route middleware).
func (s *Server) handleV3SessionState(w http.ResponseWriter, r *http.Request) {
	deps := s.sessionsModuleDeps()
	sessionID := chi.URLParam(r, "sessionID")
	result, err := s.sessionsProcessor().GetSession(r.Context(), v3sessions.GetSessionRequest{
		SessionID: sessionID,
		RequestID: requestID(r.Context()),
		Now:       time.Now(),
		HLSRoot:   deps.cfg.HLS.Root,
	})
	if err != nil {
		writeSessionStateServiceError(w, r, deps.cfg.HLS.Root, err)
		return
	}

	writeSessionStateResponse(w, r, deps.cfg.HLS.Root, result)
}

// ReportPlaybackFeedback handles POST /sessions/{sessionId}/feedback
func (s *Server) ReportPlaybackFeedback(w http.ResponseWriter, r *http.Request, sessionId openapi_types.UUID) {
	deps := s.sessionsModuleDeps()
	bus := deps.bus
	store := deps.store

	if bus == nil || store == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrV3NotAvailable)
		return
	}

	// 2. Decode Feedback
	var req PlaybackFeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, "invalid body")
		return
	}

	ctx := r.Context()
	sess, err := store.GetSession(ctx, sessionId.String())
	isDecodeError := shouldTriggerPlaybackFallback(req, sess)
	if err == nil && sess != nil {
		s.recordPlaybackFeedbackObservation(ctx, sess, req)
	}
	s.logPlaybackFeedback(sessionId.String(), sess, req, isDecodeError)

	if !isDecodeError {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	// 4. Trigger Fallback
	if err != nil {
		RespondError(w, r, http.StatusNotFound, ErrSessionFeedbackNotFound)
		return
	}
	if err != nil || sess == nil {
		RespondError(w, r, http.StatusNotFound, ErrSessionFeedbackNotFound)
		return
	}
	if sess.State != model.SessionReady && sess.State != model.SessionDraining {
		log.L().Warn().
			Str("sessionId", sessionId.String()).
			Str("state", string(sess.State)).
			Msg("ignoring playback feedback before session reached serving state")
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if !sessionHasFirstFrameArtifact(deps.cfg.HLS.Root, sess.SessionID) {
		log.L().Warn().
			Str("sessionId", sessionId.String()).
			Str("state", string(sess.State)).
			Msg("ignoring playback fallback request before first video frame was observed")
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if sess.PlaybackTrace != nil && sess.PlaybackTrace.Operator != nil && sess.PlaybackTrace.Operator.ClientFallbackDisabled {
		log.L().Warn().
			Str("sessionId", sessionId.String()).
			Msg("ignoring playback feedback because client fallback is disabled by persisted operator override")
		w.WriteHeader(http.StatusAccepted)
		return
	}

	// Atomic Update via Store
	var changed bool
	updatedSess, err := store.UpdateSession(ctx, sessionId.String(), func(s *model.SessionRecord) error {
		// If we are already in Repair profile, nothing more to do
		if s.Profile.Name == profiles.ProfileRepair {
			return nil
		}
		now := time.Now()
		trace := ensureSessionPlaybackTrace(s)
		if trace.RequestProfile == "" {
			trace.RequestProfile = profiles.PublicProfileName(s.Profile.Name)
		}
		if trace.ClientPath == "" && s.ContextData != nil {
			trace.ClientPath = strings.TrimSpace(s.ContextData[model.CtxKeyClientPath])
		}
		if trace.InputKind == "" && s.ContextData != nil {
			trace.InputKind = strings.TrimSpace(s.ContextData[model.CtxKeySourceType])
		}
		if trace.FirstFrameAtUnix == 0 {
			trace.FirstFrameAtUnix = sessionFirstFrameUnix(deps.cfg.HLS.Root, s.SessionID)
		}
		fromTarget := model.TraceTargetProfileFromProfile(s.Profile)
		fromHash := ""
		if fromTarget != nil {
			fromHash = fromTarget.Hash()
		}

		fallbackPlan := nextPlaybackFeedbackPlan(s.Profile, s.ServiceRef)
		s.Profile = fallbackPlan.profile

		s.FallbackReason = fmt.Sprintf("client_report:code=%d", derefInt(req.Code))
		s.FallbackAtUnix = now.Unix()
		s.State = model.SessionStarting
		s.PipelineState = model.PipeStopRequested
		s.StopReason = ""
		toTarget := model.TraceTargetProfileFromProfile(s.Profile)
		toHash := ""
		if toTarget != nil {
			toHash = toTarget.Hash()
		}
		trace.PolicyModeHint = s.Profile.PolicyModeHint
		trace.EffectiveModeSource = s.Profile.EffectiveModeSource
		trace.TargetProfile = toTarget
		trace.TargetProfileHash = toHash
		trace.StopReason = ""
		trace.StopClass = model.PlaybackStopClassNone
		trace.Fallbacks = append(trace.Fallbacks, model.PlaybackFallbackTrace{
			AtUnix:          now.Unix(),
			Trigger:         "client_feedback",
			Reason:          s.FallbackReason,
			PlanID:          string(fallbackPlan.id),
			PlanReason:      string(fallbackPlan.reason),
			FromProfileHash: fromHash,
			ToProfileHash:   toHash,
		})
		changed = true
		return nil
	})

	if err != nil {
		log.L().Error().Err(err).Msg("failed to update session for fallback")
		RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, "store update failed")
		return
	}

	if !changed {
		log.L().Info().Str("sessionId", sessionId.String()).Msg("fallback already active, ignoring request")
		w.WriteHeader(http.StatusAccepted)
		return
	}

	// Trigger Restart only if we actually applied the fallback
	sess = updatedSess
	log.L().
		Warn().
		Str("sessionId", sess.SessionID).
		Str("fallback_plan", sessionFallbackPlanID(sess)).
		Str("fallback_plan_reason", sessionFallbackPlanReason(sess)).
		Str("fallback_profile", sess.Profile.Name).
		Str("fallback_container", sess.Profile.Container).
		Msg("activating safari fallback due to client error")

	// Stop existing session
	stopEvt := model.StopSessionEvent{
		Type:          model.EventStopSession,
		SessionID:     sess.SessionID,
		Reason:        model.RClientStop,
		CorrelationID: sess.CorrelationID,
		RequestedAtUN: time.Now().Unix(),
	}
	if err := bus.Publish(ctx, string(model.EventStopSession), stopEvt); err != nil {
		log.L().Error().Err(err).Msg("failed to publish stop event during fallback")
	}

	s.scheduleFallbackRestart(bus, store, sess)

	w.WriteHeader(http.StatusAccepted)
}

func shouldPreferSafariTSFallbackForServiceRef(serviceRef string) bool {
	return serviceRefEnvContainsNormalized("XG2G_SAFARI_FORCE_COPY_SERVICE_REFS", serviceRef)
}

func shouldTriggerPlaybackFallback(req PlaybackFeedbackRequest, sess *model.SessionRecord) bool {
	if req.Event != PlaybackFeedbackRequestEventError || req.Code == nil {
		return false
	}

	switch *req.Code {
	case playbackErrorCodeDecode:
		return true
	case playbackErrorCodeHlsStalled:
		return shouldEscalateIOSAV1HlsStall(sess)
	default:
		return false
	}
}

func shouldEscalateIOSAV1HlsStall(sess *model.SessionRecord) bool {
	if sess == nil {
		return false
	}

	if strings.TrimSpace(sessionClientFamilyForFeedback(sess)) != playbackprofile.ClientIOSSafariNative {
		return false
	}
	if strings.TrimSpace(sessionClientPathForFeedback(sess)) != "hlsjs" {
		return false
	}

	return strings.EqualFold(strings.TrimSpace(sessionTargetVideoCodecForFeedback(sess)), "av1")
}

func sessionClientFamilyForFeedback(sess *model.SessionRecord) string {
	if sess == nil {
		return ""
	}
	if sess.ContextData != nil {
		if family := strings.TrimSpace(sess.ContextData[model.CtxKeyClientFamily]); family != "" {
			return family
		}
	}
	if sess.PlaybackTrace != nil && sess.PlaybackTrace.Client != nil {
		return strings.TrimSpace(sess.PlaybackTrace.Client.ClientFamily)
	}
	return ""
}

func sessionClientPathForFeedback(sess *model.SessionRecord) string {
	if sess == nil {
		return ""
	}
	if sess.ContextData != nil {
		if clientPath := strings.TrimSpace(sess.ContextData[model.CtxKeyClientPath]); clientPath != "" {
			return clientPath
		}
	}
	if sess.PlaybackTrace != nil {
		return strings.TrimSpace(sess.PlaybackTrace.ClientPath)
	}
	return ""
}

func sessionTargetVideoCodecForFeedback(sess *model.SessionRecord) string {
	if sess == nil {
		return ""
	}
	if sess.PlaybackTrace != nil {
		if sess.PlaybackTrace.TargetProfile != nil {
			if codec := strings.TrimSpace(sess.PlaybackTrace.TargetProfile.Video.Codec); codec != "" {
				return codec
			}
		}
		if sess.PlaybackTrace.FFmpegPlan != nil {
			if codec := strings.TrimSpace(sess.PlaybackTrace.FFmpegPlan.VideoCodec); codec != "" {
				return codec
			}
		}
		if codec := strings.TrimSpace(sess.PlaybackTrace.AutoCodecSelected); codec != "" {
			return codec
		}
	}
	return strings.TrimSpace(sess.Profile.VideoCodec)
}

func serviceRefEnvContainsNormalized(envKey, targetRef string) bool {
	targetRef = normalizeServiceRefForEnv(targetRef)
	if targetRef == "" {
		return false
	}
	raw := strings.TrimSpace(config.ParseString(envKey, ""))
	if raw == "" {
		return false
	}
	for _, candidate := range strings.Split(raw, ",") {
		if normalizeServiceRefForEnv(candidate) == targetRef {
			return true
		}
	}
	return false
}

func normalizeServiceRefForEnv(raw string) string {
	ref := strings.TrimSpace(raw)
	if ref == "" {
		return ""
	}
	ref = strings.TrimRight(ref, ":")
	if isHexColonServiceRefForEnv(ref) {
		return strings.ToUpper(ref)
	}
	return ref
}

func isHexColonServiceRefForEnv(ref string) bool {
	if ref == "" || !strings.Contains(ref, ":") {
		return false
	}
	for _, ch := range ref {
		switch {
		case ch == ':':
		case ch >= '0' && ch <= '9':
		case ch >= 'a' && ch <= 'f':
		case ch >= 'A' && ch <= 'F':
		default:
			return false
		}
	}
	return true
}

func (s *Server) scheduleFallbackRestart(eventBus bus.Bus, store SessionStateStore, sess *model.SessionRecord) {
	s.scheduleSessionRestart(eventBus, store, sess)
}

func (s *Server) scheduleSessionRestart(eventBus bus.Bus, store SessionStateStore, sess *model.SessionRecord) {
	if eventBus == nil || store == nil || sess == nil {
		return
	}

	sessionID := sess.SessionID
	serviceRef := sess.ServiceRef
	correlationID := sess.CorrelationID
	restartCtx := s.runtimeContextOrBackground()

	go func() {
		// Detached, long-lived goroutine on the live path: a panic here would
		// otherwise take down the whole daemon. The bus is now panic-safe; this
		// is defence-in-depth for any future fault in the restart path.
		defer func() {
			if r := recover(); r != nil {
				log.L().Error().
					Interface("panic", r).
					Str("sessionId", sessionID).
					Str("stack", string(debug.Stack())).
					Msg("session restart goroutine recovered from panic")
			}
		}()

		ctx, cancel := context.WithTimeout(restartCtx, fallbackRestartTimeout)
		defer cancel()

		if err := waitForTerminalSession(ctx, store, sessionID); err != nil {
			log.L().Error().Err(err).Str("sessionId", sessionID).Msg("failed to observe terminal state before session restart")
			return
		}

		startEvt := model.StartSessionEvent{
			Type:          model.EventStartSession,
			SessionID:     sessionID,
			ServiceRef:    serviceRef,
			ProfileID:     sess.Profile.Name,
			CorrelationID: correlationID,
			RequestedAtUN: time.Now().Unix(),
		}

		if err := eventBus.Publish(ctx, string(model.EventStartSession), startEvt); err != nil {
			log.L().Error().Err(err).Str("sessionId", sessionID).Msg("failed to publish session restart event")
		}
	}()
}

func waitForTerminalSession(ctx context.Context, store SessionStateStore, sessionID string) error {
	ticker := time.NewTicker(fallbackRestartPollInterval)
	defer ticker.Stop()

	for {
		sess, err := store.GetSession(ctx, sessionID)
		if err != nil {
			return err
		}
		if sess == nil || sess.State.IsTerminal() {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Server) runtimeContextOrBackground() context.Context {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.runtimeCtx != nil {
		return s.runtimeCtx
	}
	return context.Background()
}

func sessionHasFirstFrameArtifact(hlsRoot, sessionID string) bool {
	if !model.IsSafeSessionID(sessionID) {
		return false
	}
	markerPath := model.SessionFirstFrameMarkerPath(hlsRoot, sessionID)
	if markerPath == "" {
		return false
	}
	info, err := os.Stat(markerPath)
	return err == nil && !info.IsDir()
}

func sessionFirstFrameUnix(hlsRoot, sessionID string) int64 {
	if !model.IsSafeSessionID(sessionID) {
		return 0
	}
	markerPath := model.SessionFirstFrameMarkerPath(hlsRoot, sessionID)
	if markerPath == "" {
		return 0
	}
	info, err := os.Stat(markerPath)
	if err != nil || info.IsDir() {
		return 0
	}
	return info.ModTime().Unix()
}

func ensureSessionPlaybackTrace(session *model.SessionRecord) *model.PlaybackTrace {
	if session.PlaybackTrace == nil {
		session.PlaybackTrace = &model.PlaybackTrace{}
	}
	return session.PlaybackTrace
}

func (s *Server) recordPlaybackFeedbackObservation(ctx context.Context, sess *model.SessionRecord, req PlaybackFeedbackRequest) {
	if s == nil || sess == nil {
		return
	}
	observedAt := time.Now().UTC()
	if isSoftStartupPlaybackWarning(req) && sessionInPlaybackStartupWarmup(sess, s.cfg.HLS.Root, playbackStartupSoftWarningWarmup, observedAt) {
		log.L().Info().
			Str("sessionId", sess.SessionID).
			Str("event", string(req.Event)).
			Int("code", derefInt(req.Code)).
			Str("msg", derefString(req.Message)).
			Time("startupWarmupUntil", sessionPlaybackStartupWarmupUntil(sess, s.cfg.HLS.Root, playbackStartupSoftWarningWarmup)).
			Msg("ignoring soft startup playback warning during warmup")
		return
	}

	s.mu.RLock()
	registry := s.capabilityRegistry
	s.mu.RUnlock()
	if registry == nil {
		return
	}

	decisionRequestID := ""
	if sess.ContextData != nil {
		decisionRequestID = strings.TrimSpace(sess.ContextData[model.CtxKeyDecisionRequest])
	}

	linked := capreg.PlaybackObservation{}
	if decisionRequestID != "" {
		var (
			ok  bool
			err error
		)
		linked, ok, err = registry.LookupDecisionObservation(ctx, decisionRequestID)
		if err != nil {
			log.L().Warn().Err(err).Str("sessionId", sess.SessionID).Str("decisionRequestId", decisionRequestID).Msg("capability registry decision lookup failed")
		}
		if !ok {
			linked = capreg.PlaybackObservation{}
		}
	}

	trace := sess.PlaybackTrace
	target := traceTargetProfileForFeedback(sess)
	observation := capreg.PlaybackObservation{
		ObservedAt:         observedAt,
		RequestID:          decisionRequestID,
		ObservationKind:    "feedback",
		Outcome:            playbackFeedbackOutcome(req.Event, derefInt(req.Code)),
		SessionID:          strings.TrimSpace(sess.SessionID),
		SourceRef:          firstNonEmptyFeedback(strings.TrimSpace(sess.ServiceRef), linked.SourceRef),
		SourceFingerprint:  linked.SourceFingerprint,
		SubjectKind:        firstNonEmptyFeedback(feedbackSubjectKind(sess), linked.SubjectKind),
		RequestedIntent:    firstNonEmptyFeedback(feedbackTraceIntent(trace, true), linked.RequestedIntent),
		ResolvedIntent:     firstNonEmptyFeedback(feedbackTraceIntent(trace, false), linked.ResolvedIntent),
		Mode:               feedbackMode(linked.Mode, target),
		SelectedContainer:  feedbackContainer(linked.SelectedContainer, target),
		SelectedVideoCodec: feedbackVideoCodec(linked.SelectedVideoCodec, target),
		SelectedAudioCodec: feedbackAudioCodec(linked.SelectedAudioCodec, target),
		SourceWidth:        linked.SourceWidth,
		SourceHeight:       linked.SourceHeight,
		SourceFPS:          linked.SourceFPS,
		HostFingerprint:    linked.HostFingerprint,
		DeviceFingerprint:  linked.DeviceFingerprint,
		ClientCapsHash:     linked.ClientCapsHash,
		Network:            linked.Network,
		FeedbackEvent:      string(req.Event),
		FeedbackCode:       derefInt(req.Code),
		FeedbackMessage:    derefString(req.Message),
	}

	if err := registry.RecordObservation(ctx, observation); err != nil {
		log.L().Warn().Err(err).Str("sessionId", sess.SessionID).Str("decisionRequestId", decisionRequestID).Msg("capability registry feedback observation failed")
	}
}

func (s *Server) logPlaybackFeedback(sessionID string, sess *model.SessionRecord, req PlaybackFeedbackRequest, decodeError bool) {
	var event *zerolog.Event
	switch req.Event {
	case PlaybackFeedbackRequestEventError:
		event = log.L().Warn()
	default:
		event = log.L().Info()
	}

	event.
		Str("sessionId", strings.TrimSpace(sessionID)).
		Str("event", string(req.Event)).
		Int("code", derefInt(req.Code)).
		Str("msg", derefString(req.Message)).
		Bool("decodeError", decodeError)

	appendPlaybackContextFields(event, req.Context)

	if sess == nil {
		event.Msg("playback feedback received")
		return
	}

	event.
		Str("state", string(sess.State)).
		Str("serviceRef", strings.TrimSpace(sess.ServiceRef)).
		Str("profile", strings.TrimSpace(sess.Profile.Name)).
		Str("profileContainer", strings.TrimSpace(sess.Profile.Container)).
		Str("profileVideoCodec", strings.TrimSpace(sess.Profile.VideoCodec)).
		Bool("firstFrameSeen", sessionHasFirstFrameArtifact(s.cfg.HLS.Root, sess.SessionID))

	if sess.ContextData != nil {
		if requestID := strings.TrimSpace(sess.ContextData[model.CtxKeyDecisionRequest]); requestID != "" {
			event.Str("decisionRequestId", requestID)
		}
		if requestedPath := strings.TrimSpace(sess.ContextData[model.CtxKeyClientPath]); requestedPath != "" {
			event.Str("contextClientPath", requestedPath)
		}
	}

	trace := sess.PlaybackTrace
	if trace == nil {
		event.Msg("playback feedback received")
		return
	}

	event.
		Str("requestProfile", strings.TrimSpace(trace.RequestProfile)).
		Str("requestedIntent", strings.TrimSpace(trace.RequestedIntent)).
		Str("resolvedIntent", strings.TrimSpace(trace.ResolvedIntent)).
		Str("clientPath", strings.TrimSpace(trace.ClientPath)).
		Str("inputKind", strings.TrimSpace(trace.InputKind)).
		Str("autoCodecPolicy", strings.TrimSpace(trace.AutoCodecPolicy)).
		Str("autoCodecRequested", strings.TrimSpace(trace.AutoCodecRequested)).
		Str("autoCodecSelected", strings.TrimSpace(trace.AutoCodecSelected)).
		Str("autoCodecPerformanceClass", strings.TrimSpace(trace.AutoCodecHostClass)).
		Str("autoCodecBenchmarkClass", strings.TrimSpace(trace.AutoCodecBenchClass)).
		Str("ffmpegPackaging", traceFFmpegPlanValue(trace, func(plan *model.FFmpegPlanTrace) string { return strings.TrimSpace(plan.Packaging) })).
		Str("ffmpegContainer", traceFFmpegPlanValue(trace, func(plan *model.FFmpegPlanTrace) string { return strings.TrimSpace(plan.Container) })).
		Str("ffmpegVideoMode", traceFFmpegPlanValue(trace, func(plan *model.FFmpegPlanTrace) string { return strings.TrimSpace(plan.VideoMode) })).
		Str("ffmpegVideoCodec", traceFFmpegPlanValue(trace, func(plan *model.FFmpegPlanTrace) string { return strings.TrimSpace(plan.VideoCodec) })).
		Str("ffmpegAudioMode", traceFFmpegPlanValue(trace, func(plan *model.FFmpegPlanTrace) string { return strings.TrimSpace(plan.AudioMode) })).
		Str("ffmpegAudioCodec", traceFFmpegPlanValue(trace, func(plan *model.FFmpegPlanTrace) string { return strings.TrimSpace(plan.AudioCodec) })).
		Str("clientFamily", traceClientValue(trace, func(client *model.PlaybackClientSnapshot) string { return strings.TrimSpace(client.ClientFamily) })).
		Str("preferredHlsEngine", traceClientValue(trace, func(client *model.PlaybackClientSnapshot) string { return strings.TrimSpace(client.PreferredHLSEngine) }))

	if trace.TargetProfile != nil {
		event.
			Str("targetContainer", strings.TrimSpace(trace.TargetProfile.Container)).
			Str("targetPackaging", strings.TrimSpace(string(trace.TargetProfile.Packaging))).
			Str("targetVideoCodec", strings.TrimSpace(trace.TargetProfile.Video.Codec)).
			Str("targetAudioCodec", strings.TrimSpace(trace.TargetProfile.Audio.Codec))
	}

	event.Msg("playback feedback received")
}

func appendPlaybackContextFields(event *zerolog.Event, ctx *PlaybackEngineErrorContext) {
	if ctx == nil {
		return
	}
	if ctx.Engine != nil {
		event.Str("playbackEngine", string(*ctx.Engine))
	}
	if ctx.Phase != nil {
		event.Str("playbackPhase", string(*ctx.Phase))
	}
	if ctx.AttemptId != nil {
		event.Int("playbackAttemptId", *ctx.AttemptId)
	}
	if ctx.PlaybackEpoch != nil {
		event.Int("playbackEpoch", *ctx.PlaybackEpoch)
	}
	if ctx.RecoveryAttempt != nil {
		event.Int("playbackRecoveryAttempt", *ctx.RecoveryAttempt)
	}
}

func traceFFmpegPlanValue(trace *model.PlaybackTrace, selector func(*model.FFmpegPlanTrace) string) string {
	if trace == nil || trace.FFmpegPlan == nil {
		return ""
	}
	return selector(trace.FFmpegPlan)
}

func traceClientValue(trace *model.PlaybackTrace, selector func(*model.PlaybackClientSnapshot) string) string {
	if trace == nil || trace.Client == nil {
		return ""
	}
	return selector(trace.Client)
}

func sessionFallbackPlanID(sess *model.SessionRecord) string {
	if sess == nil || sess.PlaybackTrace == nil || len(sess.PlaybackTrace.Fallbacks) == 0 {
		return ""
	}
	return strings.TrimSpace(sess.PlaybackTrace.Fallbacks[len(sess.PlaybackTrace.Fallbacks)-1].PlanID)
}

func sessionFallbackPlanReason(sess *model.SessionRecord) string {
	if sess == nil || sess.PlaybackTrace == nil || len(sess.PlaybackTrace.Fallbacks) == 0 {
		return ""
	}
	return strings.TrimSpace(sess.PlaybackTrace.Fallbacks[len(sess.PlaybackTrace.Fallbacks)-1].PlanReason)
}

const (
	playbackInfoCodeProbeWindowConfirmed = 221
	playbackInfoCodeNativeRenderStable   = 231
	playbackInfoCodeNativeRenderRecover  = 232
	playbackInfoCodeHLSJSRenderStable    = 241
	playbackInfoCodeHLSJSRenderBlack     = 242
)

func playbackFeedbackOutcome(event PlaybackFeedbackRequestEvent, code int) string {
	if code == playbackInfoCodeHLSJSRenderBlack {
		return "warning"
	}
	switch event {
	case PlaybackFeedbackRequestEventError:
		return "failed"
	case PlaybackFeedbackRequestEventWarning:
		return "warning"
	default:
		return "started"
	}
}

func feedbackSubjectKind(sess *model.SessionRecord) string {
	if sess == nil || sess.ContextData == nil {
		return "live"
	}
	switch strings.ToLower(strings.TrimSpace(sess.ContextData[model.CtxKeyMode])) {
	case "recording":
		return "recording"
	default:
		return "live"
	}
}

func feedbackTraceIntent(trace *model.PlaybackTrace, requested bool) string {
	if trace == nil {
		return ""
	}
	if requested {
		return strings.TrimSpace(trace.RequestedIntent)
	}
	return strings.TrimSpace(trace.ResolvedIntent)
}

func traceTargetProfileForFeedback(sess *model.SessionRecord) *playbackprofile.TargetPlaybackProfile {
	if sess == nil {
		return nil
	}
	if sess.PlaybackTrace != nil && sess.PlaybackTrace.TargetProfile != nil {
		return sess.PlaybackTrace.TargetProfile
	}
	return model.TraceTargetProfileFromProfile(sess.Profile)
}

func feedbackMode(existing string, target *playbackprofile.TargetPlaybackProfile) string {
	if existing = strings.TrimSpace(existing); existing != "" {
		return existing
	}
	if target == nil {
		return ""
	}
	if target.Video.Mode == playbackprofile.MediaModeCopy && target.Audio.Mode == playbackprofile.MediaModeCopy {
		if target.HLS.Enabled {
			return "direct_stream"
		}
		return "direct_play"
	}
	return "transcode"
}

func feedbackContainer(existing string, target *playbackprofile.TargetPlaybackProfile) string {
	if existing = strings.TrimSpace(existing); existing != "" {
		return existing
	}
	if target == nil {
		return ""
	}
	return strings.TrimSpace(target.Container)
}

func feedbackVideoCodec(existing string, target *playbackprofile.TargetPlaybackProfile) string {
	if existing = strings.TrimSpace(existing); existing != "" {
		return existing
	}
	if target == nil {
		return ""
	}
	return strings.TrimSpace(target.Video.Codec)
}

func feedbackAudioCodec(existing string, target *playbackprofile.TargetPlaybackProfile) string {
	if existing = strings.TrimSpace(existing); existing != "" {
		return existing
	}
	if target == nil {
		return ""
	}
	return strings.TrimSpace(target.Audio.Codec)
}

func firstNonEmptyFeedback(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func mapPlaybackTraceHLSDebug(trace *model.PlaybackTrace) *PlaybackTraceHlsDebug {
	if trace == nil || trace.HLS == nil {
		return nil
	}
	hls := trace.HLS
	dto := &PlaybackTraceHlsDebug{}
	hasValue := false

	if hls.PlaylistRequestCount > 0 {
		dto.PlaylistRequestCount = optionalIntPtr(hls.PlaylistRequestCount)
		hasValue = true
	}
	if hls.LastPlaylistAtUnix > 0 {
		dto.LastPlaylistAtMs = optionalIntPtr(int(hls.LastPlaylistAtUnix * 1000))
		hasValue = true
	}
	if hls.LastPlaylistIntervalMs > 0 {
		dto.LastPlaylistIntervalMs = optionalIntPtr(hls.LastPlaylistIntervalMs)
		hasValue = true
	}
	if hls.SegmentRequestCount > 0 {
		dto.SegmentRequestCount = optionalIntPtr(hls.SegmentRequestCount)
		hasValue = true
	}
	if hls.LastSegmentAtUnix > 0 {
		// LastSegmentAtUnix is stored as UnixMilli for sub-second precision.
		dto.LastSegmentAtMs = optionalIntPtr(int(hls.LastSegmentAtUnix))
		hasValue = true
	}
	if lastSegmentName := strings.TrimSpace(hls.LastSegmentName); lastSegmentName != "" {
		dto.LastSegmentName = &lastSegmentName
		hasValue = true
	}
	if hls.LastSegmentGapMs > 0 {
		dto.LastSegmentGapMs = optionalIntPtr(hls.LastSegmentGapMs)
		hasValue = true
	}
	if hls.LatestSegmentLagMs > 0 {
		dto.LatestSegmentLagMs = optionalIntPtr(hls.LatestSegmentLagMs)
		hasValue = true
	}
	if stallHint := strings.TrimSpace(hls.StallRisk); stallHint != "" {
		dto.StallHint = &stallHint
		hasValue = true
	}
	if startupMode := strings.TrimSpace(hls.StartupMode); startupMode != "" {
		dto.StartupMode = &startupMode
		hasValue = true
	}
	if hls.StartupHeadroomSec > 0 {
		dto.StartupHeadroomSec = optionalIntPtr(hls.StartupHeadroomSec)
		hasValue = true
	}
	if len(hls.StartupReasons) > 0 {
		reasons := append([]string(nil), hls.StartupReasons...)
		dto.StartupReasons = &reasons
		hasValue = true
	}
	if hasValue {
		assessment := pipelineapi.DeriveSessionPlaybackHealth(trace, pipelineapi.SessionPlaybackHealthContext{})
		if health := strings.TrimSpace(string(assessment.Health)); health != "" {
			dto.Health = &health
		}
		if len(assessment.ReasonCodes) > 0 {
			reasons := append([]string(nil), assessment.ReasonCodes...)
			dto.HealthReasons = &reasons
		}
	}

	if !hasValue {
		return nil
	}
	return dto
}

func mapSessionPlaybackTrace(requestID string, session *model.SessionRecord, hlsRoot string) *PlaybackTrace {
	if session == nil {
		return nil
	}
	dto := &PlaybackTrace{
		RequestId: requestID,
	}
	if sid := strings.TrimSpace(session.SessionID); sid != "" {
		dto.SessionId = &sid
	}

	trace := session.PlaybackTrace
	if trace == nil {
		trace = &model.PlaybackTrace{}
	}
	runtimeState := loadSessionRuntimePolicyState(session)
	runtimeTimeline := loadSessionRuntimeTimeline(session)
	runtimeReplay := loadSessionRuntimeReplay(session)
	if runtimeReplay == nil {
		runtimeReplay = buildSessionRuntimePolicyReplay(session)
	}

	requestProfile := strings.TrimSpace(trace.RequestProfile)
	if requestProfile == "" {
		requestProfile = profiles.PublicProfileName(session.Profile.Name)
	}
	if requestProfile != "" {
		dto.RequestProfile = &requestProfile
	}

	requestedIntent := strings.TrimSpace(trace.RequestedIntent)
	if requestedIntent == "" {
		requestedIntent = requestProfile
	}
	if requestedIntent != "" {
		dto.RequestedIntent = &requestedIntent
	}

	resolvedIntent := strings.TrimSpace(trace.ResolvedIntent)
	if resolvedIntent == "" {
		resolvedIntent = profiles.PublicProfileName(session.Profile.Name)
	}
	if resolvedIntent != "" {
		dto.ResolvedIntent = &resolvedIntent
	}

	policyModeHint := strings.TrimSpace(string(trace.PolicyModeHint))
	if policyModeHint != "" {
		dto.PolicyModeHint = &policyModeHint
	}
	effectiveRuntimeMode := strings.TrimSpace(string(trace.EffectiveRuntimeMode))
	if effectiveRuntimeMode != "" {
		dto.EffectiveRuntimeMode = &effectiveRuntimeMode
	}
	effectiveModeSource := strings.TrimSpace(string(trace.EffectiveModeSource))
	if effectiveModeSource != "" {
		dto.EffectiveModeSource = &effectiveModeSource
	}

	qualityRung := strings.TrimSpace(trace.QualityRung)
	if qualityRung != "" {
		dto.QualityRung = &qualityRung
	}
	audioQualityRung := strings.TrimSpace(trace.AudioQualityRung)
	if audioQualityRung != "" {
		dto.AudioQualityRung = &audioQualityRung
	}
	videoQualityRung := strings.TrimSpace(trace.VideoQualityRung)
	if videoQualityRung != "" {
		dto.VideoQualityRung = &videoQualityRung
	}

	degradedFrom := strings.TrimSpace(trace.DegradedFrom)
	if degradedFrom != "" {
		dto.DegradedFrom = &degradedFrom
	}
	hostPressureBand := strings.TrimSpace(trace.HostPressureBand)
	if hostPressureBand != "" {
		dto.HostPressureBand = &hostPressureBand
	}
	if autoCodecPolicy := strings.TrimSpace(trace.AutoCodecPolicy); autoCodecPolicy != "" {
		dto.AutoCodecPolicy = &autoCodecPolicy
	}
	if autoCodecRequested := strings.TrimSpace(trace.AutoCodecRequested); autoCodecRequested != "" {
		dto.AutoCodecRequestedCodecs = &autoCodecRequested
	}
	if autoCodecSelected := strings.TrimSpace(trace.AutoCodecSelected); autoCodecSelected != "" {
		dto.AutoCodecSelectedCodec = &autoCodecSelected
	}
	if autoCodecHostClass := strings.TrimSpace(trace.AutoCodecHostClass); autoCodecHostClass != "" {
		dto.AutoCodecPerformanceClass = &autoCodecHostClass
	}
	if autoCodecBenchClass := strings.TrimSpace(trace.AutoCodecBenchClass); autoCodecBenchClass != "" {
		dto.AutoCodecBenchmarkClass = &autoCodecBenchClass
	}
	if trace.HostOverrideApplied {
		hostOverrideApplied := true
		dto.HostOverrideApplied = &hostOverrideApplied
	}
	clientSnapshot := sessionPlaybackClientSnapshot(session)
	if clientSnapshot != nil {
		dto.Client = mapPlaybackClientSnapshot(clientSnapshot)
		if clientFamily := strings.TrimSpace(clientSnapshot.ClientFamily); clientFamily != "" {
			dto.ClientFamily = &clientFamily
		}
		if clientCapsSource := strings.TrimSpace(clientSnapshot.ClientCapsSource); clientCapsSource != "" {
			dto.ClientCapsSource = &clientCapsSource
		}
	}

	if trace.Source != nil {
		dto.Source = mapSourceProfile(trace.Source)
	}

	clientPath := strings.TrimSpace(trace.ClientPath)
	if clientPath == "" && session.ContextData != nil {
		clientPath = strings.TrimSpace(session.ContextData[model.CtxKeyClientPath])
	}
	if clientPath != "" {
		dto.ClientPath = &clientPath
	}

	inputKind := strings.TrimSpace(trace.InputKind)
	if inputKind == "" && session.ContextData != nil {
		inputKind = strings.TrimSpace(session.ContextData[model.CtxKeySourceType])
	}
	if inputKind != "" {
		dto.InputKind = &inputKind
	}
	preflightReason := strings.TrimSpace(trace.PreflightReason)
	if preflightReason != "" {
		dto.PreflightReason = &preflightReason
	}
	preflightDetail := strings.TrimSpace(trace.PreflightDetail)
	if preflightDetail != "" {
		dto.PreflightDetail = &preflightDetail
	}

	target := trace.TargetProfile
	if target != nil {
		dto.TargetProfile = mapTargetProfile(target)
		hash := strings.TrimSpace(trace.TargetProfileHash)
		if hash != "" {
			dto.TargetProfileHash = &hash
		}
	}

	operator := PlaybackTraceOperator{}
	hasOperator := false
	if trace.Operator != nil {
		operator = PlaybackTraceOperator{
			ClientFallbackDisabled: boolPtr(trace.Operator.ClientFallbackDisabled),
			OverrideApplied:        boolPtr(trace.Operator.OverrideApplied),
		}
		hasOperator = trace.Operator.ClientFallbackDisabled || trace.Operator.OverrideApplied
		if forcedIntent := strings.TrimSpace(trace.Operator.ForcedIntent); forcedIntent != "" {
			operator.ForcedIntent = &forcedIntent
			hasOperator = true
		}
		if maxQualityRung := strings.TrimSpace(trace.Operator.MaxQualityRung); maxQualityRung != "" {
			operator.MaxQualityRung = &maxQualityRung
			hasOperator = true
		}
		if ruleName := strings.TrimSpace(trace.Operator.RuleName); ruleName != "" {
			operator.RuleName = &ruleName
			hasOperator = true
		}
		if ruleScope := strings.TrimSpace(trace.Operator.RuleScope); ruleScope != "" {
			operator.RuleScope = &ruleScope
			hasOperator = true
		}
	}
	if runtimeAction := strings.TrimSpace(string(runtimeState.LastAction)); runtimeAction != "" {
		operator.RuntimePolicyAction = &runtimeAction
		hasOperator = true
	}
	if runtimePhase := strings.TrimSpace(sessionRuntimePolicyPhaseName(runtimeState, time.Now().UTC())); runtimePhase != "" {
		operator.RuntimePolicyPhase = &runtimePhase
		hasOperator = true
	}
	if len(runtimeState.PolicyConstraints) > 0 {
		constraints := append([]string(nil), runtimeState.PolicyConstraints...)
		operator.RuntimePolicyConstraints = &constraints
		hasOperator = true
	}
	if len(runtimeState.Reasons) > 0 {
		reasons := append([]string(nil), runtimeState.Reasons...)
		operator.RuntimePolicyReasons = &reasons
		hasOperator = true
	}
	if mappedReplay := mapRuntimePolicyReplay(runtimeReplay); mappedReplay != nil {
		operator.RuntimePolicyReplay = mappedReplay
		hasOperator = true
	}
	if runtimeCandidate := strings.TrimSpace(string(runtimeState.ProbeStep)); runtimeCandidate != "" {
		operator.RuntimeProbeCandidate = &runtimeCandidate
		hasOperator = true
	}
	if mappedTimeline := mapRuntimePolicyTimeline(runtimeTimeline); mappedTimeline != nil {
		operator.RuntimePolicyTimeline = mappedTimeline
		hasOperator = true
	}
	if hasOperator {
		dto.Operator = &operator
	}

	if trace.FFmpegPlan != nil {
		dto.FfmpegPlan = &PlaybackTraceFfmpegPlan{
			InputKind:  strPtr(trace.FFmpegPlan.InputKind),
			Container:  strPtr(trace.FFmpegPlan.Container),
			Packaging:  strPtr(trace.FFmpegPlan.Packaging),
			HwAccel:    strPtr(trace.FFmpegPlan.HWAccel),
			VideoMode:  strPtr(trace.FFmpegPlan.VideoMode),
			VideoCodec: strPtr(trace.FFmpegPlan.VideoCodec),
			AudioMode:  strPtr(trace.FFmpegPlan.AudioMode),
			AudioCodec: strPtr(trace.FFmpegPlan.AudioCodec),
		}
	}
	if trace.RuntimeDiagnostics != nil && !trace.RuntimeDiagnostics.IsZero() {
		dto.RuntimeDiagnostics = mapPlaybackRuntimeDiagnostics(*trace.RuntimeDiagnostics)
	}

	if hlsDebug := mapPlaybackTraceHLSDebug(trace); hlsDebug != nil {
		dto.HlsDebug = hlsDebug
	}

	firstFrameAtUnix := trace.FirstFrameAtUnix
	if firstFrameAtUnix == 0 {
		firstFrameAtUnix = sessionFirstFrameUnix(hlsRoot, session.SessionID)
	}
	if firstFrameAtUnix > 0 {
		firstFrameAtMs := int(firstFrameAtUnix * 1000)
		dto.FirstFrameAtMs = &firstFrameAtMs
	}

	fallbackCount := len(trace.Fallbacks)
	lastFallbackReason := ""
	if fallbackCount > 0 {
		lastFallbackReason = strings.TrimSpace(trace.Fallbacks[fallbackCount-1].Reason)
	} else if strings.TrimSpace(session.FallbackReason) != "" {
		fallbackCount = 1
		lastFallbackReason = strings.TrimSpace(session.FallbackReason)
	}
	if fallbackCount > 0 {
		dto.FallbackCount = &fallbackCount
	}
	if lastFallbackReason != "" {
		dto.LastFallbackReason = &lastFallbackReason
	}

	stopReason := strings.TrimSpace(trace.StopReason)
	if stopReason == "" && session.Reason != "" && session.State.IsTerminal() {
		stopReason = string(session.Reason)
	}
	if stopReason != "" {
		dto.StopReason = &stopReason
	}

	stopClass := strings.TrimSpace(string(trace.StopClass))
	if stopClass == "" && session.State.IsTerminal() && session.Reason != "" {
		stopClass = strings.TrimSpace(string(model.TraceStopClassFromReason(session.Reason)))
	}
	if stopClass != "" {
		dto.StopClass = &stopClass
	}

	return dto
}

func mapPlaybackRuntimeDiagnostics(d ports.RuntimeDiagnostics) *PlaybackTraceRuntimeDiagnostics {
	out := &PlaybackTraceRuntimeDiagnostics{}
	if d.FrameCount > 0 {
		out.FrameCount = &d.FrameCount
	}
	if d.FPS > 0 {
		value := float32(d.FPS)
		out.Fps = &value
	}
	out.DropFrames = &d.DropFrames
	out.DupFrames = &d.DupFrames
	if d.Speed > 0 {
		value := float32(d.Speed)
		out.Speed = &value
	}
	if d.CorruptDecodedFrames > 0 {
		out.CorruptDecodedFrames = &d.CorruptDecodedFrames
	}
	if warning := strings.TrimSpace(d.LastWarning); warning != "" {
		out.LastWarning = &warning
	}
	if d.UpdatedAtUnix > 0 {
		updatedAtUnix := int(d.UpdatedAtUnix)
		out.UpdatedAtUnix = &updatedAtUnix
	}
	return out
}

func sessionRuntimePolicyPhaseName(state runtimepolicy.SessionLoopState, now time.Time) string {
	if state.ProbeState == runtimepolicy.ProbeLifecycleScheduled || state.ProbeState == runtimepolicy.ProbeLifecycleObserving {
		return "probing"
	}
	switch strings.TrimSpace(string(state.LastAction)) {
	case "probe_up":
		return "probing"
	case "cooldown":
		return "cooldown"
	case "degrade", "step_down":
		return "degraded"
	case "lock_current":
		return "recovering"
	}
	if hasString(state.Reasons, runtimepolicy.ReasonProbeRecentlyRegressed, runtimepolicy.ReasonProbeWindowRegressed) || state.ProbeState == runtimepolicy.ProbeLifecycleAborted {
		return "probe_regressed"
	}
	if !state.CooldownUntil.IsZero() && state.CooldownUntil.After(now) {
		return "cooldown"
	}
	switch state.ConfidenceState {
	case runtimepolicy.ConfidenceRecovery:
		return "recovering"
	case runtimepolicy.ConfidenceLow:
		return "degraded"
	case runtimepolicy.ConfidenceStable, runtimepolicy.ConfidenceHigh:
		return "stable"
	default:
		return ""
	}
}

func mapRuntimePolicyTimeline(timeline []runtimepolicy.TickTrace) *[]PlaybackTraceRuntimeTick {
	if len(timeline) == 0 {
		return nil
	}
	out := make([]PlaybackTraceRuntimeTick, 0, len(timeline))
	for _, tick := range timeline {
		dto := PlaybackTraceRuntimeTick{
			TickAt:             tick.TickAt,
			ConfidenceScore:    toIntPtr(tick.ConfidenceScore),
			ConfidenceState:    strPtr(string(tick.ConfidenceState)),
			PolicyAction:       strPtr(string(tick.PolicyAction)),
			PlannedTransition:  strPtr(string(tick.PlannedTransition)),
			ExecutedTransition: strPtr(string(tick.ExecutedTransition)),
			ActiveStep:         strPtr(string(tick.ActiveStep)),
			TargetStep:         strPtr(string(tick.TargetStep)),
			ProbeStep:          strPtr(string(tick.ProbeStep)),
			ProbeState:         strPtr(string(tick.ProbeState)),
		}
		if !tick.CooldownUntil.IsZero() {
			cooldown := tick.CooldownUntil
			dto.CooldownUntil = &cooldown
		}
		if len(tick.Blockers) > 0 {
			blockers := append([]string(nil), tick.Blockers...)
			dto.Blockers = &blockers
		}
		if len(tick.Reasons) > 0 {
			reasons := append([]string(nil), tick.Reasons...)
			dto.Reasons = &reasons
		}
		out = append(out, dto)
	}
	return &out
}

func mapRuntimePolicyReplay(replay *runtimepolicy.RuntimePolicyReplay) *PlaybackTraceRuntimeReplay {
	if replay == nil {
		return nil
	}
	out := &PlaybackTraceRuntimeReplay{
		Metadata:     mapRuntimePolicyReplayMetadata(replay.Metadata),
		InitialState: mapRuntimePolicyReplayState(replay.InitialState),
		FinalState:   mapRuntimePolicyReplayState(replay.FinalState),
	}
	if len(replay.Ticks) > 0 {
		ticks := make([]PlaybackTraceRuntimeReplayTick, 0, len(replay.Ticks))
		for _, tick := range replay.Ticks {
			ticks = append(ticks, PlaybackTraceRuntimeReplayTick{
				Input:    mapRuntimePolicyReplayTickInput(tick.Input),
				Expected: mapRuntimePolicyReplayTickExpected(tick.Expected),
			})
		}
		out.Ticks = &ticks
	}
	return out
}

func mapRuntimePolicyReplayMetadata(metadata runtimepolicy.ReplayMetadata) *PlaybackTraceRuntimeReplayMetadata {
	if metadata.SessionID == "" && metadata.ServiceRef == "" && metadata.ClientPath == "" && metadata.SourceType == "" && metadata.InitialTarget == runtimepolicy.PlaybackStepUnknown {
		return nil
	}
	return &PlaybackTraceRuntimeReplayMetadata{
		SessionId:     strPtr(metadata.SessionID),
		ServiceRef:    strPtr(metadata.ServiceRef),
		ClientPath:    strPtr(metadata.ClientPath),
		SourceType:    strPtr(metadata.SourceType),
		InitialTarget: strPtr(string(metadata.InitialTarget)),
	}
}

func mapRuntimePolicyReplayState(state runtimepolicy.SessionLoopState) *PlaybackTraceRuntimeReplayState {
	if state.CurrentStep == runtimepolicy.PlaybackStepUnknown &&
		state.TargetStep == runtimepolicy.PlaybackStepUnknown &&
		state.ProbeStep == runtimepolicy.PlaybackStepUnknown &&
		state.ProbeState == runtimepolicy.ProbeLifecycleNone &&
		state.ConfidenceScore == 0 &&
		state.ConfidenceState == "" &&
		state.CooldownUntil.IsZero() &&
		state.LastAction == "" &&
		len(state.PolicyConstraints) == 0 &&
		len(state.Reasons) == 0 {
		return nil
	}
	out := &PlaybackTraceRuntimeReplayState{
		ConfidenceScore: toIntPtr(state.ConfidenceScore),
		ConfidenceState: strPtr(string(state.ConfidenceState)),
		CurrentStep:     strPtr(string(state.CurrentStep)),
		LastAction:      strPtr(string(state.LastAction)),
		TargetStep:      strPtr(string(state.TargetStep)),
		ProbeStep:       strPtr(string(state.ProbeStep)),
		ProbeState:      strPtr(string(state.ProbeState)),
	}
	if !state.CooldownUntil.IsZero() {
		cooldown := state.CooldownUntil
		out.CooldownUntil = &cooldown
	}
	if len(state.PolicyConstraints) > 0 {
		constraints := append([]string(nil), state.PolicyConstraints...)
		out.PolicyConstraints = &constraints
	}
	if len(state.Reasons) > 0 {
		reasons := append([]string(nil), state.Reasons...)
		out.Reasons = &reasons
	}
	return out
}

func mapRuntimePolicyReplayTickInput(input runtimepolicy.ReplayTickInput) *PlaybackTraceRuntimeReplayTickInput {
	out := &PlaybackTraceRuntimeReplayTickInput{
		ObservedStep: strPtr(string(input.ObservedStep)),
		TargetStep:   strPtr(string(input.TargetStep)),
		TickAt:       input.TickAt,
	}
	out.Confidence = mapRuntimePolicyReplayTickInputConfidence(input.Confidence)
	return out
}

func mapRuntimePolicyReplayTickInputConfidence(snapshot runtimepolicy.ConfidenceSnapshot) *PlaybackTraceRuntimeReplayTickInputConfidence {
	out := &PlaybackTraceRuntimeReplayTickInputConfidence{
		Score:       toIntPtr(snapshot.Score),
		State:       strPtr(string(snapshot.State)),
		WindowCount: toIntPtr(snapshot.WindowCount),
	}
	if !snapshot.StateSince.IsZero() {
		stateSince := snapshot.StateSince
		out.StateSince = &stateSince
	}
	if !snapshot.CooldownUntil.IsZero() {
		cooldown := snapshot.CooldownUntil
		out.CooldownUntil = &cooldown
	}
	if len(snapshot.PolicyConstraints) > 0 {
		constraints := append([]string(nil), snapshot.PolicyConstraints...)
		out.PolicyConstraints = &constraints
	}
	if len(snapshot.Reasons) > 0 {
		reasons := append([]string(nil), snapshot.Reasons...)
		out.Reasons = &reasons
	}
	return out
}

func mapRuntimePolicyReplayTickExpected(expected runtimepolicy.ReplayTickExpectation) *PlaybackTraceRuntimeReplayTickExpected {
	out := &PlaybackTraceRuntimeReplayTickExpected{
		Action:             strPtr(string(expected.Action)),
		ActiveStep:         strPtr(string(expected.ActiveStep)),
		PlannedTransition:  strPtr(string(expected.PlannedTransition)),
		ExecutedTransition: strPtr(string(expected.ExecutedTransition)),
		ProbeStep:          strPtr(string(expected.ProbeStep)),
		ProbeState:         strPtr(string(expected.ProbeState)),
		RuntimePhase:       strPtr(expected.RuntimePhase),
	}
	if len(expected.Blockers) > 0 {
		blockers := append([]string(nil), expected.Blockers...)
		out.Blockers = &blockers
	}
	if len(expected.Reasons) > 0 {
		reasons := append([]string(nil), expected.Reasons...)
		out.Reasons = &reasons
	}
	return out
}

func toIntPtr(i int) *int { return &i }

func hasString(values []string, targets ...string) bool {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		for _, target := range targets {
			if value == strings.TrimSpace(target) {
				return true
			}
		}
	}
	return false
}

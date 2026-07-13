package v3

import (
	"context"
	"encoding/json"
	"fmt"
	v3sessions "github.com/ManuGH/xg2g/internal/control/http/v3/sessions"
	"github.com/ManuGH/xg2g/internal/control/recordings/capreg"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"github.com/go-chi/chi/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/rs/zerolog"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"time"
)

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
		// A store error is transient, not a definitive "not found". Returning 404 told the
		// client to STOP fallback recovery for a decode error; return 503 so it retries.
		RespondError(w, r, http.StatusServiceUnavailable, ErrServiceUnavailable, "session lookup failed")
		return
	}
	if sess == nil {
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
	profileResolver := s.profileResolver
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

		fallbackPlan := nextPlaybackFeedbackPlanWithResolver(s.Profile, s.ServiceRef, profileResolver)
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

func shouldPreferSafariTSFallbackForServiceRefWithSnapshot(serviceRef string, configuredRefs []string) bool {
	targetRef := normalizeServiceRefForEnv(serviceRef)
	if targetRef == "" {
		return false
	}
	for _, candidate := range configuredRefs {
		if normalizeServiceRefForEnv(candidate) == targetRef {
			return true
		}
	}
	return false
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

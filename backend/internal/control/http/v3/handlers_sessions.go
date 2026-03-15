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
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ManuGH/xg2g/internal/domain/session/lifecycle"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

// Responsibility: Handles Session status, debugging, and client feedback/reporting.
// Non-goals: Initializing streams (see intents).

const (
	fallbackRestartPollInterval = 50 * time.Millisecond
	fallbackRestartTimeout      = 5 * time.Second
)

// handleV3SessionsDebug dumps all sessions from the store (Admin only).
// Authorization: Requires v3:admin scope (enforced by route middleware).
// Supports pagination via query parameters: offset (default 0) and limit (default 100, max 1000).
func (s *Server) handleV3SessionsDebug(w http.ResponseWriter, r *http.Request) {
	deps := s.sessionsModuleDeps()
	store := deps.store

	if store == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &APIError{
			Code:    "V3_UNAVAILABLE",
			Message: "v3 store not initialized",
		})
		return
	}

	// 2. Parse Pagination Parameters
	offset, limit := parsePaginationParams(r)

	// 3. List Sessions
	allSessions, err := store.ListSessions(r.Context())
	if err != nil {
		RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, err.Error())
		return
	}

	// 4. Apply Pagination
	total := len(allSessions)
	start := offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	sessions := allSessions[start:end]

	// 5. Build Response with Metadata
	response := map[string]any{
		"sessions": sessions,
		"pagination": map[string]int{
			"offset": offset,
			"limit":  limit,
			"total":  total,
			"count":  len(sessions),
		},
	}

	writeJSON(w, http.StatusOK, response)
}

// handleV3SessionState returns a single session state.
// Authorization: Requires v3:read scope (enforced by route middleware).
func (s *Server) handleV3SessionState(w http.ResponseWriter, r *http.Request) {
	deps := s.sessionsModuleDeps()
	store := deps.store

	if store == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &APIError{
			Code:    "V3_UNAVAILABLE",
			Message: "v3 store not initialized",
		})
		return
	}

	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" || !model.IsSafeSessionID(sessionID) {
		RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, "invalid session id")
		return
	}

	session, err := store.GetSession(r.Context(), sessionID)
	if err != nil || session == nil {
		RespondError(w, r, http.StatusNotFound, &APIError{
			Code:    "SESSION_NOT_FOUND",
			Message: "session not found",
		})
		return
	}

	// CTO Contract (Phase 5.3): Terminal sessions return 410 Gone with JSON body
	if session.State.IsTerminal() {
		trace := mapSessionPlaybackTrace(requestID(r.Context()), session, deps.cfg.HLS.Root)
		out := lifecycle.PublicOutcomeFromRecord(session)
		state := string(mapSessionState(out.State))
		reason, ok := mapSessionReason(out.Reason)
		extra := map[string]any{
			"session":       session.SessionID,
			"state":         state,
			"reason_detail": mapDetailCode(out.DetailCode),
		}
		if ok {
			extra["reason"] = string(reason)
		}
		if trace != nil {
			extra["trace"] = trace
		}
		writeProblem(w, r, http.StatusGone,
			"urn:xg2g:error:session:gone",
			"Session Gone",
			"session_gone",
			"Session is in a terminal state (stopped, failed, or cancelled).",
			extra,
		)
		return
	}

	resp := SessionResponse{
		SessionId:     openapi_types.UUID(parseUUID(session.SessionID)),
		ServiceRef:    &session.ServiceRef,
		Profile:       &session.Profile.Name,
		UpdatedAtMs:   toPtr(int(session.UpdatedAtUnix * 1000)),
		RequestId:     requestID(r.Context()),
		CorrelationId: &session.CorrelationID,
		Trace:         mapSessionPlaybackTrace(requestID(r.Context()), session, deps.cfg.HLS.Root),
	}

	ensureTraceHeader(w, r.Context())

	// Map State/Reason/Detail
	out := lifecycle.PublicOutcomeFromRecord(session)
	resp.State = mapSessionState(out.State)
	if r, ok := mapSessionReason(out.Reason); ok {
		resp.Reason = &r
	}
	detail := mapDetailCode(out.DetailCode)
	resp.ReasonDetail = &detail

	mode, durationSeconds, seekableStart, seekableEnd, liveEdge := sessionPlaybackInfo(session, time.Now())
	if mode != "" {
		m := SessionResponseMode(mode)
		resp.Mode = &m
	}
	resp.DurationSeconds = toFloat32Ptr(durationSeconds)
	resp.SeekableStartSeconds = toFloat32Ptr(seekableStart)
	resp.SeekableEndSeconds = toFloat32Ptr(seekableEnd)
	resp.LiveEdgeSeconds = toFloat32Ptr(liveEdge)
	pUrl := fmt.Sprintf("%s/sessions/%s/hls/index.m3u8", V3BaseURL, session.SessionID)
	resp.PlaybackUrl = &pUrl

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func toPtr[T any](v T) *T {
	return &v
}

func toFloat32Ptr(v *float64) *float32 {
	if v == nil {
		return nil
	}
	f32 := float32(*v)
	return &f32
}

func parseUUID(s string) uuid.UUID {
	u, _ := uuid.Parse(s)
	return u
}

// ReportPlaybackFeedback handles POST /sessions/{sessionId}/feedback
func (s *Server) ReportPlaybackFeedback(w http.ResponseWriter, r *http.Request, sessionId openapi_types.UUID) {
	deps := s.sessionsModuleDeps()
	bus := deps.bus
	store := deps.store

	if bus == nil || store == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &APIError{
			Code:    "V3_UNAVAILABLE",
			Message: "v3 not available",
		})
		return
	}

	// 2. Decode Feedback
	var req PlaybackFeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, "invalid body")
		return
	}

	// 3. Check for MediaError 3 (Safari HLS decode error)
	// We only trigger fallback if it's an error and specifically code 3
	isDecodeError := req.Event == PlaybackFeedbackRequestEventError && req.Code != nil && *req.Code == 3

	if !isDecodeError {
		// Just log info/warnings
		log.L().Info().
			Str("sessionId", sessionId.String()).
			Str("event", string(req.Event)).
			Int("code", derefInt(req.Code)).
			Str("msg", derefString(req.Message)).
			Msg("playback feedback received")
		w.WriteHeader(http.StatusAccepted)
		return
	}

	// 4. Trigger Fallback
	ctx := r.Context()
	sess, err := store.GetSession(ctx, sessionId.String())
	if err != nil || sess == nil {
		RespondError(w, r, http.StatusNotFound, &APIError{Code: "NOT_FOUND", Message: "session not found"})
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

		switch s.Profile.Name {
		case profiles.ProfileSafari:
			// First recovery step for Safari is the stricter dirty-stream profile.
			s.Profile = profiles.Resolve(profiles.ProfileSafariDirty, "", s.Profile.DVRWindowSec, nil, false, profiles.HWAccelOff)
		default:
			// Escalate all other failures, including safari_dirty re-failures, to repair.
			s.Profile.Name = profiles.ProfileRepair
			s.Profile.TranscodeVideo = true
			s.Profile.Deinterlace = false // Keep simple
			s.Profile.HWAccel = ""        // Force CPU
			s.Profile.VideoCodec = "libx264"
			s.Profile.VideoCRF = 24
			s.Profile.VideoMaxWidth = 1280
			s.Profile.Preset = "veryfast"
			s.Profile.Container = "fmp4" // Ensure FMP4 for Safari
		}

		s.FallbackReason = fmt.Sprintf("client_report:code=%d", derefInt(req.Code))
		s.FallbackAtUnix = now.Unix()
		toTarget := model.TraceTargetProfileFromProfile(s.Profile)
		toHash := ""
		if toTarget != nil {
			toHash = toTarget.Hash()
		}
		trace.TargetProfile = toTarget
		trace.TargetProfileHash = toHash
		trace.StopReason = ""
		trace.StopClass = model.PlaybackStopClassNone
		trace.Fallbacks = append(trace.Fallbacks, model.PlaybackFallbackTrace{
			AtUnix:          now.Unix(),
			Trigger:         "client_feedback",
			Reason:          s.FallbackReason,
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
	log.L().Warn().Str("sessionId", sess.SessionID).Msg("activating safari fallback (fmp4) due to client error")

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

func (s *Server) scheduleFallbackRestart(eventBus bus.Bus, store SessionStateStore, sess *model.SessionRecord) {
	if eventBus == nil || store == nil || sess == nil {
		return
	}

	sessionID := sess.SessionID
	serviceRef := sess.ServiceRef
	correlationID := sess.CorrelationID
	restartCtx := s.runtimeContextOrBackground()

	go func() {
		ctx, cancel := context.WithTimeout(restartCtx, fallbackRestartTimeout)
		defer cancel()

		if err := waitForTerminalSession(ctx, store, sessionID); err != nil {
			log.L().Error().Err(err).Str("sessionId", sessionID).Msg("failed to observe terminal state before fallback restart")
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
			log.L().Error().Err(err).Str("sessionId", sessionID).Msg("failed to publish restart event during fallback")
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
	if trace.HostOverrideApplied {
		hostOverrideApplied := true
		dto.HostOverrideApplied = &hostOverrideApplied
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

	if trace.Operator != nil {
		operator := PlaybackTraceOperator{
			ClientFallbackDisabled: trace.Operator.ClientFallbackDisabled,
			OverrideApplied:        trace.Operator.OverrideApplied,
		}
		if forcedIntent := strings.TrimSpace(trace.Operator.ForcedIntent); forcedIntent != "" {
			operator.ForcedIntent = &forcedIntent
		}
		if maxQualityRung := strings.TrimSpace(trace.Operator.MaxQualityRung); maxQualityRung != "" {
			operator.MaxQualityRung = &maxQualityRung
		}
		if ruleName := strings.TrimSpace(trace.Operator.RuleName); ruleName != "" {
			operator.RuleName = &ruleName
		}
		if ruleScope := strings.TrimSpace(trace.Operator.RuleScope); ruleScope != "" {
			operator.RuleScope = &ruleScope
		}
		dto.Operator = &operator
	}

	if trace.FFmpegPlan != nil {
		dto.FfmpegPlan = &PlaybackTraceFfmpegPlan{
			InputKind:  trace.FFmpegPlan.InputKind,
			Container:  trace.FFmpegPlan.Container,
			Packaging:  trace.FFmpegPlan.Packaging,
			HwAccel:    trace.FFmpegPlan.HWAccel,
			VideoMode:  trace.FFmpegPlan.VideoMode,
			VideoCodec: trace.FFmpegPlan.VideoCodec,
			AudioMode:  trace.FFmpegPlan.AudioMode,
			AudioCodec: trace.FFmpegPlan.AudioCodec,
		}
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

func sessionPlaybackInfo(session *model.SessionRecord, now time.Time) (string, *float64, *float64, *float64, *float64) {
	mode := model.ModeLive
	if session.ContextData != nil {
		if raw := strings.TrimSpace(session.ContextData[model.CtxKeyMode]); raw != "" {
			mode = strings.ToUpper(raw)
		}
	}
	if mode != model.ModeLive && mode != model.ModeRecording {
		mode = model.ModeLive
	}

	if mode == model.ModeRecording {
		durationSeconds := parseContextSeconds(session.ContextData, model.CtxKeyDurationSeconds)
		if durationSeconds == nil {
			return mode, nil, nil, nil, nil
		}
		zero := 0.0
		return mode, durationSeconds, &zero, durationSeconds, nil
	}

	var durationSeconds *float64
	if session.Profile.DVRWindowSec > 0 {
		val := float64(session.Profile.DVRWindowSec)
		durationSeconds = &val
	}

	nowUnix := session.LastAccessUnix
	if nowUnix == 0 {
		nowUnix = session.UpdatedAtUnix
	}
	if nowUnix == 0 {
		nowUnix = now.Unix()
	}

	startUnix := session.CreatedAtUnix
	if startUnix == 0 {
		startUnix = session.UpdatedAtUnix
	}
	if startUnix == 0 {
		startUnix = nowUnix
	}

	liveEdgeVal := float64(nowUnix - startUnix)
	if liveEdgeVal < 0 {
		liveEdgeVal = 0
	}

	seekableStart := liveEdgeVal
	if durationSeconds != nil && *durationSeconds > 0 {
		seekableStart = liveEdgeVal - *durationSeconds
		if seekableStart < 0 {
			seekableStart = 0
		}
	}

	return mode, durationSeconds, &seekableStart, &liveEdgeVal, &liveEdgeVal
}

func parseContextSeconds(ctx map[string]string, key string) *float64 {
	if ctx == nil {
		return nil
	}
	raw := strings.TrimSpace(ctx[key])
	if raw == "" {
		return nil
	}
	val, err := strconv.ParseFloat(raw, 64)
	if err != nil || val <= 0 {
		return nil
	}
	return &val
}

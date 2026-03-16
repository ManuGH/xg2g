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
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	v3sessions "github.com/ManuGH/xg2g/internal/control/http/v3/sessions"
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
			ClientFallbackDisabled: boolPtr(trace.Operator.ClientFallbackDisabled),
			OverrideApplied:        boolPtr(trace.Operator.OverrideApplied),
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

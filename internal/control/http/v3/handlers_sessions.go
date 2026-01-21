// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

// Responsibility: Handles Session status, debugging, and client feedback/reporting.
// Non-goals: Initializing streams (see intents).

// handleV3SessionsDebug dumps all sessions from the store (Admin only).
// Authorization: Requires v3:admin scope (enforced by route middleware).
// Supports pagination via query parameters: offset (default 0) and limit (default 100, max 1000).
func (s *Server) handleV3SessionsDebug(w http.ResponseWriter, r *http.Request) {
	// 1. Access Store
	s.mu.RLock()
	store := s.v3Store
	s.mu.RUnlock()

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
	s.mu.RLock()
	store := s.v3Store
	s.mu.RUnlock()

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
		writeProblem(w, r, http.StatusGone,
			"urn:xg2g:error:session:gone",
			"Session Gone",
			"session_gone",
			"Session is in a terminal state (stopped, failed, or cancelled).",
			map[string]any{"session": session.SessionID},
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
	}

	ensureTraceHeader(w, r.Context())

	// Map State
	resp.State = mapSessionState(session.State)

	if session.Reason != "" {
		r := SessionResponseReason(session.Reason)
		resp.Reason = &r
	}
	resp.ReasonDetail = &session.ReasonDetail

	mode, durationSeconds, seekableStart, seekableEnd, liveEdge := sessionPlaybackInfo(session, time.Now())
	if mode != "" {
		m := SessionResponseMode(mode)
		resp.Mode = &m
	}
	resp.DurationSeconds = toFloat32Ptr(durationSeconds)
	resp.SeekableStartSeconds = toFloat32Ptr(seekableStart)
	resp.SeekableEndSeconds = toFloat32Ptr(seekableEnd)
	resp.LiveEdgeSeconds = toFloat32Ptr(liveEdge)
	pUrl := fmt.Sprintf("/api/v3/sessions/%s/hls/index.m3u8", session.SessionID)
	resp.PlaybackUrl = &pUrl

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func mapSessionState(s model.SessionState) SessionResponseState {
	switch s {
	case model.SessionStarting:
		return STARTING
	case model.SessionPriming:
		return PRIMING
	case model.SessionReady:
		return READY
	case model.SessionDraining:
		return DRAINING
	case model.SessionStopping:
		return STOPPING
	case model.SessionStopped:
		return STOPPED
	case model.SessionFailed:
		return FAILED
	case model.SessionCancelled:
		return CANCELLED
	default:
		return IDLE
	}
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
	// 1. Verify V3 Available
	s.mu.RLock()
	bus := s.v3Bus
	store := s.v3Store
	s.mu.RUnlock()

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

	// Atomic Update via Store
	var changed bool
	updatedSess, err := store.UpdateSession(ctx, sessionId.String(), func(s *model.SessionRecord) error {
		// If we are already in Repair profile, nothing more to do
		if s.Profile.Name == profiles.ProfileRepair {
			return nil
		}

		// Force switch to Repair Profile (CPU Transcode, Safe Settings)
		s.Profile.Name = profiles.ProfileRepair
		s.Profile.TranscodeVideo = true
		s.Profile.Deinterlace = false // Keep simple
		s.Profile.HWAccel = ""        // Force CPU
		s.Profile.VideoCodec = "libx264"
		s.Profile.VideoCRF = 24
		s.Profile.VideoMaxWidth = 1280
		s.Profile.Preset = "veryfast"
		s.Profile.Container = "fmp4" // Ensure FMP4 for Safari

		s.FallbackReason = fmt.Sprintf("client_report:code=%d", derefInt(req.Code))
		s.FallbackAtUnix = time.Now().Unix()
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

	// Start new session
	startEvt := model.StartSessionEvent{
		Type:          model.EventStartSession,
		SessionID:     sess.SessionID,
		ServiceRef:    sess.ServiceRef,
		CorrelationID: sess.CorrelationID,
		RequestedAtUN: time.Now().Unix(),
	}

	if err := bus.Publish(ctx, string(model.EventStartSession), startEvt); err != nil {
		log.L().Error().Err(err).Msg("failed to publish restart event during fallback")
	}

	w.WriteHeader(http.StatusAccepted)
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

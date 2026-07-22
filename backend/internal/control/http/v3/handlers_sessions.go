package v3

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	v3sessions "github.com/ManuGH/xg2g/internal/control/http/v3/sessions"
	"github.com/go-chi/chi/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"
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
	var req PlaybackFeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, "invalid body")
		return
	}

	result, _ := s.sessionsProcessor().ReportPlaybackFeedback(r.Context(), sessionId.String(), toPlaybackFeedbackInput(req))
	if result.Unavailable {
		RespondError(w, r, http.StatusServiceUnavailable, ErrServiceUnavailable, result.ErrorMessage)
		return
	}
	if result.NotFound {
		RespondError(w, r, http.StatusNotFound, ErrSessionFeedbackNotFound)
		return
	}
	if result.InternalError {
		RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, result.ErrorMessage)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func toPlaybackFeedbackInput(req PlaybackFeedbackRequest) v3sessions.PlaybackFeedbackInput {
	var ctxInput *v3sessions.PlaybackEngineErrorContextInput
	if req.Context != nil {
		var engineStr *string
		if req.Context.Engine != nil {
			s := string(*req.Context.Engine)
			engineStr = &s
		}
		var phaseStr *string
		if req.Context.Phase != nil {
			s := string(*req.Context.Phase)
			phaseStr = &s
		}
		ctxInput = &v3sessions.PlaybackEngineErrorContextInput{
			AttemptId:       req.Context.AttemptId,
			Engine:          engineStr,
			Phase:           phaseStr,
			PlaybackEpoch:   req.Context.PlaybackEpoch,
			RecoveryAttempt: req.Context.RecoveryAttempt,
		}
	}
	return v3sessions.PlaybackFeedbackInput{
		Code:    req.Code,
		Context: ctxInput,
		Event:   v3sessions.PlaybackFeedbackEvent(req.Event),
		Message: req.Message,
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

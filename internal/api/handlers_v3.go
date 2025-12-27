// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/ManuGH/xg2g/internal/log"
	v3api "github.com/ManuGH/xg2g/internal/v3/api"
	"github.com/ManuGH/xg2g/internal/v3/model"
	"github.com/ManuGH/xg2g/internal/v3/profiles"
)

// handleV3Intents handles POST /api/v3/intents.
func (s *Server) handleV3Intents(w http.ResponseWriter, r *http.Request) {
	// 0. Hardening: Limit Request Size (1MB)
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)

	// Get Config Snapshot for consistent view during request
	cfg := s.GetConfig()

	// 1. Verify V3 Components Available
	s.mu.RLock()
	bus := s.v3Bus
	store := s.v3Store
	s.mu.RUnlock()

	if bus == nil || store == nil {
		// V3 Worker not running
		RespondError(w, r, http.StatusServiceUnavailable, &APIError{
			Code:    "V3_UNAVAILABLE",
			Message: "v3 control plane not enabled",
		})
		return
	}

	// 2. Decode Request
	var req v3api.IntentRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields() // Hardening
	if err := dec.Decode(&req); err != nil {
		RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, err.Error())
		return
	}

	// 3. Idempotency Check (Store)
	// We check if this intent was already processed recently
	if req.IdempotencyKey != "" {
		ctx := r.Context()
		existingSessionID, ok, err := store.GetIdempotency(ctx, req.IdempotencyKey)
		if err != nil {
			log.L().Error().Err(err).Str("k", req.IdempotencyKey).Msg("v3 idempotency check failed")
			RespondError(w, r, http.StatusServiceUnavailable, ErrServiceUnavailable)
			return
		}
		if ok {
			// Already processed, return established SessionID
			writeJSON(w, http.StatusOK, map[string]string{
				"sessionId": existingSessionID,
				"status":    "idempotent_replay",
			})
			return
		}
	}

	// 4. Generate Session ID (Strong UUID)
	// For START, new ID. For STOP, use provided ID.
	var sessionID string
	intentType := req.Type
	if intentType == "" {
		intentType = model.IntentTypeStreamStart
	}

	switch intentType {
	case model.IntentTypeStreamStart:
		sessionID = uuid.New().String()
	case model.IntentTypeStreamStop:
		if req.SessionID == "" {
			RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, "sessionId required for stop")
			return
		}
		sessionID = req.SessionID
	default:
		RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, "unsupported intent type")
		return
	}

	// Extract ClientIP from params or request (Normalized)
	clientIP := req.Params["client_ip"]
	if clientIP == "" {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err == nil {
			clientIP = host
		} else {
			clientIP = r.RemoteAddr
		}
	}

	// 5. Build & Publish Event
	switch intentType {
	case model.IntentTypeStreamStart:
		requestedProfile := req.ProfileID
		profileSpec := profiles.Resolve(requestedProfile, r.UserAgent(), cfg.DVRWindowSec)
		log.L().
			Info().
			Str("ua", r.UserAgent()).
			Str("requested_profile", requestedProfile).
			Str("profile", profileSpec.Name).
			Msg("intent profile resolved")

		// Create Session Record (Starting state)
		session := &model.SessionRecord{
			SessionID:      sessionID,
			ServiceRef:     req.ServiceRef,
			Profile:        profileSpec,
			State:          model.SessionNew,
			CreatedAtUnix:  time.Now().Unix(),
			UpdatedAtUnix:  time.Now().Unix(),
			LastAccessUnix: time.Now().Unix(),
			ContextData:    map[string]string{"client_ip": clientIP},
		}

		// Persist Session (Atomic)
		if err := store.PutSessionWithIdempotency(r.Context(), session, req.IdempotencyKey, 1*time.Minute); err != nil {
			RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, "failed to persist session")
			return
		}

		event := model.StartSessionEvent{
			Type:          model.EventStartSession,
			SessionID:     sessionID,
			ServiceRef:    req.ServiceRef,
			ProfileID:     profileSpec.Name,
			RequestedAtUN: time.Now().Unix(),
		}
		if err := bus.Publish(r.Context(), string(model.EventStartSession), event); err != nil {
			log.L().Error().Err(err).Msg("failed to publish start event")
			RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, "failed to dispatch intent")
			return
		}
	case model.IntentTypeStreamStop:
		// For STOP, we don't create a session record if it doesn't exist.
		// We just publish the stop event. The worker/orchestrator handles the rest.
		event := model.StopSessionEvent{
			Type:          model.EventStopSession,
			SessionID:     sessionID,
			Reason:        model.RClientStop,
			RequestedAtUN: time.Now().Unix(),
		}
		if err := bus.Publish(r.Context(), string(model.EventStopSession), event); err != nil {
			log.L().Error().Err(err).Msg("failed to publish stop event")
			RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, "failed to dispatch intent")
			return
		}
	}

	// 8. Respond Success (Accepted)
	writeJSON(w, http.StatusAccepted, map[string]string{
		"sessionId": sessionID,
		"status":    "accepted",
	})
}

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

	resp := v3api.SessionResponse{
		SessionID:       session.SessionID,
		ServiceRef:      session.ServiceRef,
		ProfileID:       session.Profile.Name,
		State:           session.State,
		Reason:          session.Reason,
		ReasonDetail:    session.ReasonDetail,
		CorrelationID:   session.CorrelationID,
		UpdatedAtUnixMs: session.UpdatedAtUnix * 1000,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleV3HLS serves HLS playlists and segments.
// Authorization: Requires v3:read scope (enforced by route middleware).
func (s *Server) handleV3HLS(w http.ResponseWriter, r *http.Request) {
	// 1. Check v3 availability
	s.mu.RLock()
	store := s.v3Store
	s.mu.RUnlock()

	if store == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &APIError{
			Code:    "V3_UNAVAILABLE",
			Message: "v3 not available",
		})
		return
	}

	// 2. Extract Params
	sessionID := chi.URLParam(r, "sessionID")
	filename := chi.URLParam(r, "filename")

	// 3. Serve via HLS helper
	v3api.ServeHLS(w, r, store, s.GetConfig().HLSRoot, sessionID, filename)
}

// parsePaginationParams extracts offset and limit from query parameters.
// Defaults: offset=0, limit=100. Max limit: 1000.
func parsePaginationParams(r *http.Request) (offset int, limit int) {
	// Default values
	offset = 0
	limit = 100

	// Parse offset
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if val, err := strconv.Atoi(offsetStr); err == nil && val >= 0 {
			offset = val
		}
	}

	// Parse limit
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if val, err := strconv.Atoi(limitStr); err == nil && val > 0 {
			limit = val
			if limit > 1000 {
				limit = 1000 // Cap at 1000
			}
		}
	}

	return offset, limit
}

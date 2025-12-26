// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/ManuGH/xg2g/internal/log"
	v3api "github.com/ManuGH/xg2g/internal/v3/api"
	"github.com/ManuGH/xg2g/internal/v3/model"
)

// handleV3Intents handles POST /api/v3/intents.
func (s *Server) handleV3Intents(w http.ResponseWriter, r *http.Request) {
	// 0. Hardening: Limit Request Size (1MB)
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)

	// 1. Verify V3 Components Available
	s.mu.RLock()
	bus := s.v3Bus
	store := s.v3Store
	s.mu.RUnlock()

	if bus == nil || store == nil {
		// V3 Worker not running
		http.Error(w, "v3 control plane not enabled", http.StatusServiceUnavailable)
		return
	}

	// 2. Decode Request
	var req v3api.IntentRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields() // Hardening
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// 3. Idempotency Check (Store)
	// We check if this intent was already processed recently
	if req.IdempotencyKey != "" {
		ctx := r.Context()
		existingSessionID, ok, err := store.GetIdempotency(ctx, req.IdempotencyKey)
		if err != nil {
			log.L().Error().Err(err).Str("k", req.IdempotencyKey).Msg("v3 idempotency check failed")
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
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

	// 4a. User-Agent Detection (Safari Fix)
	// If the client is Safari and no specific profile is requested, force "safari" profile
	// to ensure we transcode incompatible formats (like MPEG-2) to H.264.
	isSafari := false
	if ua := r.UserAgent(); ua != "" {
		// Chrome also includes "Safari", so check for "Safari" AND NOT "Chrome"
		// (Simple heuristic, can be improved)
		if strings.Contains(ua, "Safari") && !strings.Contains(ua, "Chrome") && !strings.Contains(ua, "Chromium") {
			isSafari = true
		}
	}

	if isSafari && (req.ProfileID == "" || req.ProfileID == "default" || req.ProfileID == "hd") {
		req.ProfileID = "safari_dvr"
	}
	log.L().Info().Str("ua", r.UserAgent()).Bool("is_safari", isSafari).Str("final_profile", req.ProfileID).Msg("intent profile debug")

	switch intentType {
	case model.IntentTypeStreamStart:
		sessionID = uuid.New().String()
	case model.IntentTypeStreamStop:
		if req.SessionID == "" {
			http.Error(w, "sessionId required for stop", http.StatusBadRequest)
			return
		}
		sessionID = req.SessionID
	default:
		http.Error(w, "unsupported intent type", http.StatusBadRequest)
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
		// Create Session Record (Starting state)
		session := &model.SessionRecord{
			SessionID:  sessionID,
			ServiceRef: req.ServiceRef,
			Profile: model.ProfileSpec{
				Name:         req.ProfileID,
				DVRWindowSec: s.cfg.DVRWindowSec, // Use configured DVR window
			},
			State:          model.SessionNew,
			CreatedAtUnix:  time.Now().Unix(),
			UpdatedAtUnix:  time.Now().Unix(),
			LastAccessUnix: time.Now().Unix(),
			ContextData:    map[string]string{"client_ip": clientIP},
		}

		// Persist Session (Atomic)
		if err := store.PutSessionWithIdempotency(r.Context(), session, req.IdempotencyKey, 1*time.Minute); err != nil {
			http.Error(w, "failed to persist session", http.StatusInternalServerError)
			return
		}

		event := model.StartSessionEvent{
			Type:          model.EventStartSession,
			SessionID:     sessionID,
			ServiceRef:    req.ServiceRef,
			ProfileID:     req.ProfileID,
			RequestedAtUN: time.Now().Unix(),
		}
		if err := bus.Publish(r.Context(), string(model.EventStartSession), event); err != nil {
			log.L().Error().Err(err).Msg("failed to publish start event")
			http.Error(w, "failed to dispatch intent", http.StatusInternalServerError)
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
			http.Error(w, "failed to dispatch intent", http.StatusInternalServerError)
			return
		}
	}

	// 8. Respond Success (Accepted)
	writeJSON(w, http.StatusAccepted, map[string]string{
		"sessionId": sessionID,
		"status":    "accepted",
	})
}

// handleV3SessionsDebug dumps all sessions from the store (Debug/Admin only).
func (s *Server) handleV3SessionsDebug(w http.ResponseWriter, r *http.Request) {
	// 1. Auth check (Strict: DevMode Only)
	// Even if Auth is disabled (Anonymous), this endpoint exposes internal state.
	// We require the server to be in Dev Mode explicitly.
	// TODO: Add Role-Based Access Control (RBAC) in Phase 7B/8.
	if !s.cfg.DevMode {
		http.Error(w, "debug interface disabled (requires XG2G_DEV=true)", http.StatusForbidden)
		return
	}

	// 2. Access Store
	s.mu.RLock()
	store := s.v3Store
	s.mu.RUnlock()

	if store == nil {
		http.Error(w, "v3 store not initialized", http.StatusServiceUnavailable)
		return
	}

	// 3. List Sessions
	sessions, err := store.ListSessions(r.Context())
	if err != nil {
		http.Error(w, "failed to list sessions: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, sessions)
}

// handleV3SessionState returns a single session state without DevMode gating.
func (s *Server) handleV3SessionState(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	store := s.v3Store
	s.mu.RUnlock()

	if store == nil {
		http.Error(w, "v3 store not initialized", http.StatusServiceUnavailable)
		return
	}

	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" || !model.IsSafeSessionID(sessionID) {
		http.Error(w, "invalid session id", http.StatusBadRequest)
		return
	}

	sess, err := store.GetSession(r.Context(), sessionID)
	if err != nil || sess == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	resp := v3api.SessionResponse{
		SessionID:       sess.SessionID,
		ServiceRef:      sess.ServiceRef,
		ProfileID:       sess.Profile.Name,
		State:           sess.State,
		Reason:          sess.Reason,
		ReasonDetail:    sess.ReasonDetail,
		CorrelationID:   sess.CorrelationID,
		UpdatedAtUnixMs: sess.UpdatedAtUnix * 1000,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleV3HLS serves HLS playlists and segments.
func (s *Server) handleV3HLS(w http.ResponseWriter, r *http.Request) {
	// 1. Check v3 availability
	s.mu.RLock()
	store := s.v3Store
	s.mu.RUnlock()

	if store == nil {
		http.Error(w, "v3 not available", http.StatusServiceUnavailable)
		return
	}

	// 2. Extract Params
	sessionID := chi.URLParam(r, "sessionID")
	filename := chi.URLParam(r, "filename")

	// 3. Serve via HLS helper
	v3api.ServeHLS(w, r, store, s.cfg.HLSRoot, sessionID, filename)
}

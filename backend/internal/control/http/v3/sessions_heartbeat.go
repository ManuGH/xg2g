// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/metrics"
)

// SessionHeartbeat handles POST /api/v3/sessions/{id}/heartbeat (ADR-009)
// Renews session lease if not expired. Returns 410 if already expired.
// Idempotent: multiple heartbeats within interval = no-op.
func (s *Server) SessionHeartbeat(w http.ResponseWriter, r *http.Request, sessionID string) {
	s.ScopeMiddleware(ScopeV3Read)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		s.handleSessionHeartbeat(w, r, sessionID)
	})).ServeHTTP(w, r)
}

func (s *Server) handleSessionHeartbeat(w http.ResponseWriter, r *http.Request, sessionID string) {
	log.L().Info().Str("sessionId", sessionID).Msg("Session heartbeat received")

	ctx := r.Context()
	logger := log.WithComponentFromContext(ctx, "api")

	// Get config snapshot
	deps := s.sessionsModuleDeps()
	cfg := deps.cfg
	store := deps.store

	if store == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &APIError{
			Code:    "V3_UNAVAILABLE",
			Message: "v3 control plane not enabled",
		})
		return
	}

	session, err := store.GetSession(ctx, sessionID)
	if err != nil {
		logger.Error().Err(err).Str("sessionId", sessionID).Msg("heartbeat: store error")
		writeProblem(w, r, http.StatusInternalServerError, "sessions/store_error", "Session Store Error", "STORE_ERROR", err.Error(), nil)
		return
	}
	if session == nil {
		writeProblem(w, r, http.StatusNotFound, "sessions/not_found", "Session Not Found", "SESSION.NOT_FOUND", "The requested session does not exist.", map[string]any{"sessionId": sessionID})
		return
	}

	// 1.5 Check if terminal
	if session.State.IsTerminal() {
		logger.Debug().Str("sessionId", sessionID).Str("state", string(session.State)).Msg("heartbeat rejected: session terminal")
		metrics.IncSessionHeartbeatTerminal()
		extra := map[string]any{
			"sessionId": sessionID,
			"state":     string(session.State),
			"reason":    string(session.Reason),
		}
		writeProblem(w, r, http.StatusGone, "sessions/terminal", "Session is no longer active", "SESSION.TERMINAL", "The session has reached a terminal state and cannot be heartbeated.", extra)
		return
	}

	// 2. Check if already expired
	now := time.Now().Unix()
	if now > session.LeaseExpiresAtUnix {
		writeProblem(w, r, http.StatusGone, "sessions/expired", "Session lease has expired", "SESSION.EXPIRED", "The session lease has expired and cannot be renewed.", map[string]any{"sessionId": sessionID})
		return
	}

	// 3. Check idempotency: Only extend once per heartbeat_interval
	// ADR-009: Idempotent heartbeat within interval
	timeSinceLastHB := now - session.LastHeartbeatUnix
	if timeSinceLastHB < int64(session.HeartbeatInterval) {
		// No-op: Return current expiry (idempotent)
		logger.Debug().
			Str("sessionId", sessionID).
			Int64("time_since_last", timeSinceLastHB).
			Msg("heartbeat idempotent (within interval)")

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sessionId":        sessionID,
			"lease_expires_at": time.Unix(session.LeaseExpiresAtUnix, 0).Format(time.RFC3339),
			"acknowledged":     true,
		})
		return
	}

	// 4. Extend lease (backend-controlled, best-effort)
	// ADR-009: Use config TTL (CTO Patch 1 - NO hardcoded + 60)
	newExpiry := time.Now().Add(cfg.Sessions.LeaseTTL).Unix()

	_, err = store.UpdateSession(ctx, sessionID, func(s *model.SessionRecord) error {
		s.LeaseExpiresAtUnix = newExpiry
		s.LastHeartbeatUnix = now
		return nil
	})

	if err != nil {
		logger.Error().Err(err).Str("sessionId", sessionID).Msg("failed to extend lease")
		writeProblem(w, r, http.StatusInternalServerError, "sessions/update_error", "Internal Server Error", "SESSION.UPDATE_ERROR", "Failed to update session lease.", nil)
		return
	}

	logger.Debug().
		Str("sessionId", sessionID).
		Int64("new_expiry", newExpiry).
		Msg("session lease extended")

	// 5. Return new expiry
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"sessionId":        sessionID,
		"lease_expires_at": time.Unix(newExpiry, 0).Format(time.RFC3339),
		"acknowledged":     true,
	})
}

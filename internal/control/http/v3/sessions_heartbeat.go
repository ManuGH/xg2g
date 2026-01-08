// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

// SessionHeartbeat handles POST /api/v3/sessions/{id}/heartbeat (ADR-009)
// Renews session lease if not expired. Returns 410 if already expired.
// Idempotent: multiple heartbeats within interval = no-op.
func (s *Server) SessionHeartbeat(w http.ResponseWriter, r *http.Request, sessionID string) {
	ctx := r.Context()
	logger := log.WithComponentFromContext(ctx, "api")

	// Get config snapshot
	cfg := s.GetConfig()

	// 1. Get session
	s.mu.RLock()
	store := s.v3Store
	s.mu.RUnlock()

	if store == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &APIError{
			Code:    "V3_UNAVAILABLE",
			Message: "v3 control plane not enabled",
		})
		return
	}

	session, err := store.GetSession(ctx, sessionID)
	if err != nil || session == nil {
		RespondError(w, r, http.StatusNotFound, &APIError{
			Code:    "SESSION.NOT_FOUND",
			Message: "Session not found",
		})
		return
	}

	// 2. Check if already expired
	now := time.Now().Unix()
	if now > session.LeaseExpiresAtUnix {
		RespondError(w, r, http.StatusGone, &APIError{
			Code:    "SESSION.EXPIRED",
			Message: "Session lease has expired",
		})
		return
	}

	// 3. Check idempotency: Only extend once per heartbeat_interval
	// ADR-009: Idempotent heartbeat within interval
	timeSinceLastHB := now - session.LastHeartbeatUnix
	if timeSinceLastHB < int64(session.HeartbeatInterval) {
		// No-op: Return current expiry (idempotent)
		logger.Debug().
			Str("session_id", sessionID).
			Int64("time_since_last", timeSinceLastHB).
			Msg("heartbeat idempotent (within interval)")

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"session_id":       sessionID,
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
		logger.Error().Err(err).Str("session_id", sessionID).Msg("failed to extend lease")
		RespondError(w, r, http.StatusInternalServerError, ErrInternalServer)
		return
	}

	logger.Debug().
		Str("session_id", sessionID).
		Int64("new_expiry", newExpiry).
		Msg("session lease extended")

	// 5. Return new expiry
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"session_id":       sessionID,
		"lease_expires_at": time.Unix(newExpiry, 0).Format(time.RFC3339),
		"acknowledged":     true,
	})
}

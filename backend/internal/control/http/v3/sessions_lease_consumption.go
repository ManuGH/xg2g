// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/log"
)

// renewLeaseFromConsumption extends a live session's lease when a client is
// actively consuming its HLS artifacts. A playing client refetches the
// playlist every target duration, which makes consumption a stronger
// liveness signal than JS heartbeats: iOS Safari throttles or suspends
// timers during fullscreen/background playback, so a session can be
// streaming flawlessly while its heartbeat loop is dead (2026-07-05: a
// session was reaped by lease timeout while its playlist was fetched every
// 2 seconds).
//
// Renewal shares the heartbeat idempotency window (LastHeartbeatUnix), so it
// writes at most once per heartbeat interval per session. Expired or
// terminal sessions are left untouched — the reaper owns those.
func (s *Server) renewLeaseFromConsumption(ctx context.Context, sessionID string) {
	deps := s.sessionsModuleDeps()
	store := deps.store
	if store == nil {
		return
	}

	session, err := store.GetSession(ctx, sessionID)
	if err != nil || session == nil {
		return
	}
	if session.State.IsTerminal() {
		return
	}

	now := time.Now().Unix()
	if session.LeaseExpiresAtUnix > 0 && now > session.LeaseExpiresAtUnix {
		return
	}
	if now-session.LastHeartbeatUnix < int64(session.HeartbeatInterval) {
		return
	}

	ttl := deps.cfg.Sessions.LeaseTTL
	if ttl <= 0 {
		return
	}
	newExpiry := time.Now().Add(ttl).Unix()
	if newExpiry <= session.LeaseExpiresAtUnix {
		return
	}

	logger := log.WithComponentFromContext(ctx, "api")
	if _, err := store.UpdateSession(ctx, sessionID, func(rec *model.SessionRecord) error {
		rec.LeaseExpiresAtUnix = newExpiry
		rec.LastHeartbeatUnix = now
		return nil
	}); err != nil {
		logger.Warn().
			Err(err).
			Str("sessionId", sessionID).
			Msg("hls consumption lease renewal failed")
		return
	}

	logger.Debug().
		Str("sessionId", sessionID).
		Int64("new_expiry", newExpiry).
		Msg("session lease extended via hls consumption")
}

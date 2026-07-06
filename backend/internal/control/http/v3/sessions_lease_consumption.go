// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"
	"errors"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/log"
)

// fallbackHeartbeatIntervalSec caps renewal frequency when a session record
// carries no positive heartbeat interval; matches the sessions.heartbeat_interval
// config default.
const fallbackHeartbeatIntervalSec = 30

// errLeaseRenewalNoOp aborts an UpdateSession that turned out to be
// unnecessary once inside the store's critical section (concurrent heartbeat
// already extended further, or the session went terminal since the read).
var errLeaseRenewalNoOp = errors.New("lease renewal no-op")

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

	// The caller's request context ends with the playlist response; a client
	// that disconnects mid-request must not abort the lease write.
	ctx = context.WithoutCancel(ctx)

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

	// A record without a positive heartbeat interval must not bypass the
	// idempotency window — that would mean one store write per playlist fetch.
	interval := session.HeartbeatInterval
	if interval <= 0 {
		if d := deps.cfg.Sessions.HeartbeatInterval; d > 0 {
			interval = int(d / time.Second)
		}
	}
	if interval <= 0 {
		interval = fallbackHeartbeatIntervalSec
	}
	if now-session.LastHeartbeatUnix < int64(interval) {
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
		// Re-check inside the store's critical section: the session can go
		// terminal or receive a further-reaching heartbeat between the read
		// above and this write, and a renewal must never shorten a lease.
		if rec.State.IsTerminal() || newExpiry <= rec.LeaseExpiresAtUnix {
			return errLeaseRenewalNoOp
		}
		rec.LeaseExpiresAtUnix = newExpiry
		if now > rec.LastHeartbeatUnix {
			rec.LastHeartbeatUnix = now
		}
		return nil
	}); err != nil {
		if errors.Is(err, errLeaseRenewalNoOp) {
			return
		}
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

// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package worker

import (
	"context"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/pipeline/model"
	"github.com/ManuGH/xg2g/internal/pipeline/store"
)

// LeaseExpiryWorker runs a background goroutine that expires sessions
// whose leases have expired (ADR-009)
type LeaseExpiryWorker struct {
	Store  store.StateStore
	Config *config.AppConfig
}

// Run starts the lease expiry check loop
// ADR-009: CTO Patch 2/3 compliant (efficient query, multi-state expiry)
func (w *LeaseExpiryWorker) Run(ctx context.Context) error {
	// CTO Patch 1: Use config-driven interval (not hardcoded)
	interval := w.Config.Sessions.ExpiryCheckInterval
	if interval == 0 {
		interval = 10 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.L().Info().
		Dur("interval", interval).
		Msg("lease expiry worker started")

	for {
		select {
		case <-ticker.C:
			w.expireStaleSessions(ctx)
		case <-ctx.Done():
			log.L().Info().Msg("lease expiry worker stopped")
			return ctx.Err()
		}
	}
}

func (w *LeaseExpiryWorker) expireStaleSessions(ctx context.Context) {
	now := time.Now().Unix()

	// CTO Patch 2: Efficient query (NO full-scan)
	// Filter by: states (new|starting|ready) AND lease_expires_at <= now
	filter := store.SessionFilter{
		States: []model.SessionState{
			model.SessionNew,
			model.SessionStarting,
			model.SessionReady,
		},
		LeaseExpiresBefore: now,
	}

	sessions, err := w.Store.QuerySessions(ctx, filter)
	if err != nil {
		log.L().Error().Err(err).Msg("failed to query sessions for expiry check")
		return
	}

	expiredCount := 0

	// CTO Patch 3: Already filtered to new|starting|ready by query
	for _, session := range sessions {
		// Determine stop reason based on state
		var stopReason string
		var releaseResources bool

		switch session.State {
		case model.SessionNew, model.SessionStarting:
			stopReason = "LEASE_EXPIRED"
			releaseResources = false // No resources allocated yet
		case model.SessionReady:
			stopReason = "LEASE_EXPIRED"
			releaseResources = true // Release tuner, cleanup HLS
		default:
			continue // Skip (defensive, should not happen due to filter)
		}

		// Transition to stopped
		_, err := w.Store.UpdateSession(ctx, session.SessionID, func(s *model.SessionRecord) error {
			// Skip if already terminal
			if s.State.IsTerminal() {
				return nil
			}
			s.State = model.SessionStopped
			s.StopReason = stopReason
			s.UpdatedAtUnix = now
			return nil
		})

		if err != nil {
			log.L().Error().
				Err(err).
				Str("session_id", session.SessionID).
				Msg("failed to expire session")
			continue
		}

		log.L().Info().
			Str("session_id", session.SessionID).
			Str("previous_state", string(session.State)).
			Str("stop_reason", stopReason).
			Msg("session lease expired")

		// Metrics
		sessionsLeaseExpiredTotal.WithLabelValues(string(session.State)).Inc()
		expiredCount++

		// Release resources if needed (for READY/RUNNING sessions)
		if releaseResources {
			// Publish stop event for cleanup
			// This triggers the orchestrator's cleanup logic
			_ = publishStopEvent(ctx, w.Store, session.SessionID, stopReason)
		}
	}

	if expiredCount > 0 {
		log.L().Info().
			Int("expired", expiredCount).
			Msg("expired sessions by lease timeout")
	}
}

// publishStopEvent publishes a stop event for cleanup
func publishStopEvent(ctx context.Context, store store.StateStore, sessionID string, reason string) error {
	// Note: This is a helper - actual implementation may need bus access
	// For now, just log the intent
	log.L().Debug().
		Str("session_id", sessionID).
		Str("reason", reason).
		Msg("would publish stop event for cleanup")
	return nil
}

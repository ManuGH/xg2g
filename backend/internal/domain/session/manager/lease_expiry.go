// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package manager

import (
	"context"
	"errors"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/domain/session/lifecycle"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/log"
)

type stopEventBus interface {
	Publish(ctx context.Context, topic string, event interface{}) error
}

// LeaseExpiryWorker runs a background goroutine that expires sessions
// whose leases have expired (ADR-009)
type LeaseExpiryWorker struct {
	Store  store.SessionExpiryStore
	Bus    stopEventBus
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
	expiredAt := time.Unix(now, 0)

	// CTO Patch 2: Efficient query (NO full-scan)
	// Filter by: states (new|starting|priming|ready) AND lease_expires_at <= now
	filter := store.SessionFilter{
		States: []model.SessionState{
			model.SessionNew,
			model.SessionStarting,
			model.SessionPriming,
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

	// CTO Patch 3: Already filtered to new|starting|priming|ready by query
	for _, session := range sessions {
		// Determine stop reason based on state
		var stopReason string
		var releaseResources bool

		switch session.State {
		case model.SessionNew:
			stopReason = "LEASE_EXPIRED"
			releaseResources = false // No resources allocated yet
		case model.SessionStarting:
			stopReason = "LEASE_EXPIRED"
			releaseResources = true // STARTING already owns dedup/tuner leases
		case model.SessionPriming:
			stopReason = "LEASE_EXPIRED"
			releaseResources = true // Priming already owns runtime resources and must be cleaned up
		case model.SessionReady:
			stopReason = "LEASE_EXPIRED"
			releaseResources = true // Release tuner, cleanup HLS
		default:
			continue // Skip (defensive, should not happen due to filter)
		}

		var err error
		if releaseResources {
			err = w.requestCleanupStop(ctx, session, stopReason, expiredAt)
		} else {
			err = w.markLeaseExpired(ctx, session.SessionID, stopReason, expiredAt)
		}
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
	}

	if expiredCount > 0 {
		log.L().Info().
			Int("expired", expiredCount).
			Msg("expired sessions by lease timeout")
	}
}

func (w *LeaseExpiryWorker) markLeaseExpired(ctx context.Context, sessionID string, stopReason string, now time.Time) error {
	_, err := w.Store.UpdateSession(ctx, sessionID, func(s *model.SessionRecord) error {
		if s == nil || s.State.IsTerminal() {
			return nil
		}
		_, err := lifecycle.Dispatch(s, lifecycle.PhaseFromState(s.State), lifecycle.Event{Kind: lifecycle.EvLeaseExpired}, nil, false, now)
		if err != nil {
			return err
		}
		s.StopReason = stopReason
		return nil
	})
	return err
}

func (w *LeaseExpiryWorker) requestCleanupStop(ctx context.Context, session *model.SessionRecord, stopReason string, now time.Time) error {
	if w.Bus == nil {
		return errors.New("lease expiry cleanup bus not configured")
	}
	if session == nil {
		return errors.New("session is nil")
	}

	var shouldPublish bool
	_, err := w.Store.UpdateSession(ctx, session.SessionID, func(s *model.SessionRecord) error {
		if s == nil || s.State.IsTerminal() {
			return nil
		}
		if s.State == model.SessionStopping {
			if s.StopReason == "" {
				s.StopReason = stopReason
			}
			if s.Reason == "" {
				s.Reason = model.RLeaseExpired
			}
			s.PipelineState = model.PipeStopRequested
			return nil
		}

		_, err := lifecycle.Dispatch(
			s,
			lifecycle.PhaseFromState(s.State),
			lifecycle.Event{Kind: lifecycle.EvStopRequested, Reason: model.RLeaseExpired},
			nil,
			false,
			now,
		)
		if err != nil {
			return err
		}
		s.StopReason = stopReason
		s.PipelineState = model.PipeStopRequested
		shouldPublish = true
		return nil
	})
	if err != nil || !shouldPublish {
		return err
	}

	event := model.StopSessionEvent{
		Type:          model.EventStopSession,
		SessionID:     session.SessionID,
		Reason:        model.RLeaseExpired,
		CorrelationID: session.CorrelationID,
		RequestedAtUN: now.Unix(),
	}
	return w.Bus.Publish(ctx, string(model.EventStopSession), event)
}

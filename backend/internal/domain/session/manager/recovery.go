// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package manager

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/lifecycle"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/rs/zerolog"
)

// recoverStaleLeases scans for sessions in intermediate states with expired leases
// and resets them to a safe state (NEW or FAILED).
func (o *Orchestrator) recoverStaleLeases(ctx context.Context) error {
	logger := log.L().With().Str("component", "worker.recovery").Logger()
	logger.Info().Msg("starting recovery sweep")

	start := time.Now()
	recoveredCount := 0

	// Phase 1: Identify Candidates (Read-Only)
	var candidates []*model.SessionRecord
	err := o.Store.ScanSessions(ctx, func(s *model.SessionRecord) error {
		if isIntermediateState(s.State) {
			// Copy to safe struct
			cp := *s
			candidates = append(candidates, &cp)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Phase 2: Attempt Recovery (Read-Write)
	for _, s := range candidates {
		if !shouldRecover(start, o.LeaseTTL, s) {
			continue
		}
		// Probe-based recovery only applies to sessions that participate in the
		// tuner-lease protocol — i.e. that recorded a held tuner slot. A leaseless
		// session (recording/VOD) has no lease whose acquisition proves liveness;
		// probing a fabricated key would trivially succeed and force-fail a live
		// session. Skip it and let freshness-based mechanisms own leaseless liveness.
		probeKey, ok := o.recoveryLeaseKey(s)
		if !ok {
			logger.Debug().Str("session_id", s.SessionID).Msg("skipping probe-recovery: session holds no tuner lease")
			continue
		}
		probeOwner := fmt.Sprintf("recovery-probe-%d", os.Getpid())
		lease, acquired, err := o.Store.TryAcquireLease(ctx, probeKey, probeOwner, o.LeaseTTL)
		if err != nil {
			logger.Warn().Err(err).Str("session_id", s.SessionID).Msg("failed to probe lease during recovery")
			continue
		}

		if !acquired {
			continue // Active elsewhere
		}

		// We acquired -> Recover
		// Release immediately (or defer in loop scope?)
		// Defer in loop scope is bad (leaks until function end).
		// Function call wrapper or explicit release.
		handleRecovery(ctx, o, s, lease, logger)
		recoveredCount++
		jobsTotal.WithLabelValues("recovered").Inc()
	}

	logger.Info().
		Int("recovered_count", recoveredCount).
		Dur("duration", time.Since(start)).
		Msg("recovery sweep complete")

	return nil
}

func shouldRecover(now time.Time, leaseTTL time.Duration, s *model.SessionRecord) bool {
	if leaseTTL <= 0 {
		return true
	}
	if s == nil {
		return false
	}
	ts := s.UpdatedAtUnix
	if ts == 0 {
		ts = s.CreatedAtUnix
	}
	if ts == 0 {
		return true
	}
	age := now.Sub(time.Unix(ts, 0))
	return age >= leaseTTL
}

func handleRecovery(ctx context.Context, o *Orchestrator, s *model.SessionRecord, l store.Lease, logger zerolog.Logger) bool {
	defer func() {
		if err := o.Store.ReleaseLease(ctx, l.Key(), l.Owner()); err != nil {
			logger.Error().Err(err).
				Str("lease_key", l.Key()).
				Str("owner", l.Owner()).
				Msg("failed to release recovery lease")
		}
	}()

	targetState := determineRecoveryTarget(s.State)
	logger.Info().
		Str("session_id", s.SessionID).
		Str("from_state", string(s.State)).
		Str("to_state", string(targetState)).
		Msg("recovering stale session")

	_, err := o.Store.UpdateSession(ctx, s.SessionID, func(r *model.SessionRecord) error {
		var ev lifecycle.Event
		switch targetState {
		case model.SessionNew:
			ev = lifecycle.Event{Kind: lifecycle.EvRecoveryReset}
		default:
			ev = lifecycle.Event{Kind: lifecycle.EvRecoveryFail}
		}
		if _, err := lifecycle.Dispatch(r, lifecycle.PhaseFromState(r.State), ev, nil, false, time.Now()); err != nil {
			return err
		}
		if r.ContextData == nil {
			r.ContextData = make(map[string]string)
		}
		r.ContextData["recovered"] = "true"
		r.ContextData["recovered_from"] = string(s.State)
		return nil
	})

	if err != nil {
		logger.Error().Err(err).Str("session_id", s.SessionID).Msg("failed to update session during recovery")
		return false
	}
	return true
}

func isIntermediateState(s model.SessionState) bool {
	switch s {
	case model.SessionStarting, model.SessionPriming, model.SessionStopping, model.SessionDraining, model.SessionReady:
		return true
	// TUNING, PACKAGING, LEASED etc would go here if defined in SessionState (they are PipelineStates in model)
	// model.SessionState is coarse.
	default:
		return false
	}
}

func determineRecoveryTarget(s model.SessionState) model.SessionState {
	switch s {
	case model.SessionStarting:
		return model.SessionNew // Retry start
	case model.SessionPriming:
		return model.SessionFailed
	case model.SessionStopping, model.SessionDraining, model.SessionReady:
		return model.SessionFailed // Abort stop/drain/ready -> Failed (safest terminal)
	default:
		return model.SessionFailed
	}
}

// recoveryLeaseKey returns the tuner-slot lease key a session recorded, plus whether the
// session participates in the lease protocol at all. The probe's entire inference is "I
// could acquire this session's lease, therefore its owner is gone" — which is only valid for
// a key the session actually held. A session that persisted a tuner slot (Live, via
// transitionStarting) holds such a lease; everything else (recording/VOD, or any future
// leaseless mode) holds none, so ok is false and the caller must not probe. This keys on
// lease-participation (a recorded slot), NOT on session type, so a future leaseless mode is
// covered without another patch. The old LeaseKeyService fallback probed a key these
// sessions never held, which acquired trivially and force-failed live, leaseless sessions.
func (o *Orchestrator) recoveryLeaseKey(s *model.SessionRecord) (string, bool) {
	if s != nil && s.ContextData != nil {
		if raw := s.ContextData[model.CtxKeyTunerSlot]; raw != "" {
			if slot, err := strconv.Atoi(raw); err == nil {
				return model.LeaseKeyTunerSlot(slot), true
			}
		}
	}
	return "", false
}

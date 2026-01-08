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

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/pipeline/lease"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
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
		// Check Lease Status (Probe)
		// Phase 8-2b: Probe correct key (Tuner Slot if present, fallback to ServiceRef)
		probeKey := o.recoveryLeaseKey(s)
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
	defer func() { _ = o.Store.ReleaseLease(ctx, l.Key(), l.Owner()) }()

	targetState := determineRecoveryTarget(s.State)
	logger.Info().
		Str("session_id", s.SessionID).
		Str("from_state", string(s.State)).
		Str("to_state", string(targetState)).
		Msg("recovering stale session")

	_, err := o.Store.UpdateSession(ctx, s.SessionID, func(r *model.SessionRecord) error {
		r.State = targetState
		r.UpdatedAtUnix = time.Now().Unix()
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

func (o *Orchestrator) recoveryLeaseKey(s *model.SessionRecord) string {
	if s != nil && s.ContextData != nil {
		if raw := s.ContextData[model.CtxKeyTunerSlot]; raw != "" {
			if slot, err := strconv.Atoi(raw); err == nil {
				return lease.LeaseKeyTunerSlot(slot)
			}
		}
	}
	// fallback (legacy/partial)
	return lease.LeaseKeyService(s.ServiceRef)
}

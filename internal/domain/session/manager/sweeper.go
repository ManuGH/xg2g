package manager

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/log"
)

// SweeperConfig defines retention policies.
type SweeperConfig struct {
	Interval         time.Duration
	SessionRetention time.Duration // How long to keep terminal sessions in Store
	FileRetention    time.Duration // How long to keep orphan files? (Or strict sync?)
	IdleTimeout      time.Duration // Stop READY sessions after no client access (0 disables)
}

// Sweeper performs background cleanup of stale sessions and files.
type Sweeper struct {
	Orch      *Orchestrator
	Conf      SweeperConfig
	RecoverFn func(context.Context) error // optional; if nil, uses Orch.recoverStaleLeases
}

// Run starts the sweeper loop. It periodically calls SweepOnce on a ticker.
func (s *Sweeper) Run(ctx context.Context) {
	if s.Conf.Interval <= 0 {
		return
	}

	ticker := time.NewTicker(s.Conf.Interval)
	defer ticker.Stop()

	log.L().Info().Dur("interval", s.Conf.Interval).Msg("background sweeper started")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.SweepOnce(ctx)
		}
	}
}

// SweepOnce performs exactly one sweep pass: recovery, store cleanup, file cleanup.
// This method is deterministic and suitable for unit testing.
func (s *Sweeper) SweepOnce(ctx context.Context) {
	if s.RecoverFn != nil {
		if err := s.RecoverFn(ctx); err != nil {
			log.L().Warn().Err(err).Msg("recovery sweep failed")
		}
	} else {
		if err := s.Orch.recoverStaleLeases(ctx); err != nil {
			log.L().Warn().Err(err).Msg("recovery sweep failed")
		}
	}
	s.sweepStore(ctx)
	s.sweepFiles(ctx)
}

func (s *Sweeper) sweepStore(ctx context.Context) {
	now := time.Now()
	expiredCount := 0

	var toDelete []string
	var toFinalize []string
	var toStop []string
	count := 0
	err := s.Orch.Store.ScanSessions(ctx, func(r *model.SessionRecord) error {
		count++

		// Guard: UpdatedAtUnix safety
		updatedAt := r.UpdatedAtUnix
		if updatedAt == 0 {
			updatedAt = r.CreatedAtUnix
		}
		if updatedAt == 0 {
			updatedAt = now.Unix() // Assume fresh
		}
		age := now.Sub(time.Unix(updatedAt, 0))

		// Rule 1: Terminal Sessions (STOPPED, FAILED, CANCELLED) -> Retention
		// Fix 17: Ensure leases are released for terminal sessions (Janitor)
		// This handles cases where the process crashed or logic failed to release on exit.
		if r.State.IsTerminal() {
			s.Orch.ForceReleaseLeases(ctx, r.SessionID, r.ServiceRef, r)

			if age > s.Conf.SessionRetention {
				toDelete = append(toDelete, r.SessionID)
			}
			return nil
		}

		// Rule 2: Stuck STOPPING Sessions -> Force Finalize to STOPPED (Safe)
		// If a session remains in STOPPING for > 1 minute, assume worker failed/died.
		// We do NOT delete immediately to avoid race conditions with active processes.
		// Instead, we force state -> STOPPED, and let next sweep handle retention.
		if r.State == model.SessionStopping {
			if age > 1*time.Minute {
				toFinalize = append(toFinalize, r.SessionID)
			}
		}

		// Rule 3: Idle READY sessions -> STOP (if enabled)
		if s.Conf.IdleTimeout > 0 && (r.State == model.SessionReady || r.State == model.SessionDraining) {
			lastAccess := r.LastAccessUnix
			if lastAccess == 0 {
				lastAccess = r.UpdatedAtUnix
			}
			if lastAccess == 0 {
				lastAccess = r.CreatedAtUnix
			}
			if lastAccess > 0 && now.Sub(time.Unix(lastAccess, 0)) > s.Conf.IdleTimeout {
				toStop = append(toStop, r.SessionID)
			}
		}

		return nil
	})

	if err != nil {
		log.L().Error().Err(err).Msg("sweep scan failed")
		return
	}

	// 1. Finalize Stuck Sessions
	for _, sid := range toFinalize {
		_, err := s.Orch.Store.UpdateSession(ctx, sid, func(r *model.SessionRecord) error {
			if r.State.IsTerminal() {
				return nil // Already handled
			}
			r.State = model.SessionStopped
			r.PipelineState = model.PipeStopped
			r.Reason = model.RIdleTimeout
			r.ReasonDetail = "sweeper_forced_stop_stuck"
			r.UpdatedAtUnix = time.Now().Unix()
			return nil
		})
		if err != nil {
			log.L().Warn().Err(err).Str("sid", sid).Msg("failed to finalize stuck session")
		} else {
			// Trigger cleanup?
			s.Orch.cleanupFiles(sid)
		}
	}

	// 2. Stop Idle Sessions
	for _, sid := range toStop {
		err := s.Orch.handleStop(ctx, model.StopSessionEvent{
			Type:          model.EventStopSession,
			SessionID:     sid,
			Reason:        model.RIdleTimeout,
			RequestedAtUN: time.Now().Unix(),
		})
		if err != nil {
			log.L().Warn().Err(err).Str("sid", sid).Msg("failed to stop idle session")
		}
	}

	// 3. Delete Expired Sessions
	for _, sid := range toDelete {
		if err := s.Orch.Store.DeleteSession(ctx, sid); err != nil {
			log.L().Warn().Err(err).Str("sid", sid).Msg("failed to delete expired session")
		} else {
			expiredCount++
			s.Orch.cleanupFiles(sid)
		}
	}

	if expiredCount > 0 {
		log.L().Info().Int("count", expiredCount).Msg("sweep store removed expired sessions")
	}
}

func (s *Sweeper) sweepFiles(ctx context.Context) {
	if s.Orch.HLSRoot == "" {
		return
	}
	sessionsDir := filepath.Join(s.Orch.HLSRoot, "sessions")

	// Check if sessionsDir exists
	if _, err := os.Stat(sessionsDir); os.IsNotExist(err) {
		return
	}

	orphanCount := 0
	retention := s.Conf.FileRetention
	if retention == 0 {
		retention = s.Conf.SessionRetention
	}
	cutoff := time.Now().Add(-retention)

	// List directories in /sessions/
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		log.L().Warn().Err(err).Msg("sweep files failed to read dir")
		return
	}

	// Build map of active sessions to avoid O(N²) lookups
	activeSessions := make(map[string]bool)
	allSessions, err := s.Orch.Store.ListSessions(ctx)
	if err != nil {
		log.L().Warn().Err(err).Msg("sweep files failed to list sessions, using slow path")
		// Fallback to individual lookups
	} else {
		for _, sess := range allSessions {
			activeSessions[sess.SessionID] = true
		}
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sid := e.Name()
		if !model.IsSafeSessionID(sid) {
			continue // Skip unsafe/unknown paths
		}

		info, err := e.Info()
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoff) {
			// Fast path: check in-memory map
			if len(activeSessions) > 0 {
				if !activeSessions[sid] {
					// Not in active sessions -> Orphan
					s.Orch.cleanupFiles(sid)
					orphanCount++
				}
			} else {
				// Slow path: individual lookup (fallback)
				rec, err := s.Orch.Store.GetSession(ctx, sid)
				if err != nil {
					continue // Store error, skip safety
				}
				if rec == nil {
					// Not in store, and old -> Orphan
					s.Orch.cleanupFiles(sid)
					orphanCount++
				}
			}
		}
	}

	if orphanCount > 0 {
		log.L().Info().Int("count", orphanCount).Msg("sweep files removed orphan directories")
	}
}

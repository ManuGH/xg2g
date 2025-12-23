package worker

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/v3/model"
)

// SweeperConfig defines retention policies.
type SweeperConfig struct {
	Interval         time.Duration
	SessionRetention time.Duration // How long to keep terminal sessions in Store
	FileRetention    time.Duration // How long to keep orphan files? (Or strict sync?)
}

// Sweeper performs background cleanup of stale sessions and files.
type Sweeper struct {
	Orch *Orchestrator
	Conf SweeperConfig
}

func (s *Sweeper) Run(ctx context.Context) {
	ticker := time.NewTicker(s.Conf.Interval)
	defer ticker.Stop()

	log.L().Info().Dur("interval", s.Conf.Interval).Msg("background sweeper started")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweepStore(ctx)
			s.sweepFiles(ctx)
		}
	}
}

func (s *Sweeper) sweepStore(ctx context.Context) {
	now := time.Now()
	expiredCount := 0

	var toDelete []string
	var toFinalize []string
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
		if r.State.IsTerminal() {
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

	// 2. Delete Expired Sessions
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

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sid := e.Name()
		if !safeIDRe.MatchString(sid) {
			continue // Skip unsafe/unknown paths
		}

		// Check if active in Store?
		// Optimization: Gather all active SIDs first? Or check individual?
		// Checking individual is slow (N store calls).
		// Better: Build map of known SIDs from Store Scan?
		// But Scan is expensive too.
		// MVP: Check if dir mod time is old?

		info, err := e.Info()
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoff) {
			// Candidate for deep check: Is it in Store?
			rec, err := s.Orch.Store.GetSession(ctx, sid)
			if err != nil {
				continue // Store error, skip safety
			}
			if rec == nil {
				// Not in store, and old -> Orphan
				s.Orch.cleanupFiles(sid)
				orphanCount++
			} else {
				// In store. Check if it matches our retention policy (should have been swept by sweepStore if terminal)
				// If it's active but old modtime? (Maybe streaming but no file updates? Unlikely for HLS)
			}
		}
	}

	if orphanCount > 0 {
		log.L().Info().Int("count", orphanCount).Msg("sweep files removed orphan directories")
	}
}

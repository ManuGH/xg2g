package v3

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	admissionmonitor "github.com/ManuGH/xg2g/internal/admission"
	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/metrics"
)

// StartMonitor begins the background storage health checks.
func (s *Server) StartMonitor(ctx context.Context) {
	s.monitorMu.Lock()
	defer s.monitorMu.Unlock()

	if s.storageMonitor != nil && !s.monitorStarted {
		s.monitorStarted = true
		go s.storageMonitor.Start(ctx, 30*time.Second, s)
		log.L().Info().Msg("storage_monitor: background loop started")
	}
}

// StartRecordingCacheEvicter starts a background task to clean up old recording cache entries.
func (s *Server) StartRecordingCacheEvicter(ctx context.Context) {
	// Fixed cadence: eviction runs every 10 minutes. Effective TTL is bounded by this interval.
	const interval = 10 * time.Minute

	warnedCadenceMismatch := false
	runOnce := func() {
		cfg := s.GetConfig()
		if strings.TrimSpace(cfg.HLS.Root) == "" {
			metrics.SetRecordingCacheEntries(0)
			return
		}
		if cfg.VODCacheMaxEntries <= 0 {
			log.L().Error().Int("maxEntries", cfg.VODCacheMaxEntries).Msg("recording cache eviction disabled: invalid maxEntries")
			return
		}
		if cfg.VODCacheTTL > 0 && cfg.VODCacheTTL < interval {
			if !warnedCadenceMismatch {
				log.L().Warn().
					Dur("ttl", cfg.VODCacheTTL).
					Dur("interval", interval).
					Msg("recording cache eviction cadence exceeds ttl")
				warnedCadenceMismatch = true
			}
		} else {
			warnedCadenceMismatch = false
		}

		s.mu.RLock()
		vodMgr := s.vodManager
		s.mu.RUnlock()

		excludedPaths := make(map[string]struct{})
		if vodMgr != nil {
			for _, jobID := range vodMgr.ActiveJobIDs() {
				excludedPaths[jobID] = struct{}{}
			}
		}

		res, err := vod.EvictRecordingCacheWithExclusions(
			cfg.HLS.Root,
			cfg.VODCacheTTL,
			cfg.VODCacheMaxEntries,
			vod.RealClock{},
			excludedPaths,
		)
		if err != nil {
			log.L().Error().Err(err).Msg("recording cache eviction failed")
			return
		}

		metrics.SetRecordingCacheEntries(res.Entries)
		metrics.AddVODCacheEvicted(metrics.CacheEvictReasonTTL, res.EvictedTTL)
		metrics.AddVODCacheEvicted(metrics.CacheEvictReasonMaxEntries, res.EvictedMaxEntries)
		if res.Errors > 0 {
			metrics.IncVODCacheEvictionErrors()
			log.L().Warn().Int("errors", res.Errors).Msg("recording cache eviction completed with errors")
		}

		if vodMgr != nil {
			pruned := vodMgr.PruneMetadata(time.Now(), cfg.VODCacheTTL, cfg.VODCacheMaxEntries)
			metrics.AddVODMetadataPruned(metrics.CacheEvictReasonTTL, pruned.RemovedTTL)
			metrics.AddVODMetadataPruned(metrics.CacheEvictReasonMaxEntries, pruned.RemovedMaxEntries)
			if pruned.RemovedTTL+pruned.RemovedMaxEntries > 0 {
				log.L().Info().
					Int("removed_ttl", pruned.RemovedTTL).
					Int("removed_max_entries", pruned.RemovedMaxEntries).
					Int("remaining", pruned.Remaining).
					Msg("recording metadata cache pruned")
			}
		}
	}

	runOnce()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runOnce()
		}
	}
}

// SetRuntimeContext binds runtime workers to the provided root context.
func (s *Server) SetRuntimeContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("runtime context is nil")
	}

	s.mu.Lock()
	if s.runtimeCancel != nil {
		s.runtimeCancel()
	}
	runtimeCtx, runtimeCancel := context.WithCancel(ctx)
	s.runtimeCtx, s.runtimeCancel = runtimeCtx, runtimeCancel
	hostPressureMonitor := s.hostPressureMonitor
	librarySvc := s.libraryService
	s.mu.Unlock()

	if librarySvc != nil {
		if err := librarySvc.InitializeRoots(runtimeCtx); err != nil {
			runtimeCancel()
			s.mu.Lock()
			s.runtimeCancel = nil
			s.runtimeCtx = nil
			s.mu.Unlock()
			return fmt.Errorf("initialize library roots: %w", err)
		}
	}
	admissionmonitor.StartCPUSampler(runtimeCtx, hostPressureMonitor, 0, nil)
	return nil
}

// Shutdown stops v3 background workers and closes owned resources.
func (s *Server) Shutdown(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("shutdown context is nil")
	}

	s.mu.Lock()
	runtimeCancel := s.runtimeCancel
	s.runtimeCancel = nil
	s.runtimeCtx = nil
	vodMgr := s.vodManager
	librarySvc := s.libraryService
	s.mu.Unlock()

	if runtimeCancel != nil {
		runtimeCancel()
	}

	var errs []error
	if vodMgr != nil {
		if err := vodMgr.ShutdownContext(ctx); err != nil {
			errs = append(errs, fmt.Errorf("vod manager shutdown: %w", err))
		}
	}
	if librarySvc != nil {
		if store := librarySvc.GetStore(); store != nil {
			if err := store.Close(); err != nil {
				errs = append(errs, fmt.Errorf("library store close: %w", err))
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("v3 shutdown errors: %w", errors.Join(errs...))
	}
	return nil
}

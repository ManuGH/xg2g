package v3

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	admissionmonitor "github.com/ManuGH/xg2g/internal/admission"
	"github.com/ManuGH/xg2g/internal/config"
	v3recordings "github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
	"github.com/ManuGH/xg2g/internal/control/playback"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/metrics"
	platformpaths "github.com/ManuGH/xg2g/internal/platform/paths"
)

// StartMonitor begins the background storage health checks.
func (s *Server) StartMonitor(ctx context.Context) {
	s.monitorMu.Lock()
	defer s.monitorMu.Unlock()

	if s.storageMonitor != nil && !s.monitorStarted {
		s.monitorStarted = true
		go s.storageMonitor.Start(ctx, 5*time.Minute, s)
		log.L().Info().Msg("storage_monitor: background loop started")
	}
}

// StartAutoPrepareWorker starts a background task to pre-package completed recordings to SSD cache.
func (s *Server) StartAutoPrepareWorker(ctx context.Context) {
	go s.startAutoPrepareWorkerLoop(ctx)
}

func (s *Server) startAutoPrepareWorkerLoop(ctx context.Context) {
	enabled := config.ParseBool("XG2G_DVR_AUTOPREPARE", true)
	if !enabled {
		log.L().Info().Msg("dvr_autoprepare: worker disabled by configuration (XG2G_DVR_AUTOPREPARE=false)")
		return
	}

	adapter := &serverAutoPrepareAdapter{s: s}
	worker := recservice.NewAutoPrepareWorker(recservice.AutoPrepareConfig{
		Enabled:       true,
		Interval:      60 * time.Second,
		MaxConcurrent: 1,
	}, adapter, adapter, s.vodManager)

	worker.RunLoop(ctx)
}

type serverAutoPrepareAdapter struct {
	s *Server
}

func (a *serverAutoPrepareAdapter) ResolvePlayback(ctx context.Context, recordingID, profile string) (recservice.PlaybackResolution, error) {
	svc := a.s.RecordingsService()
	if svc == nil {
		return recservice.PlaybackResolution{}, errors.New("recordings service unavailable")
	}
	return svc.ResolvePlayback(ctx, recordingID, profile)
}

func (a *serverAutoPrepareAdapter) List(ctx context.Context, in recservice.ListInput) (recservice.ListResult, error) {
	svc := a.s.RecordingsService()
	if svc == nil {
		return recservice.ListResult{}, errors.New("recordings service unavailable")
	}
	return svc.List(ctx, in)
}

func (a *serverAutoPrepareAdapter) GetPlaybackInfo(ctx context.Context, in recservice.PlaybackInfoInput) (recservice.PlaybackInfoResult, error) {
	svc := a.s.RecordingsService()
	if svc == nil {
		return recservice.PlaybackInfoResult{}, errors.New("recordings service unavailable")
	}
	return svc.GetPlaybackInfo(ctx, in)
}

func (a *serverAutoPrepareAdapter) GetStatus(ctx context.Context, in recservice.StatusInput) (recservice.StatusResult, error) {
	svc := a.s.RecordingsService()
	if svc == nil {
		return recservice.StatusResult{}, errors.New("recordings service unavailable")
	}
	return svc.GetStatus(ctx, in)
}

func (a *serverAutoPrepareAdapter) GetMediaTruth(ctx context.Context, recordingID string) (playback.MediaTruth, error) {
	svc := a.s.RecordingsService()
	if svc == nil {
		return playback.MediaTruth{}, errors.New("recordings service unavailable")
	}
	return svc.GetMediaTruth(ctx, recordingID)
}

func (a *serverAutoPrepareAdapter) Stream(ctx context.Context, in recservice.StreamInput) (recservice.StreamResult, error) {
	svc := a.s.RecordingsService()
	if svc == nil {
		return recservice.StreamResult{}, errors.New("recordings service unavailable")
	}
	return svc.Stream(ctx, in)
}

func (a *serverAutoPrepareAdapter) Delete(ctx context.Context, in recservice.DeleteInput) (recservice.DeleteResult, error) {
	svc := a.s.RecordingsService()
	if svc == nil {
		return recservice.DeleteResult{}, errors.New("recordings service unavailable")
	}
	return svc.Delete(ctx, in)
}

func (a *serverAutoPrepareAdapter) EnsurePrepared(ctx context.Context, recordingID string) error {
	art := a.s.ArtifactsResolver()
	if art == nil {
		return errors.New("artifacts resolver unavailable")
	}
	processor := a.s.recordingsProcessor()
	if processor != nil {
		reqID := recordingID
		if !recservice.ValidRecordingID(reqID) {
			reqID = recservice.EncodeRecordingID(reqID)
		}
		res, pErr := processor.ResolvePlaybackInfo(ctx, v3recordings.PlaybackInfoRequest{
			SubjectID:   reqID,
			SubjectKind: v3recordings.PlaybackSubjectRecording,
			APIVersion:  "v3.1",
			SchemaType:  "compact",
		})
		if pErr == nil && res.Decision != nil && res.Decision.TargetProfile != nil {
			return art.EnsurePreparedWithTarget(ctx, reqID, res.Decision.TargetProfile)
		}
		if pErr != nil && pErr.Kind == v3recordings.PlaybackInfoErrorPreparing {
			log.L().Debug().Str("recordingID", recordingID).Msg("dvr_autoprepare: recording is currently being probed; deferring prepare")
			return nil
		}
	}
	return art.EnsurePrepared(ctx, recordingID)
}

// StartRecordingCacheEvicter starts a background task to clean up old recording cache entries.
func (s *Server) StartRecordingCacheEvicter(ctx context.Context) {
	go s.startRecordingCacheEvicterLoop(ctx)
}

// startRecordingCacheEvicterLoop runs the eviction ticker loop in a background goroutine.
func (s *Server) startRecordingCacheEvicterLoop(ctx context.Context) {
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
			cacheRoot := platformpaths.RecordingArtifactsRoot(cfg.HLS.Root)
			for _, jobID := range vodMgr.ActiveJobIDs() {
				excludedPaths[filepath.Join(cacheRoot, jobID)] = struct{}{}
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
	worker := s.plannerShadowWorker
	s.mu.Unlock()

	if worker != nil {
		worker.Start(runtimeCtx)
	}

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
	s.mu.Lock()
	vodMgr := s.vodManager
	s.mu.Unlock()
	if vodMgr != nil {
		vodMgr.StartProberPool(runtimeCtx)
	}
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
	worker := s.plannerShadowWorker
	s.mu.Unlock()

	if runtimeCancel != nil {
		runtimeCancel()
	}

	var errs []error
	if worker != nil {
		if err := worker.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("planner shadow worker close: %w", err))
		}
	}
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

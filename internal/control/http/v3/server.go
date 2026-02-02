// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/channels"
	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/http/v3/recordings/artifacts"
	"github.com/ManuGH/xg2g/internal/control/read"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"

	"github.com/ManuGH/xg2g/internal/control/admission"
	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/dvr"
	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/ManuGH/xg2g/internal/health"
	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/ManuGH/xg2g/internal/library"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/ManuGH/xg2g/internal/pipeline/resume"
	recinfra "github.com/ManuGH/xg2g/internal/recordings"
	"golang.org/x/sync/singleflight"
)

type Server struct {
	mu sync.RWMutex

	// Shared State & Configuration
	cfg       config.AppConfig
	snap      config.Snapshot
	status    jobs.Status
	startTime time.Time

	// Core Components
	v3Bus               bus.Bus
	v3Store             store.StateStore
	resumeStore         resume.Store
	v3Scan              ChannelScanner
	owiFactory          receiverControlFactory // Factory for creating OpenWebIF clients (injectable for tests)
	recordingPathMapper *recinfra.PathMapper
	channelManager      *channels.Manager
	seriesManager       *dvr.Manager
	seriesEngine        *dvr.SeriesEngine
	vodManager          *vod.Manager
	resolver            recservice.Resolver // Strict V4 Resolver (Domain)
	artifacts           artifacts.Resolver
	epgCache            *epg.TV // EPG Cache reference
	owiClient           *openwebif.Client
	owiEpoch            uint64
	configManager       *config.Manager
	configMu            sync.Mutex // Serializes configuration updates
	epgCacheTime        time.Time
	epgCacheMTime       time.Time
	epgSfg              singleflight.Group
	receiverSfg         singleflight.Group
	libraryService      *library.Service // Media library per ADR-ENG-002
	admission           *admission.Controller
	admissionState      AdmissionState

	// Lifecycle
	requestShutdown   func(context.Context) error
	preflightProvider PreflightProvider
	healthManager     *health.Manager
	logSource         interface{ GetRecentLogs() []log.LogEntry }
	scanSource        ScanSource
	dvrSource         RecordingStatusProvider
	servicesSource    ServiceStateReader
	timersSource      TimerReader
	epgSource         read.EpgSource
	recordingsService recservice.Service
	storageMonitor    *StorageMonitor
	monitorStarted    bool
	monitorMu         sync.Mutex

	// Middlewares (injectable for tests)
	AuthMiddlewareOverride func(http.Handler) http.Handler
}

// NewServer creates a new implemented v3 server.
func NewServer(cfg config.AppConfig, cfgMgr *config.Manager, rootCancel context.CancelFunc) *Server {
	// Initialize library service if enabled (Phase 0 per ADR-ENG-002)
	var librarySvc *library.Service
	if cfg.Library.Enabled && len(cfg.Library.Roots) > 0 {
		// Convert config roots to library roots
		var libraryRoots []library.RootConfig
		for _, r := range cfg.Library.Roots {
			libraryRoots = append(libraryRoots, library.RootConfig{
				ID:         r.ID,
				Path:       r.Path,
				Type:       r.Type,
				MaxDepth:   r.MaxDepth,
				IncludeExt: r.IncludeExt,
			})
		}

		store, err := library.NewStore(cfg.Library.DBPath)
		if err != nil {
			log.L().Error().Err(err).Msg("failed to initialize library store")
		} else {
			librarySvc = library.NewService(libraryRoots, store)
			log.L().Info().Int("roots", len(libraryRoots)).Msg("library service initialized")
		}
	}

	s := &Server{
		cfg:            cfg,
		configManager:  cfgMgr,
		startTime:      time.Now(),
		libraryService: librarySvc,
		storageMonitor: NewStorageMonitor(),
		admission:      admission.NewController(cfg),
		// owiFactory defaults to nil (uses newOpenWebIFClient in prod)
	}
	s.epgSource = &epgAdapter{s}
	return s
}

// LibraryService returns the underlying library service.
func (s *Server) LibraryService() *library.Service {
	return s.libraryService
}

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

		res, err := vod.EvictRecordingCache(cfg.HLS.Root, cfg.VODCacheTTL, cfg.VODCacheMaxEntries, vod.RealClock{})
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

		s.mu.RLock()
		vodMgr := s.vodManager
		s.mu.RUnlock()
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

// SetResolver sets the V4 resolver used by GetRecordingPlaybackInfo.
func (s *Server) SetResolver(r recservice.Resolver) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolver = r
}

// SetRecordingsService sets the recordings service (for tests).
func (s *Server) SetRecordingsService(svc recservice.Service) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordingsService = svc
}

// SetAdmission sets the resource monitor for admission control.
// SetAdmission sets the controller for admission control.
func (s *Server) SetAdmission(ctrl *admission.Controller) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.admission = ctrl
}

// SetShutdownHandler sets the function to call for graceful shutdown.
func (s *Server) SetShutdownHandler(fn func(context.Context) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requestShutdown = fn
}

// authMiddleware is the default authentication middleware.
func (s *Server) authMiddleware(h http.Handler) http.Handler {
	if s.AuthMiddlewareOverride != nil {
		return s.AuthMiddlewareOverride(h)
	}
	return s.authMiddlewareImpl(h)
}

// UpdateConfig updates the internal configuration snapshot.
func (s *Server) UpdateConfig(cfg config.AppConfig, snap config.Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = cfg
	s.snap = snap
	s.owiEpoch++ // Invalidate cached OWI client
}

// UpdateStatus updates the internal status snapshot.
func (s *Server) UpdateStatus(st jobs.Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = st
}

// SetPreflightCheck sets the source availability validator.
func (s *Server) SetPreflightCheck(fn PreflightProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.preflightProvider = fn
}

// SetDependencies injects shared services into the handler.
func (s *Server) SetDependencies(
	bus bus.Bus,
	store store.StateStore,
	resume resume.Store,
	scan ChannelScanner,
	rpm *recinfra.PathMapper,
	cm *channels.Manager,
	sm *dvr.Manager,
	se *dvr.SeriesEngine,
	vm *vod.Manager,
	epg *epg.TV,

	hm *health.Manager,
	ls interface{ GetRecentLogs() []log.LogEntry },
	ss ScanSource,
	ds RecordingStatusProvider,
	svs ServiceStateReader,
	ts TimerReader,
	recSvc recservice.Service,
	requestShutdown func(context.Context) error,
	preflightProvider PreflightProvider,
) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !isNil(bus) {
		s.v3Bus = bus
	} else {
		s.v3Bus = nil
	}

	if !isNil(store) {
		s.v3Store = store
	} else {
		s.v3Store = nil
	}

	if !isNil(resume) {
		s.resumeStore = resume
	} else {
		s.resumeStore = nil
	}

	if !isNil(scan) {
		s.v3Scan = scan
	} else {
		s.v3Scan = nil
	}

	if !isNil(ss) {
		s.scanSource = ss
	} else {
		s.scanSource = nil
	}

	if !isNil(ds) {
		s.dvrSource = ds
	} else {
		s.dvrSource = nil
	}

	if !isNil(svs) {
		s.servicesSource = svs
	} else {
		s.servicesSource = nil
	}

	if !isNil(ts) {
		s.timersSource = ts
	} else {
		s.timersSource = nil
	}

	if !isNil(rpm) {
		s.recordingPathMapper = rpm
	} else {
		s.recordingPathMapper = nil
	}

	if !isNil(cm) {
		s.channelManager = cm
	} else {
		s.channelManager = nil
	}

	if !isNil(sm) {
		s.seriesManager = sm
	} else {
		s.seriesManager = nil
	}

	if !isNil(se) {
		s.seriesEngine = se
	} else {
		s.seriesEngine = nil
	}

	if !isNil(vm) {
		s.vodManager = vm
		// Start the background prober pool (Phase 2 compliance)
		s.vodManager.StartProberPool(context.Background())
		// PR3: Initialize Artifacts Resolver
		s.artifacts = artifacts.New(&s.cfg, vm, s.recordingPathMapper)
	} else {
		s.vodManager = nil
		s.artifacts = nil
	}

	if !isNil(epg) {
		s.epgCache = epg
	} else {
		s.epgCache = nil
	}

	if !isNil(hm) {
		s.healthManager = hm
	} else {
		s.healthManager = nil
	}

	if !isNil(ls) {
		s.logSource = ls
	} else {
		s.logSource = nil
	}

	if !isNil(requestShutdown) {
		s.requestShutdown = requestShutdown
	} else {
		s.requestShutdown = nil
	}

	if !isNil(preflightProvider) {
		s.preflightProvider = preflightProvider
	} else {
		s.preflightProvider = nil
	}

	if !isNil(recSvc) {
		s.recordingsService = recSvc
	} else {
		s.recordingsService = nil
	}

	// Initialize Admission State Source (Store-backed)
	s.admissionState = &storeAdmissionState{
		store:      store,
		tunerCount: len(s.cfg.Engine.TunerSlots),
	}
}

// GetConfig returns a copy of the current config.
func (s *Server) GetConfig() config.AppConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// GetStatus returns the current status.
func (s *Server) GetStatus() jobs.Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

func (s *Server) dataFilePath(rel string) (string, error) {
	clean := filepath.Clean(rel)
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("data file path must be relative: %s", rel)
	}
	if strings.Contains(clean, "..") {
		return "", fmt.Errorf("data file path contains traversal: %s", rel)
	}

	s.mu.RLock()
	dataDir := s.cfg.DataDir
	s.mu.RUnlock()

	root, err := filepath.Abs(dataDir)
	if err != nil {
		return "", fmt.Errorf("resolve data directory: %w", err)
	}

	full := filepath.Join(root, clean)
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		resolvedRoot = root
	}

	resolved := full
	if info, statErr := os.Stat(full); statErr == nil {
		if info.IsDir() {
			return "", fmt.Errorf("data file path points to directory: %s", rel)
		}
		if resolvedPath, evalErr := filepath.EvalSymlinks(full); evalErr == nil {
			resolved = resolvedPath
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", fmt.Errorf("stat data file: %w", statErr)
	} else {
		// File might be generated later; still ensure parent directories stay within root.
		dir := filepath.Dir(full)
		if _, dirErr := os.Stat(dir); dirErr == nil {
			if realDir, evalErr := filepath.EvalSymlinks(dir); evalErr == nil {
				resolved = filepath.Join(realDir, filepath.Base(full))
			}
		}
	}

	relToRoot, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil {
		return "", fmt.Errorf("resolve relative path: %w", err)
	}
	if strings.HasPrefix(relToRoot, "..") || filepath.IsAbs(relToRoot) {
		return "", fmt.Errorf("data file escapes data directory: %s", rel)
	}

	return resolved, nil
}

// owi returns a ReceiverControl, using the injected factory if present (tests)
// or falling back to the cached production client.
func (s *Server) owi(cfg config.AppConfig, snap config.Snapshot) ReceiverControl {
	if s.owiFactory != nil {
		return s.owiFactory(cfg, snap)
	}
	return s.newOpenWebIFClient(cfg, snap)
}

// newOpenWebIFClient gets or creates a cached client from config
func (s *Server) newOpenWebIFClient(cfg config.AppConfig, snap config.Snapshot) *openwebif.Client {
	// 1. Fast path: Read lock check
	s.mu.RLock()
	cachedClient := s.owiClient
	cachedEpoch := s.owiEpoch
	s.mu.RUnlock()

	// If cached match, assume safe to use (Client is thread-safe)
	if cachedClient != nil && cachedEpoch == snap.Epoch {
		return cachedClient
	}

	// 2. Slow path: Write lock
	s.mu.Lock()
	defer s.mu.Unlock()

	// Double check
	if s.owiClient != nil && s.owiEpoch == snap.Epoch {
		return s.owiClient
	}

	// Rebuild
	log.L().Debug().Uint64("epoch", snap.Epoch).Msg("recreating OpenWebIF client")
	client := openwebif.NewWithPort(cfg.Enigma2.BaseURL, cfg.Enigma2.StreamPort, openwebif.Options{
		Timeout:             cfg.Enigma2.Timeout,
		Username:            cfg.Enigma2.Username,
		Password:            cfg.Enigma2.Password,
		UseWebIFStreams:     cfg.Enigma2.UseWebIFStreams,
		StreamBaseURL:       snap.Runtime.OpenWebIF.StreamBaseURL,
		HTTPMaxConnsPerHost: snap.Runtime.OpenWebIF.HTTPMaxConnsPerHost,
	})

	s.owiClient = client
	s.owiEpoch = snap.Epoch

	return client
}

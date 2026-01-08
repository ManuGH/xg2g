// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/channels"
	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/read"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/dvr"
	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/ManuGH/xg2g/internal/health"
	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/ManuGH/xg2g/internal/library"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/ManuGH/xg2g/internal/pipeline/resume"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	"github.com/ManuGH/xg2g/internal/recordings"
	"github.com/ManuGH/xg2g/internal/vod"
	"golang.org/x/sync/singleflight"
)

// PreflightCheckFunc validates source accessibility before initiating a stream.
type PreflightCheckFunc func(context.Context, string) error

// DvrSource defines the minimal interface required for DVR read operations.
type DvrSource interface {
	GetStatusInfo(ctx context.Context) (*openwebif.StatusInfo, error)
	HasTimerChange(ctx context.Context) bool
}

// ScanSource defines the minimal interface required for scan status.
type ScanSource interface {
	GetStatus() scan.ScanStatus
}

// ServicesSource defines the minimal interface required for service listing.
type ServicesSource interface {
	IsEnabled(id string) bool
}

// TimersSource defines the minimal interface required for timer listing.
type TimersSource interface {
	GetTimers(ctx context.Context) ([]openwebif.Timer, error)
}

// Server implements the v3 API handlers.
// It encapsulates all logic for /api/v3 endpoints.
// Field names are kept consistent with internal/api.Server for seamless migration.
// Scanner abstracts the refresh/scan subsystem for testability.
type scanner interface {
	RunBackground() bool
	GetCapability(serviceRef string) (scan.Capability, bool)
}

// openWebIFClient abstracts OpenWebIF client operations for DVR timers.
// This enables deterministic testing without real receiver dependencies.
// Note: *openwebif.Client satisfies this interface directly.
type openWebIFClient interface {
	GetTimers(ctx context.Context) ([]openwebif.Timer, error)
	AddTimer(ctx context.Context, sRef string, begin, end int64, name, desc string) error
	DeleteTimer(ctx context.Context, sRef string, begin, end int64) error
	UpdateTimer(ctx context.Context, oldSRef string, oldBegin, oldEnd int64, newSRef string, newBegin, newEnd int64, name, description string, enabled bool) error
	HasTimerChange(ctx context.Context) bool
}

// owiFactory creates an openWebIFClient instance.
type owiFactory func(cfg config.AppConfig, snap config.Snapshot) openWebIFClient

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
	v3Scan              scanner
	owiFactory          owiFactory // Factory for creating OpenWebIF clients (injectable for tests)
	recordingPathMapper *recordings.PathMapper
	channelManager      *channels.Manager
	seriesManager       *dvr.Manager
	seriesEngine        *dvr.SeriesEngine
	vodManager          vod.ManagerAPI
	epgCache            *epg.TV // EPG Cache reference
	owiClient           *openwebif.Client
	owiEpoch            uint64
	configManager       *config.Manager
	epgCacheTime        time.Time
	epgCacheMTime       time.Time
	epgSfg              singleflight.Group
	libraryService      *library.Service // Media library per ADR-ENG-002

	// Lifecycle
	requestShutdown func(context.Context) error
	preflightCheck  PreflightCheckFunc
	healthManager   *health.Manager
	logSource       interface{ GetRecentLogs() []log.LogEntry }
	scanSource      ScanSource
	dvrSource       DvrSource
	servicesSource  ServicesSource
	timersSource    TimersSource
	epgSource       read.EpgSource
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
		// owiFactory defaults to nil (uses newOpenWebIFClient in prod)
	}
	s.epgSource = &epgSourceWrapper{s}
	return s
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
func (s *Server) SetPreflightCheck(fn PreflightCheckFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.preflightCheck = fn
}

// SetDependencies injects shared services into the handler.
func (s *Server) SetDependencies(
	bus bus.Bus,
	store store.StateStore,
	resume resume.Store,
	scan scanner,
	rpm *recordings.PathMapper,
	cm *channels.Manager,
	sm *dvr.Manager,
	se *dvr.SeriesEngine,
	vm vod.ManagerAPI,
	epg *epg.TV,
	hm *health.Manager,
	ls interface{ GetRecentLogs() []log.LogEntry },
	ss ScanSource,
	ds DvrSource,
	svs ServicesSource,
	ts TimersSource,
	requestShutdown func(context.Context) error,
	preflightCheck PreflightCheckFunc,
) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.v3Bus = bus
	s.v3Store = store
	s.resumeStore = resume
	s.v3Scan = scan
	s.recordingPathMapper = rpm
	s.channelManager = cm
	s.seriesManager = sm
	s.seriesEngine = se
	s.vodManager = vm
	s.epgCache = epg
	s.healthManager = hm
	s.logSource = ls
	s.scanSource = ss
	s.dvrSource = ds
	s.servicesSource = svs
	s.timersSource = ts
	s.requestShutdown = requestShutdown
	s.preflightCheck = preflightCheck
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

// owi returns an OpenWebIF client, using the injected factory if present (tests)
// or falling back to the cached production client.
func (s *Server) owi(cfg config.AppConfig, snap config.Snapshot) openWebIFClient {
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
	enableHTTP2 := snap.Runtime.OpenWebIF.HTTPEnableHTTP2
	client := openwebif.NewWithPort(cfg.Enigma2.BaseURL, cfg.Enigma2.StreamPort, openwebif.Options{
		Timeout:                 cfg.Enigma2.Timeout,
		Username:                cfg.Enigma2.Username,
		Password:                cfg.Enigma2.Password,
		UseWebIFStreams:         cfg.Enigma2.UseWebIFStreams,
		StreamBaseURL:           snap.Runtime.OpenWebIF.StreamBaseURL,
		HTTPMaxIdleConns:        snap.Runtime.OpenWebIF.HTTPMaxIdleConns,
		HTTPMaxIdleConnsPerHost: snap.Runtime.OpenWebIF.HTTPMaxIdleConnsPerHost,
		HTTPMaxConnsPerHost:     snap.Runtime.OpenWebIF.HTTPMaxConnsPerHost,
		HTTPIdleTimeout:         snap.Runtime.OpenWebIF.HTTPIdleTimeout,
		HTTPEnableHTTP2:         &enableHTTP2,
	})

	s.owiClient = client
	s.owiEpoch = snap.Epoch

	return client
}

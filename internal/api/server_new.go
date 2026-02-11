// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/channels"
	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/admission"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/ManuGH/xg2g/internal/control/vod/preflight"
	"github.com/ManuGH/xg2g/internal/dvr"
	"github.com/ManuGH/xg2g/internal/hdhr"
	"github.com/ManuGH/xg2g/internal/health"
	infra "github.com/ManuGH/xg2g/internal/infra/ffmpeg"
	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/recordings"
	"github.com/ManuGH/xg2g/internal/resilience"
)

// New creates and initializes a new HTTP API server.
func New(cfg config.AppConfig, cfgMgr *config.Manager, opts ...ServerOption) (*Server, error) {
	// 1. Initialized root context for server lifecycle (MUST be before v3Handler)
	rootCtx, rootCancel := context.WithCancel(context.Background())

	// Initialize channel manager
	cm := channels.NewManager(cfg.DataDir)
	if err := cm.Load(); err != nil {
		log.L().Error().Err(err).Msg("failed to load channel states")
	}

	// Initialize Series Manager (DVR v2)
	sm := dvr.NewManager(cfg.DataDir)
	if err := sm.Load(); err != nil {
		log.L().Error().Err(err).Msg("failed to load series rules")
	}

	env, err := config.ReadOSRuntimeEnv()
	if err != nil {
		log.L().Warn().Err(err).Msg("failed to read runtime environment, using defaults")
		env = config.DefaultEnv()
	}
	snap := config.BuildSnapshot(cfg, env)

	s := &Server{
		cfg:                 cfg,
		configManager:       cfgMgr,
		rootCtx:             rootCtx,
		rootCancel:          rootCancel,
		snap:                snap,
		channelManager:      cm,
		seriesManager:       sm,
		recordingPathMapper: recordings.NewPathMapper(cfg.RecordingPathMappings),
		status: jobs.Status{
			Version: cfg.Version, // Initialize version from config
		},
		startTime:         time.Now(),
		piconSemaphore:    make(chan struct{}, 50),
		preflightProvider: preflight.NewHTTPPreflightProvider(nil, cfg.Enigma2.PreflightTimeout),
		v3Factory:         v3.NewServer, // Default factory
	}

	// Initialize VOD Manager with error handling
	vodMgr, err := vod.NewManager(infra.NewExecutor(cfg.FFmpeg.Bin, *log.L(), cfg.Timeouts.TranscodeStart, cfg.Timeouts.TranscodeNoProgress), infra.NewProber(cfg.FFmpeg.FFprobeBin), recordings.NewPathMapper(cfg.RecordingPathMappings))
	if err != nil {
		rootCancel()
		return nil, fmt.Errorf("initialize vod manager: %w", err)
	}
	s.vodManager = vodMgr

	for _, opt := range opts {
		opt(s)
	}

	// v3Handler expects a valid root cancel function
	if cfgMgr == nil {
		rootCancel()
		return nil, fmt.Errorf("config manager is required for API server initialization")
	}
	s.v3Handler = s.v3Factory(cfg, cfgMgr, s.rootCancel)
	// Initialize v3Handler with current snapshot to ensure Runtime settings are available immediately
	s.v3Handler.UpdateConfig(cfg, s.snap)

	// P4: Wire NEW V4 Resolver (recordings package)
	// This is the canonical resolver used by GetRecordingPlaybackInfo
	var resolverOpts recservice.ResolverOptions
	if libSvc := s.v3Handler.LibraryService(); libSvc != nil {
		resolverOpts.DurationStore = recservice.NewLibraryDurationStore(libSvc.GetStore())
		resolverOpts.PathResolver = recservice.NewLibraryPathResolver(s.recordingPathMapper, libSvc.GetConfigs())
	}
	v4Resolver, err := recservice.NewResolver(&cfg, s.vodManager, resolverOpts)
	if err != nil {
		rootCancel()
		return nil, fmt.Errorf("initialize recordings resolver: %w", err)
	}

	// Create OpenWebIF client using configured BaseURL and credentials
	// We use the same configuration logic as the health checker
	s.owiClient = openwebif.NewWithPort(cfg.Enigma2.BaseURL, 0, openwebif.Options{
		Timeout:  cfg.Enigma2.Timeout,
		Username: cfg.Enigma2.Username,
		Password: cfg.Enigma2.Password,
		// Using defaults for other options as they are not currently exposed in AppConfig for main client
	})

	// Create infrastructure adapters for domain service
	owiAdapter := v3.NewOWIAdapter(s.owiClient)
	resumeAdapter := v3.NewResumeAdapter(s.resumeStore)

	// Create domain RecordingsService
	// Note: v4Resolver here is a domain resolver because of the v3.Server.SetResolver signature change
	recSvc, err := recservice.NewService(&cfg, s.vodManager, v4Resolver, owiAdapter, resumeAdapter, v4Resolver)
	if err != nil {
		rootCancel()
		return nil, fmt.Errorf("initialize recordings service: %w", err)
	}
	s.recordingsService = recSvc

	s.v3Handler.SetResolver(v4Resolver)

	// Initialize Series Engine
	// Server (s) implements EpgProvider interface via GetEvents method
	s.seriesEngine = dvr.NewSeriesEngine(cfg, sm, func() dvr.OWIClient {
		return openwebif.New(cfg.Enigma2.BaseURL)
	})

	// Default refresh function
	s.refreshFn = jobs.Refresh
	// Initialize a conservative default circuit breaker (3 failures -> 30s open)
	s.cb = resilience.NewCircuitBreaker("v2-api", 5, 10, 60*time.Second, 30*time.Second, resilience.WithPanicRecovery(true))

	// Initialize health manager
	s.healthManager = health.NewManager(cfg.Version)

	// P10: Wire runtime-provided v3 dependencies and admission via a single DI entrypoint.
	// Initialize with conservative defaults (10 concurrent transcodes, 10 CPU-heavy ops)
	// In the future this should come from config.
	adm := admission.NewController(cfg)
	s.WireV3Runtime(s.v3Bus, s.v3Store, s.resumeStore, s.v3Scan, adm)

	// Initialize HDHomeRun emulation if enabled
	logger := log.WithComponent("api")
	// Map config.AppConfig.HDHR -> hdhr.Config
	hdhrEnabled := false
	if cfg.HDHR.Enabled != nil {
		hdhrEnabled = *cfg.HDHR.Enabled
	}

	if hdhrEnabled {
		// Populate HDHR config from AppConfig
		tunerCount := 4
		if cfg.HDHR.TunerCount != nil {
			tunerCount = *cfg.HDHR.TunerCount
		}
		plexForceHLS := false
		if cfg.HDHR.PlexForceHLS != nil {
			plexForceHLS = *cfg.HDHR.PlexForceHLS
		}

		hdhrConf := hdhr.Config{
			Enabled:          hdhrEnabled,
			DeviceID:         cfg.HDHR.DeviceID,
			FriendlyName:     cfg.HDHR.FriendlyName,
			ModelName:        cfg.HDHR.ModelNumber,
			FirmwareName:     cfg.HDHR.FirmwareName,
			BaseURL:          cfg.HDHR.BaseURL,
			TunerCount:       tunerCount,
			PlexForceHLS:     plexForceHLS,
			PlaylistFilename: s.snap.Runtime.PlaylistFilename, // Runtime snapshot has the correct filename
			DataDir:          cfg.DataDir,
			Logger:           logger,
		}

		s.hdhr = hdhr.NewServer(hdhrConf, cm)
		logger.Info().
			Bool("hdhr_enabled", true).
			Str("device_id", hdhrConf.DeviceID).
			Msg("HDHomeRun emulation enabled")
	}

	// Register health checkers
	playlistName := s.snap.Runtime.PlaylistFilename
	playlistPath := filepath.Join(cfg.DataDir, playlistName)
	s.healthManager.RegisterChecker(health.NewFileChecker("playlist", playlistPath))

	if strings.TrimSpace(cfg.XMLTVPath) != "" {
		xmltvPath := filepath.Join(cfg.DataDir, cfg.XMLTVPath)
		s.healthManager.RegisterChecker(health.NewFileChecker("xmltv", xmltvPath))
	}

	s.healthManager.RegisterChecker(health.NewLastRunChecker(func() (time.Time, string) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return s.status.LastRun, s.status.Error
	}))

	// Receiver connectivity check
	s.healthManager.RegisterChecker(health.NewReceiverChecker(func(ctx context.Context) error {
		if cfg.Enigma2.BaseURL == "" {
			return fmt.Errorf("receiver not configured")
		}
		// Use client for check if possible, or keep simple HTTP
		// Use the context provided by health manager (which includes timeout)
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, cfg.Enigma2.BaseURL, nil)
		if err != nil {
			return err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer func() {
			_ = resp.Body.Close()
		}()
		if resp.StatusCode >= 400 {
			return fmt.Errorf("receiver returned HTTP %d", resp.StatusCode)
		}
		return nil
	}))

	// Channels loaded check
	s.healthManager.RegisterChecker(health.NewChannelsChecker(func() int {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return s.status.Channels
	}))

	// EPG status check
	s.healthManager.RegisterChecker(health.NewEPGChecker(func() (bool, time.Time) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		loaded := s.status.EPGProgrammes > 0
		return loaded, s.status.LastRun
	}))

	return s, nil
}

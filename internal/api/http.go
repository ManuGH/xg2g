// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Package api provides HTTP server functionality for the xg2g application.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/admission"
	"github.com/ManuGH/xg2g/internal/channels"
	"github.com/ManuGH/xg2g/internal/config"
	controlhttp "github.com/ManuGH/xg2g/internal/control/http"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	"github.com/ManuGH/xg2g/internal/control/middleware"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/ManuGH/xg2g/internal/control/vod/preflight"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/dvr"
	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/ManuGH/xg2g/internal/hdhr"
	"github.com/ManuGH/xg2g/internal/health"
	infra "github.com/ManuGH/xg2g/internal/infra/ffmpeg"
	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/ManuGH/xg2g/internal/library"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/ManuGH/xg2g/internal/pipeline/resume"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	"github.com/ManuGH/xg2g/internal/platform/paths"
	"github.com/ManuGH/xg2g/internal/recordings"
	"github.com/go-chi/chi/v5"

	"github.com/ManuGH/xg2g/internal/resilience"
)

// Server represents the HTTP API server for xg2g.
type Server struct {
	mu             sync.RWMutex
	refreshing     atomic.Bool // serialize refreshes via atomic flag
	cfg            config.AppConfig
	snap           config.Snapshot
	configHolder   ConfigHolder
	status         jobs.Status
	cb             *resilience.CircuitBreaker
	hdhr           *hdhr.Server      // HDHomeRun emulation server
	auditLogger    AuditLogger       // Optional: for audit logging
	healthManager  *health.Manager   // Health and readiness checks
	channelManager *channels.Manager // Channel management
	configManager  *config.Manager   // Config operations
	seriesManager  *dvr.Manager      // Series Recording Rules (DVR v2)
	seriesEngine   *dvr.SeriesEngine // Series Recording Engine (DVR v2.1)

	// refreshFn allows tests to stub the refresh operation; defaults to jobs.Refresh
	refreshFn      func(context.Context, config.Snapshot) (*jobs.Status, error)
	startTime      time.Time
	piconSemaphore chan struct{} // Limit concurrent upstream picon fetches

	// EPG Cache (P1 Performance Fix)
	epgCache *epg.TV

	// Phase B: SOA Refactor - VOD Manager
	vodManager *vod.Manager

	// OpenWebIF Client Cache (P1 Performance Fix)
	owiClient *openwebif.Client // In-memory cache for openWebIF client

	// v3 Integration
	v3Handler         *v3.Server
	v3Bus             bus.Bus
	v3Store           store.StateStore
	resumeStore       resume.Store
	v3Scan            *scan.Manager
	recordingsService recservice.Service

	// Recording Playback Path Mapper
	recordingPathMapper *recordings.PathMapper

	// P8.2: Hardening & Test Stability
	preflightProvider v3.PreflightProvider

	// P9: Safety & Shutdown
	rootCtx    context.Context
	rootCancel context.CancelFunc
	shutdownFn func(context.Context) error
	started    atomic.Bool // P10: Lifecycle Invariant (Deliverable #4)

	// Dependency Injection (Internal)
	v3Factory func(config.AppConfig, *config.Manager, context.CancelFunc) *v3.Server
}

// AuditLogger interface for audit logging (optional).
type AuditLogger interface {
	ConfigReload(actor, result string, details map[string]string)
	RefreshStart(actor string, bouquets []string)
	RefreshComplete(actor string, channels, bouquets int, durationMS int64)
	RefreshError(actor, reason string)
	AuthSuccess(remoteAddr, endpoint string)
	AuthFailure(remoteAddr, endpoint, reason string)
	AuthMissing(remoteAddr, endpoint string)
	RateLimitExceeded(remoteAddr, endpoint string)
}

// ServerOption allows functional configuration of the Server.
type ServerOption func(*Server)

// WithV3ServerFactory overrides the v3 server implementation (for tests).
func WithV3ServerFactory(f func(config.AppConfig, *config.Manager, context.CancelFunc) *v3.Server) ServerOption {
	return func(s *Server) {
		s.v3Factory = f
	}
}

// ConfigHolder interface allows hot configuration reloading without import cycles.
// Implemented by config.ConfigHolder.
type ConfigHolder interface {
	Current() *config.Snapshot
	Reload(ctx context.Context) error
}

// dataFilePath resolves a relative path inside the configured data directory while
// protecting against path traversal and symlink escapes. The returned path points to
// the real location on disk and is safe to open.
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

// New creates and initializes a new HTTP API server.
func New(cfg config.AppConfig, cfgMgr *config.Manager, opts ...ServerOption) *Server {
	// 1. Initialized root context for server lifecycle (MUST be before v3Handler)
	rootCtx, rootCancel := context.WithCancel(context.Background())

	// Initialize channel manager
	cm := channels.NewManager(cfg.DataDir)
	if err := cm.Load(); err != nil {
		log.L().Error().Err(err).Msg("failed to load channel states")
	}

	// Initialize Trusted Proxies (Global API Config)
	SetTrustedProxies(cfg.TrustedProxies)

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
	vodMgr, err := vod.NewManager(infra.NewExecutor(cfg.FFmpeg.Bin, *log.L()), infra.NewProber(), recordings.NewPathMapper(cfg.RecordingPathMappings))
	if err != nil {
		log.L().Fatal().Err(err).Msg("failed to initialize VOD manager")
	}
	s.vodManager = vodMgr

	for _, opt := range opts {
		opt(s)
	}

	// v3Handler expects a valid root cancel function
	if cfgMgr == nil {
		log.L().Fatal().Msg("config.Manager is required for API server initialization")
	}
	s.v3Handler = s.v3Factory(cfg, cfgMgr, s.rootCancel)
	// Initialize v3Handler with current snapshot to ensure Runtime settings are available immediately
	s.v3Handler.UpdateConfig(cfg, s.snap)
	s.v3Handler.StartMonitor(s.rootCtx)

	// P4: Wire NEW V4 Resolver (recordings package)
	// This is the canonical resolver used by GetRecordingPlaybackInfo
	var resolverOpts recservice.ResolverOptions
	if libSvc := s.v3Handler.LibraryService(); libSvc != nil {
		resolverOpts.DurationStore = recservice.NewLibraryDurationStore(libSvc.GetStore())
		resolverOpts.PathResolver = recservice.NewLibraryPathResolver(s.recordingPathMapper, libSvc.GetConfigs())
	}
	v4Resolver, err := recservice.NewResolver(&cfg, s.vodManager, resolverOpts)
	if err != nil {
		log.L().Fatal().Err(err).Msg("failed to initialize recordings resolver")
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
		log.L().Fatal().Err(err).Msg("failed to initialize recordings service")
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
	s.cb = resilience.NewCircuitBreaker("api_refresh", 3, 30*time.Second, resilience.WithPanicRecovery(true))

	// Initialize health manager
	s.healthManager = health.NewManager(cfg.Version)

	// Wire v3 Handler dependencies
	s.v3Handler.SetDependencies(
		s.v3Bus,
		s.v3Store,
		s.resumeStore,
		s.v3Scan,
		s.recordingPathMapper,
		s.channelManager,
		s.seriesManager,
		s.seriesEngine,
		s.vodManager,
		s.epgCache,
		s.healthManager,
		logSourceWrapper{},
		s.v3Scan,
		&dvrSourceWrapper{s},
		s.channelManager,
		&dvrSourceWrapper{s},
		s.recordingsService,
		s.requestShutdown,
		s.preflightProvider,
	)

	// P10: Wired Admission Control (Deliverable #5)
	// Initialize with conservative defaults (10 concurrent transcodes, 10 CPU-heavy ops)
	// In the future this should come from config.
	adm := admission.NewResourceMonitor(10, 10, 0)
	s.v3Handler.SetAdmission(adm)

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

	return s
}

// Shutdown performs a graceful shutdown of the server.
// P9: Safety & Shutdown
func (s *Server) Shutdown(ctx context.Context) error {
	log.L().Info().Msg("shutting down server")

	// 1. Cancel root context (signals builds to stop)
	if s.rootCancel != nil {
		s.rootCancel()
	}

	// 2. Run final VOD cleanup (kill processes)
	if s.vodManager != nil {
		s.vodManager.CancelAll()
	}
	// Legacy cleanupRecordingBuilds removed.

	// 3. Wait for active builds?
	// We don't have a specific WaitGroup for builds, but cleanup killed the processes.
	// The build goroutines will exit when they see cmd.Wait() returns/error.

	return nil
}

// SetRootContext ties server lifecycle to the provided parent context.
// SetRootContext ties server lifecycle to the provided parent context.
// It replaces any existing root context and cancels the previous one.
// Returns error if called after server usage has begun.
func (s *Server) SetRootContext(ctx context.Context) error {
	if s.started.Load() {
		return fmt.Errorf("cannot SetRootContext after Start")
	}
	if ctx == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rootCancel != nil {
		s.rootCancel()
	}
	s.rootCtx, s.rootCancel = context.WithCancel(ctx)
	return nil
}

// SetShutdownFunc wires a graceful shutdown trigger (daemon-level).
// The function should cancel the daemon root context and/or invoke manager shutdown.
func (s *Server) SetShutdownFunc(fn func(context.Context) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shutdownFn = fn
}

func (s *Server) requestShutdown(ctx context.Context) error {
	s.mu.RLock()
	fn := s.shutdownFn
	s.mu.RUnlock()
	if fn == nil {
		return nil
	}
	return fn(ctx)
}

// GetEvents implements dvr.EpgProvider interface
func (s *Server) GetEvents(from, to time.Time) ([]openwebif.EPGEvent, error) {
	s.mu.RLock()
	cache := s.epgCache
	s.mu.RUnlock()

	if cache == nil {
		return nil, nil // No EPG data
	}

	var events []openwebif.EPGEvent

	// Programs is flat list in epg.TV?
	// Yes: Programs []Programme
	// We need to iterate and convert.

	for _, p := range cache.Programs {
		// Parse times
		// formatXMLTVTime: "20060102150405 -0700"
		start, err := time.Parse("20060102150405 -0700", p.Start)
		if err != nil {
			continue
		}

		// Optimization: Skip if outside window
		if start.After(to) {
			continue
		}

		stop, err := time.Parse("20060102150405 -0700", p.Stop)
		if err != nil {
			// Fallback: 30 mins
			stop = start.Add(30 * time.Minute)
		}

		if stop.Before(from) {
			continue
		}

		// Convert to EPGEvent
		evt := openwebif.EPGEvent{
			Title:       p.Title.Text,
			Description: p.Desc.Text,
			Begin:       start.Unix(),
			Duration:    int64(stop.Sub(start).Seconds()),
			SRef:        p.Channel, // Channel ID in XMLTV is SRef
		}
		events = append(events, evt)
	}

	return events, nil
}

// HealthManager returns the health check manager
func (s *Server) HealthManager() *health.Manager {
	return s.healthManager
}

func (s *Server) GetConfig() config.AppConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// HDHomeRunServer returns the HDHomeRun server instance if enabled
func (s *Server) HDHomeRunServer() *hdhr.Server {
	return s.hdhr
}

// GetSeriesEngine returns the SeriesEngine instance (for scheduler wiring)
func (s *Server) GetSeriesEngine() *dvr.SeriesEngine {
	return s.seriesEngine
}

func (s *Server) routes() http.Handler {
	var trustedProxies []*net.IPNet
	if list := strings.Split(s.cfg.TrustedProxies, ","); len(list) > 0 {
		if tp, err := middleware.ParseCIDRs(list); err == nil {
			trustedProxies = tp
		}
	}

	r := middleware.NewRouter(middleware.StackConfig{
		EnableCORS:           true,
		AllowedOrigins:       s.cfg.AllowedOrigins,
		CORSAllowCredentials: false, // PR3 requirement: hardcoded off

		EnableSecurityHeaders: true,
		CSP:                   middleware.DefaultCSP,
		TrustedProxies:        trustedProxies,

		EnableMetrics:  true,
		TracingService: "xg2g-api",
		EnableLogging:  true,

		EnableRateLimit:    true,
		RateLimitEnabled:   s.cfg.RateLimitEnabled,
		RateLimitGlobalRPS: s.cfg.RateLimitGlobal,
		RateLimitBurst:     s.cfg.RateLimitBurst,
		RateLimitWhitelist: s.cfg.RateLimitWhitelist,
	})

	// 1. PUBLIC Endpoints (No Auth)
	r.Get("/healthz", s.handleHealth)
	r.Get("/readyz", s.handleReady)

	r.Handle("/ui/*", http.StripPrefix("/ui", controlhttp.UIHandler(controlhttp.UIConfig{
		CSP: middleware.DefaultCSP,
	})))

	r.Get("/ui", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusMovedPermanently)
	})

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusTemporaryRedirect)
	})

	// 2. AUTHENTICATED Group (Fail-closed base)
	rAuth := r.With(s.authMiddleware)

	// 3. SCOPED Groups
	rRead := rAuth.With(s.scopeMiddleware(v3.ScopeV3Read))
	rWrite := rAuth.With(s.scopeMiddleware(v3.ScopeV3Write))
	rAdmin := rAuth.With(s.scopeMiddleware(v3.ScopeV3Admin))

	// 4. Admin Operations
	rAdmin.Post("/internal/system/config/reload", http.HandlerFunc(s.handleConfigReload))

	// 5. Setup Validation
	rAuth.Post("/internal/setup/validate", http.HandlerFunc(s.handleSetupValidate))

	// 6. Register Generated API v3 Routes
	// NOTE: HandlerWithOptions creates its own handler stack.
	// We pass rAuth as the BaseRouter to ensure all v3 routes are guarded by auth.
	v3.HandlerWithOptions(s.v3Handler, v3.ChiServerOptions{
		BaseURL:    "/api/v3",
		BaseRouter: rAuth,
		Middlewares: []v3.MiddlewareFunc{
			s.v3Handler.ScopeMiddlewareFromContext,
		},
	})

	// 7. Manual v3 Extensions (Strictly Scoped)
	rRead.Get("/api/v3/vod/{recordingId}", func(w http.ResponseWriter, r *http.Request) {
		recordingId := chi.URLParam(r, "recordingId")
		s.v3Handler.GetRecordingPlaybackInfo(w, r, recordingId)
	})

	rRead.Head("/api/v3/recordings/{recordingId}/stream.mp4", func(w http.ResponseWriter, r *http.Request) {
		recordingId := chi.URLParam(r, "recordingId")
		s.v3Handler.StreamRecordingDirect(w, r, recordingId)
	})

	rWrite.Put("/api/v3/recordings/{recordingId}/resume", s.v3Handler.HandleRecordingResume)
	rWrite.Options("/api/v3/recordings/{recordingId}/resume", s.v3Handler.HandleRecordingResumeOptions)

	// 8. Client Integration (Neutral Shape)
	// Supports DirectPlay decision logic without backend coupling
	rRead.Post("/Items/{itemId}/PlaybackInfo", func(w http.ResponseWriter, r *http.Request) {
		itemId := chi.URLParam(r, "itemId")
		s.v3Handler.PostItemsPlaybackInfo(w, r, itemId)
	})

	// 9. LAN Guard (Restrict discovery/legacy endpoints to private networks)
	// trusted proxies are comma-separated in config
	var proxies []string
	if s.cfg.TrustedProxies != "" {
		proxies = strings.Split(s.cfg.TrustedProxies, ",")
	}
	lanGuard, _ := middleware.NewLANGuard(middleware.LANGuardConfig{
		TrustedProxyCIDRs: proxies,
	})

	// PROTECTED: Discovery / Legacy Endpoints
	r.Group(func(r chi.Router) {
		r.Use(lanGuard.RequireLAN)

		// HDHomeRun emulation endpoints (versionless - hardware emulation protocol)
		if s.hdhr != nil {
			r.Get("/discover.json", s.hdhr.HandleDiscover)
			r.Get("/lineup_status.json", s.hdhr.HandleLineupStatus)
			r.Get("/lineup.json", s.handleLineupJSON)
			r.Post("/lineup.json", s.hdhr.HandleLineupPost)
			r.HandleFunc("/lineup.post", s.hdhr.HandleLineupPost) // supports both GET and POST
			r.Get("/device.xml", s.hdhr.HandleDeviceXML)
		}

		// XMLTV endpoint (versionless - standard format)
		r.Method(http.MethodGet, "/xmltv.xml", http.HandlerFunc(s.handleXMLTV))
		r.Method(http.MethodHead, "/xmltv.xml", http.HandlerFunc(s.handleXMLTV))

		// Internal playlist export
		// Internal playlist export
		// Legacy endpoint: /playlist.m3u (serves the current playlist file, whatever it is)
		r.Get("/playlist.m3u", func(w http.ResponseWriter, r *http.Request) {
			s.mu.RLock()
			cfg := s.cfg
			snap := s.snap
			s.mu.RUnlock()

			playlistPath, err := paths.ValidatePlaylistPath(cfg.DataDir, snap.Runtime.PlaylistFilename)
			if err != nil {
				log.L().Error().Err(err).Msg("playlist path rejected")
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			http.ServeFile(w, r, playlistPath)
		})

		// Modern endpoint: /playlist.m3u8 (sets correct MIME type)
		r.Get("/playlist.m3u8", func(w http.ResponseWriter, r *http.Request) {
			s.mu.RLock()
			cfg := s.cfg
			snap := s.snap
			s.mu.RUnlock()

			playlistPath, err := paths.ValidatePlaylistPath(cfg.DataDir, snap.Runtime.PlaylistFilename)
			if err != nil {
				log.L().Error().Err(err).Msg("playlist path rejected")
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", controlhttp.ContentTypeHLSPlaylist)
			http.ServeFile(w, r, playlistPath)
		})
	})

	// PUBLIC (or Internal Auth): Logo Proxy (Needs access from Players)
	// Some players (esp mobile) might come from outside if strict LAN isn't perfect,
	// but for now we treat logos as discovery assets.
	r.With(lanGuard.RequireLAN).Get("/logos/{ref}.png", s.handlePicons)
	r.With(lanGuard.RequireLAN).Head("/logos/{ref}.png", s.handlePicons)

	// Harden file server: disable directory listing and use a secure handler
	// NOTE: fileserver applies its own allowlist, but we add LAN guard for depth.
	r.With(lanGuard.RequireLAN).Handle("/files/*", http.StripPrefix("/files/", controlhttp.SecureFileServer(s.cfg.DataDir, controlhttp.NewPromFileMetrics())))

	return r
}

// GetStatus returns the current server status (thread-safe)
// This method is exposed for use by versioned API handlers
func (s *Server) GetStatus() jobs.Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

// UpdateStatus updates the server status (thread-safe)
func (s *Server) UpdateStatus(status jobs.Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
}

// SetV3Components configures v3 event bus, store, and scan manager
func (s *Server) SetV3Components(b bus.Bus, st store.StateStore, rs resume.Store, sm *scan.Manager) {
	s.mu.Lock()
	s.v3Bus = b
	s.v3Store = st
	s.resumeStore = rs
	s.v3Scan = sm
	s.mu.Unlock()

	// Update sub-handler dependencies (it maintains its own state)
	if s.v3Handler != nil {
		s.v3Handler.SetDependencies(
			b, st, rs, sm,
			s.recordingPathMapper,
			s.channelManager,
			s.seriesManager,
			s.seriesEngine,
			s.vodManager,
			s.epgCache,
			s.healthManager,
			logSourceWrapper{},
			s.v3Scan,
			&dvrSourceWrapper{s},
			s.channelManager,
			&dvrSourceWrapper{s},
			s.recordingsService,
			s.requestShutdown,
			s.preflightProvider,
		)
	}
}

// LibraryService returns the underlying library service from v3 handler.
func (s *Server) LibraryService() *library.Service {
	if s.v3Handler != nil {
		return s.v3Handler.LibraryService()
	}
	return nil
}

// VODManager returns the underlying VOD manager.
func (s *Server) VODManager() *vod.Manager {
	return s.vodManager
}

// SetVODProber injects a custom prober into the VOD manager for testing.
func (s *Server) SetVODProber(p vod.Prober) {
	if s.vodManager != nil {
		s.vodManager.SetProber(p)
	}
}

// SetResolver injects a resolver into the v3 handler (tests).
func (s *Server) SetResolver(r recservice.Resolver) {
	if s.v3Handler != nil {
		s.v3Handler.SetResolver(r)
	}
	if r == nil {
		return
	}

	owiAdapter := v3.NewOWIAdapter(s.owiClient)
	resumeAdapter := v3.NewResumeAdapter(s.resumeStore)
	recSvc, err := recservice.NewService(&s.cfg, s.vodManager, r, owiAdapter, resumeAdapter, r)
	if err != nil {
		log.L().Error().Err(err).Msg("failed to re-initialize recordings service")
		return
	}
	s.recordingsService = recSvc
	if s.v3Handler != nil {
		s.v3Handler.SetDependencies(
			s.v3Bus,
			s.v3Store,
			s.resumeStore,
			s.v3Scan,
			s.recordingPathMapper,
			s.channelManager,
			s.seriesManager,
			s.seriesEngine,
			s.vodManager,
			s.epgCache,
			s.healthManager,
			logSourceWrapper{},
			s.v3Scan,
			&dvrSourceWrapper{s},
			s.channelManager,
			&dvrSourceWrapper{s},
			s.recordingsService,
			s.requestShutdown,
			s.preflightProvider,
		)
	}
}

// SetRecordingsService injects a recordings service into the v3 handler (tests).
func (s *Server) SetRecordingsService(svc recservice.Service) {
	if s.v3Handler != nil {
		s.v3Handler.SetRecordingsService(svc)
	}
	s.recordingsService = svc
}

// SetAdmission sets the resource monitor for admission control.
func (s *Server) SetAdmission(adm *admission.ResourceMonitor) {
	if s.v3Handler != nil {
		s.v3Handler.SetAdmission(adm)
	}
}

// HandleRefreshInternal exposes the refresh handler for versioned APIs
// This allows different API versions to wrap the core refresh logic
func (s *Server) HandleRefreshInternal(w http.ResponseWriter, r *http.Request) {
	s.handleRefresh(w, r)
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	logger := log.WithComponentFromContext(r.Context(), "api")
	actor := r.RemoteAddr

	// Try to acquire the refresh flag atomically; fail fast if already running
	if !s.refreshing.CompareAndSwap(false, true) {
		logger.Warn().
			Str("event", "refresh.conflict").
			Str("method", r.Method).
			Msg("refresh already in progress")

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "30") // suggest retry after 30s
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":  "conflict",
			"detail": "A refresh operation is already in progress",
		})
		return
	}
	defer s.refreshing.Store(false)

	// Capture snapshot once to prevent config drift within this operation.
	s.mu.RLock()
	snap := s.snap
	s.mu.RUnlock()

	// Audit log: refresh started
	bouquets := strings.Split(snap.App.Bouquet, ",")
	if s.auditLogger != nil {
		s.auditLogger.RefreshStart(actor, bouquets)
	}

	// Create independent context for background job
	// Use Background() instead of request context to prevent premature cancellation
	jobCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Optional: Monitor client disconnect for logging
	clientDisconnected := make(chan struct{})
	go func() {
		<-r.Context().Done()
		if r.Context().Err() == context.Canceled {
			logger.Info().Msg("client disconnected during refresh (job continues)")
			close(clientDisconnected)
		}
	}()

	start := time.Now()
	var st *jobs.Status
	// Run the refresh via circuit breaker; it will mark failures and handle panics
	err := s.cb.Execute(func() error {
		var err error
		st, err = s.refreshFn(jobCtx, snap)
		return err
	})
	duration := time.Since(start)

	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		// Audit log: refresh error
		if s.auditLogger != nil {
			s.auditLogger.RefreshError(actor, err.Error())
		}

		// Distinguish open circuit (fast-fail) from internal error
		if errors.Is(err, resilience.ErrCircuitOpen) {
			logger.Warn().
				Str("event", "refresh.circuit_open").
				Int64("duration_ms", duration.Milliseconds()).
				Msg("circuit breaker open for refresh; rejecting request")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":  "unavailable",
				"detail": "Refresh temporarily disabled due to repeated failures",
			})
			return
		}
		s.mu.Lock()
		s.status.Error = "refresh operation failed" // Security: don't expose internal error details
		s.status.Channels = 0                       // NEW: reset channel count on error
		s.mu.Unlock()

		logger.Error().
			Err(err).
			Str("event", "refresh.failed").
			Str("method", r.Method).
			Int64("duration_ms", duration.Milliseconds()).
			Str("status", "error").
			Msg("refresh failed")
		// Security: Never expose internal error details to client
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Audit log: refresh completed successfully
	if s.auditLogger != nil {
		s.auditLogger.RefreshComplete(actor, st.Channels, st.Bouquets, duration.Milliseconds())
	}

	recordRefreshMetrics(duration, st.Channels)

	select {
	case <-clientDisconnected:
		logger.Info().
			Str("event", "refresh.success").
			Str("method", r.Method).
			Int("channels", st.Channels).
			Int64("duration_ms", duration.Milliseconds()).
			Str("status", "success").
			Msg("refresh completed despite client disconnect")
	default:
		logger.Info().
			Str("event", "refresh.success").
			Str("method", r.Method).
			Int("channels", st.Channels).
			Int64("duration_ms", duration.Milliseconds()).
			Str("status", "success").
			Msg("refresh completed successfully")
	}

	s.mu.Lock()
	s.status = *st
	s.mu.Unlock()

	if err := json.NewEncoder(w).Encode(st); err != nil {
		logger.Error().Err(err).Str("event", "refresh.encode_error").Msg("failed to encode refresh response")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// Handler returns the configured HTTP handler with all routes and middleware applied.
func (s *Server) Handler() http.Handler {
	return s.routes()
}

type logSourceWrapper struct{}

func (l logSourceWrapper) GetRecentLogs() []log.LogEntry {
	return log.GetRecentLogs()
}

type dvrSourceWrapper struct {
	s *Server
}

func (d *dvrSourceWrapper) GetStatusInfo(ctx context.Context) (*openwebif.StatusInfo, error) {
	d.s.mu.RLock()
	cfg := d.s.cfg
	d.s.mu.RUnlock()
	client := openwebif.New(cfg.Enigma2.BaseURL)
	return client.GetStatusInfo(ctx)
}

func (d *dvrSourceWrapper) HasTimerChange(ctx context.Context) bool {
	d.s.mu.RLock()
	cfg := d.s.cfg
	d.s.mu.RUnlock()
	client := openwebif.New(cfg.Enigma2.BaseURL)
	return client.HasTimerChange(ctx)
}

func (d *dvrSourceWrapper) GetTimers(ctx context.Context) ([]openwebif.Timer, error) {
	d.s.mu.RLock()
	cfg := d.s.cfg
	d.s.mu.RUnlock()
	client := openwebif.New(cfg.Enigma2.BaseURL)
	return client.GetTimers(ctx)
}

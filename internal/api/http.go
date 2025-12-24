// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Package api provides HTTP server functionality for the xg2g application.
package api

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ManuGH/xg2g/internal/api/middleware"
	"github.com/ManuGH/xg2g/internal/auth"
	"github.com/ManuGH/xg2g/internal/channels"
	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/dvr"
	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/ManuGH/xg2g/internal/hdhr"
	"github.com/ManuGH/xg2g/internal/health"
	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/v3/bus"
	"github.com/ManuGH/xg2g/internal/v3/store"
	"github.com/go-chi/chi/v5"
	"golang.org/x/sync/singleflight"

	"github.com/ManuGH/xg2g/internal/resilience"
)

//go:embed dist/*
var uiFS embed.FS

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
	epgCache      *epg.TV
	epgCacheTime  time.Time
	epgCacheMTime time.Time
	epgSfg        singleflight.Group

	// OpenWebIF Client Cache (P1 Performance Fix)
	owiClient *openwebif.Client
	owiEpoch  uint64

	// v3 Integration
	v3Bus   bus.Bus
	v3Store store.StateStore
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
func New(cfg config.AppConfig, cfgMgr *config.Manager) *Server {
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

	// Initialize OpenWebIF Client
	// Options can be tuned if needed (e.g. timeout, caching)

	env, err := config.ReadOSRuntimeEnv()
	if err != nil {
		log.L().Warn().Err(err).Msg("failed to read runtime environment, using defaults")
		env = config.DefaultEnv()
	}
	snap := config.BuildSnapshot(cfg, env)

	s := &Server{
		cfg:            cfg,
		snap:           snap,
		channelManager: cm,
		configManager:  cfgMgr,
		seriesManager:  sm,
		status: jobs.Status{
			Version: cfg.Version, // Initialize version from config
		},
		startTime:      time.Now(),
		piconSemaphore: make(chan struct{}, 50),
	}

	// Initialize Series Engine
	// Server (s) implements EpgProvider interface via GetEvents method
	s.seriesEngine = dvr.NewSeriesEngine(cfg, sm, func() dvr.OWIClient {
		return openwebif.New(cfg.OWIBase)
	})

	// Default refresh function
	s.refreshFn = jobs.Refresh
	s.refreshFn = jobs.Refresh
	// Initialize a conservative default circuit breaker (3 failures -> 30s open)
	s.cb = resilience.NewCircuitBreaker("api_refresh", 3, 30*time.Second, resilience.WithPanicRecovery(true))

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

	// Initialize health manager
	s.healthManager = health.NewManager(cfg.Version)

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
		if cfg.OWIBase == "" {
			return fmt.Errorf("receiver not configured")
		}
		// Use client for check if possible, or keep simple HTTP
		// Use the context provided by health manager (which includes timeout)
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, cfg.OWIBase, nil)
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
			Description: p.Desc,
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

// HDHomeRunServer returns the HDHomeRun server instance if enabled
func (s *Server) HDHomeRunServer() *hdhr.Server {
	return s.hdhr
}

// GetSeriesEngine returns the SeriesEngine instance (for scheduler wiring)
func (s *Server) GetSeriesEngine() *dvr.SeriesEngine {
	return s.seriesEngine
}

func (s *Server) routes() http.Handler {
	r := middleware.NewRouter(middleware.StackConfig{
		EnableCORS:     true,
		AllowedOrigins: s.cfg.AllowedOrigins,

		EnableSecurityHeaders: true,
		CSP:                   middleware.DefaultCSP,

		EnableMetrics:  true,
		TracingService: "xg2g-api",
		EnableLogging:  true,

		EnableRateLimit:    true,
		RateLimitEnabled:   s.cfg.RateLimitEnabled,
		RateLimitGlobalRPS: s.cfg.RateLimitGlobal,
		RateLimitBurst:     s.cfg.RateLimitBurst,
		RateLimitWhitelist: s.cfg.RateLimitWhitelist,
	})

	// Health checks (versionless - infrastructure endpoints)
	r.Get("/healthz", s.handleHealth)
	r.Get("/readyz", s.handleReady)

	// Web UI (read-only dashboard)
	r.Handle("/ui/*", http.StripPrefix("/ui", func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// CRITICAL: Disable Caching for UI to prevent version skew
			w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
			next.ServeHTTP(w, r)
		})
	}(s.uiHandler())))

	r.Get("/ui", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusMovedPermanently)
	})

	// Now/Next EPG for a list of services (frontend helper)
	r.With(s.authMiddleware).Post("/api/v2/services/now-next", http.HandlerFunc(s.handleNowNextEPG))
	// EPG listing is now handled by the generated API client (GetEpg)
	// Trigger config reload from disk (if a file-backed config is configured)
	r.With(s.authMiddleware).Post("/api/v2/system/config/reload", http.HandlerFunc(s.handleConfigReload))

	// Session Login (Cookie issuance for Native Players)
	r.With(s.authMiddleware).Post("/api/v2/auth/session", http.HandlerFunc(s.HandleSessionLogin))

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusTemporaryRedirect)
	})

	// Register Generated API v2 Routes
	// We use the generated handler which attaches to our existing router 'r'
	// and creates routes starting with /api
	HandlerWithOptions(s, ChiServerOptions{
		BaseURL:    "/api/v2",
		BaseRouter: r,
		Middlewares: []MiddlewareFunc{
			// Apply Auth Middleware to all API routes
			func(next http.Handler) http.Handler {
				return s.authMiddleware(next) // Use struct method for config access
			},
		},
	})

	// Manual Routes for Recordings (MVP)
	r.With(s.authMiddleware).Get("/api/v2/recordings", s.GetRecordingsHandler)
	r.With(s.authMiddleware).Get("/api/v2/recordings/stream/{ref}", s.GetRecordingStreamHandler)
	r.With(s.authMiddleware).Delete("/api/v2/recordings/{ref}", s.DeleteRecordingHandler)

	// HLS for Recordings (Proxied)
	// Note: Cookies (HttpOnly) are used for auth, but authMiddleware checks headers/cookies now.
	// So we apply authMiddleware.
	r.With(s.authMiddleware).Get("/api/v2/recordings/{recordingId}/playlist.m3u8", s.GetRecordingHLSPlaylistHandler)
	r.With(s.authMiddleware).Get("/api/v2/recordings/{recordingId}/{segment}", s.GetRecordingHLSCustomSegmentHandler)

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

	// Logo Proxy (Renamed from Picon to clean cache)
	r.Get("/logos/{ref}.png", s.handlePicons)
	r.Head("/logos/{ref}.png", s.handlePicons)

	// Phase 7A: v3 Control Plane (experimental/preview, /api/v3/*)
	r.Post("/api/v3/intents", s.handleV3Intents)
	r.Get("/api/v3/sessions", s.handleV3SessionsDebug)
	r.Get("/api/v3/sessions/{sessionID}", s.handleV3SessionState)
	r.Get("/api/v3/sessions/{sessionID}/hls/{filename}", s.handleV3HLS)

	// Harden file server: disable directory listing and use a secure handler
	r.Handle("/files/*", http.StripPrefix("/files/", s.secureFileServer()))
	return r
}

// authRequired is a middleware that enforces API token authentication for a handler.
// It implements a "fail-closed" strategy: if no token is configured, access is denied.
func (s *Server) authRequired(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := log.WithComponentFromContext(r.Context(), "auth")
		s.mu.RLock()
		token := s.cfg.APIToken
		authAnon := s.cfg.AuthAnonymous
		s.mu.RUnlock()

		if token == "" {
			if authAnon {
				logger.Warn().Str("event", "auth.anonymous").Msg("XG2G_AUTH_ANONYMOUS=true, allowing unauthenticated access")
				next.ServeHTTP(w, r)
				return
			}
			logger.Error().Str("event", "auth.fail_closed").Msg("XG2G_API_TOKEN not set and XG2G_AUTH_ANONYMOUS!=true. Denying access.")
			http.Error(w, "Unauthorized: Authentication required", http.StatusUnauthorized)
			return
		}

		// Use unified token extraction
		// Security: Query parameter tokens can leak in logs/proxies (use s.cfg.AllowQueryTokens to enable if needed)
		requestToken := extractToken(r, s.cfg.AllowQueryTokens)

		if requestToken == "" {
			logger.Warn().Str("event", "auth.missing_header").Msg("authorization header/param/cookie missing")

			// Audit log: missing authentication
			if s.auditLogger != nil {
				s.auditLogger.AuthMissing(clientIP(r), r.URL.Path)
			}
			http.Error(w, "Unauthorized: Missing API token", http.StatusUnauthorized)
			return
		}

		// Use constant-time comparison to prevent timing attacks
		if !auth.AuthorizeToken(requestToken, token) {
			logger.Warn().Str("event", "auth.invalid_token").Msg("invalid api token")

			// Audit log: authentication failure
			if s.auditLogger != nil {
				s.auditLogger.AuthFailure(clientIP(r), r.URL.Path, "invalid token")
			}
			http.Error(w, "Forbidden: Invalid API token", http.StatusForbidden)
			return
		}

		// Audit log: authentication success
		if s.auditLogger != nil {
			s.auditLogger.AuthSuccess(clientIP(r), r.URL.Path)
		}

		// Token is valid
		next.ServeHTTP(w, r)
	}
}

// GetStatus returns the current server status (thread-safe)
// This method is exposed for use by versioned API handlers
func (s *Server) GetStatus() jobs.Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

// GetConfig returns the server's current configuration
func (s *Server) GetConfig() config.AppConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// SetV3Components configures v3 event bus and store
func (s *Server) SetV3Components(b bus.Bus, st store.StateStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.v3Bus = b
	s.v3Store = st
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

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.healthManager.ServeHealth(w, r)
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	s.healthManager.ServeReady(w, r)
}

func (s *Server) handleXMLTV(w http.ResponseWriter, r *http.Request) {
	logger := log.WithComponentFromContext(r.Context(), "api")

	if strings.TrimSpace(s.cfg.XMLTVPath) == "" {
		logger.Warn().Str("event", "xmltv.not_configured").Msg("XMLTV path not configured")
		http.Error(w, "XMLTV file not available", http.StatusNotFound)
		return
	}

	// Get XMLTV file path with traversal protection
	xmltvPath, err := s.dataFilePath(s.cfg.XMLTVPath)
	if err != nil {
		logger.Error().Err(err).Str("event", "xmltv.invalid_path").Msg("XMLTV path rejected")
		http.Error(w, "XMLTV file not available", http.StatusNotFound)
		return
	}

	// Check if file exists
	fileInfo, err := os.Stat(xmltvPath)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Warn().
				Str("event", "xmltv.not_found").
				Str("path", xmltvPath).
				Msg("XMLTV file not found")
			http.Error(w, "XMLTV file not available", http.StatusNotFound)
			return
		}
		logger.Error().Err(err).Msg("failed to stat XMLTV file")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Security: Limit file size to prevent memory exhaustion (50MB max)
	const maxFileSize = 50 * 1024 * 1024
	if fileInfo.Size() > maxFileSize {
		logger.Warn().
			Int64("size", fileInfo.Size()).
			Str("event", "xmltv.too_large").
			Msg("XMLTV file exceeds maximum size")
		http.Error(w, "XMLTV file too large", http.StatusRequestEntityTooLarge)
		return
	}

	// Read XMLTV file
	// #nosec G304 -- xmltvPath is validated by dataFilePath and confined to the data directory
	xmltvData, err := os.ReadFile(xmltvPath)
	if err != nil {
		logger.Error().Err(err).Msg("failed to read XMLTV file")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Read M3U to build tvg-id to tvg-chno mapping
	m3uPath, err := s.dataFilePath("playlist.m3u")
	if err != nil {
		logger.Warn().Err(err).Msg("playlist path rejected, serving raw XMLTV")
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		if _, writeErr := w.Write(xmltvData); writeErr != nil {
			logger.Error().Err(writeErr).Msg("failed to write raw XMLTV response")
		}
		return
	}

	// Check M3U file size
	m3uInfo, err := os.Stat(m3uPath)
	if err != nil {
		logger.Warn().Err(err).Msg("M3U file not found, serving raw XMLTV")
		// Serve original XMLTV if M3U not available
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		if _, err := w.Write(xmltvData); err != nil {
			logger.Error().Err(err).Msg("failed to write raw XMLTV response")
		}
		return
	}

	// Security: Limit M3U file size (10MB max)
	const maxM3USize = 10 * 1024 * 1024
	if m3uInfo.Size() > maxM3USize {
		logger.Warn().
			Int64("size", m3uInfo.Size()).
			Msg("M3U file too large, serving raw XMLTV")
		// Serve original XMLTV if M3U is too large
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		if _, err := w.Write(xmltvData); err != nil {
			logger.Error().Err(err).Msg("failed to write raw XMLTV response")
		}
		return
	}

	// #nosec G304 -- m3uPath is validated by dataFilePath and confined to the data directory
	m3uData, err := os.ReadFile(m3uPath)
	if err != nil {
		logger.Error().Err(err).Msg("failed to read M3U file")
		// Serve original XMLTV if M3U not available
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		if _, err := w.Write(xmltvData); err != nil {
			logger.Error().Err(err).Msg("failed to write raw XMLTV response")
		}
		return
	}

	// Build mapping from tvg-id (sref-...) to tvg-chno (1, 2, 3...)
	idToNumber := make(map[string]string)
	m3uLines := strings.Split(string(m3uData), "\n")
	for _, line := range m3uLines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#EXTINF:") {
			var tvgID, tvgChno string

			// Extract tvg-id
			if idx := strings.Index(line, `tvg-id="`); idx != -1 {
				start := idx + 8
				if end := strings.Index(line[start:], `"`); end != -1 {
					tvgID = line[start : start+end]
				}
			}

			// Extract tvg-chno
			if idx := strings.Index(line, `tvg-chno="`); idx != -1 {
				start := idx + 10
				if end := strings.Index(line[start:], `"`); end != -1 {
					tvgChno = line[start : start+end]
				}
			}

			if tvgID != "" && tvgChno != "" {
				idToNumber[tvgID] = tvgChno
			}
		}
	}

	// Replace all channel IDs in XMLTV
	xmltvString := string(xmltvData)
	for oldID, newID := range idToNumber {
		// Replace in channel elements: <channel id="sref-...">
		xmltvString = strings.ReplaceAll(xmltvString, `id="`+oldID+`"`, `id="`+newID+`"`)
		// Replace in programme elements: <programme channel="sref-...">
		xmltvString = strings.ReplaceAll(xmltvString, `channel="`+oldID+`"`, `channel="`+newID+`"`)
	}

	// Serve the modified XMLTV
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300") // Cache for 5 minutes
	if _, err := w.Write([]byte(xmltvString)); err != nil {
		logger.Error().Err(err).Msg("failed to write XMLTV response")
		return
	}

	logger.Debug().
		Str("event", "xmltv.served").
		Str("path", xmltvPath).
		Int("mappings", len(idToNumber)).
		Msg("XMLTV file served with channel ID remapping")
}

// Handler returns the configured HTTP handler with all routes and middleware applied.
func (s *Server) Handler() http.Handler {
	return s.routes()
}

// handleLineupJSON handles /lineup.json endpoint for HDHomeRun emulation
// It reads the M3U playlist and converts it to HDHomeRun lineup format
func (s *Server) handleLineupJSON(w http.ResponseWriter, r *http.Request) {
	logger := log.WithComponentFromContext(r.Context(), "hdhr")

	// Read the M3U playlist file
	m3uPath, err := s.dataFilePath("playlist.m3u")
	if err != nil {
		logger.Error().Err(err).Str("event", "lineup.invalid_path").Msg("playlist path rejected")
		http.Error(w, "Lineup not available", http.StatusInternalServerError)
		return
	}

	// #nosec G304 -- m3uPath is validated by dataFilePath and confined to the data directory
	data, err := os.ReadFile(m3uPath)
	if err != nil {
		logger.Error().Err(err).Str("path", m3uPath).Msg("failed to read playlist file")
		http.Error(w, "Lineup not available", http.StatusInternalServerError)
		return
	}

	// Parse M3U content to extract channels
	var lineup []hdhr.LineupEntry
	lines := strings.Split(string(data), "\n")
	var currentChannel hdhr.LineupEntry

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#EXTINF:") {
			// Parse channel info from EXTINF line
			// Format: #EXTINF:-1 tvg-chno="X" tvg-id="sref-..." tvg-name="Channel Name",Display Name

			// Extract tvg-chno (channel number) - Plex uses this for EPG matching with XMLTV
			if idx := strings.Index(line, `tvg-chno="`); idx != -1 {
				start := idx + 10
				if end := strings.Index(line[start:], `"`); end != -1 {
					currentChannel.GuideNumber = line[start : start+end]
				}
			}

			// Extract channel name (after the last comma)
			if idx := strings.LastIndex(line, ","); idx != -1 {
				currentChannel.GuideName = strings.TrimSpace(line[idx+1:])
			}
		} else if len(line) > 0 && !strings.HasPrefix(line, "#") && currentChannel.GuideName != "" {
			// This is the stream URL
			streamURL := line

			currentChannel.URL = streamURL
			lineup = append(lineup, currentChannel)
			currentChannel = hdhr.LineupEntry{} // Reset for next channel
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(lineup); err != nil {
		logger.Error().Err(err).Msg("failed to encode lineup")
		return
	}

	logger.Debug().
		Int("channels", len(lineup)).
		Msg("HDHomeRun lineup served")
}

// uiHandler returns a handler that serves the embedded Web UI
func (s *Server) uiHandler() http.Handler {
	// Subdirectory "dist" matches the build output
	subFS, err := fs.Sub(uiFS, "dist")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "UI not available", http.StatusInternalServerError)
		})
	}
	fileServer := http.FileServer(http.FS(subFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Explicitly attach CSP so the main UI HTML allows blob: media (Safari HLS)
		w.Header().Set("Content-Security-Policy", middleware.DefaultCSP)

		// Assets (js, css, images) should be cached (hashed)
		// Index.html should NOT be cached to ensure updates
		path := r.URL.Path
		if path == "/" || path == "/index.html" || path == "" || !strings.Contains(path, ".") {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
		} else {
			// Assets in /assets/ are hashed usually
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		fileServer.ServeHTTP(w, r)
	})
}

// NewRouter creates and configures a new router with all middlewares and handlers.
// This includes the logging middleware, security headers, and the API routes.
func NewRouter(cfg config.AppConfig) http.Handler {
	server := New(cfg, nil)
	return server.routes()
}

// handlePicons proxies picon requests to the backend receiver and caches them locally
// Path: /logos/{ref}.png
func (s *Server) handlePicons(w http.ResponseWriter, r *http.Request) {
	ref := chi.URLParam(r, "ref")
	if ref == "" {
		http.Error(w, "Missing picon reference", http.StatusBadRequest)
		return
	}
	// Decode URL-encoded chars if present
	if decoded, err := url.PathUnescape(ref); err == nil {
		ref = decoded
	}

	// normalizeRef is used for Upstream requests (needs colons usually)
	// cacheRef is used for Local Filesystem (needs underscores for safety)

	// Ensure we have a "Colon-style" ref for logical processing / upstream
	processRef := strings.ReplaceAll(ref, "_", ":")

	// Ensure we have an "Underscore-style" ref for filesystem
	cacheRef := strings.ReplaceAll(processRef, ":", "_")

	// Local Cache Path
	piconDir := filepath.Join(s.cfg.DataDir, "picons")
	if err := os.MkdirAll(piconDir, 0750); err != nil {
		log.L().Error().Err(err).Msg("failed to create picon cache dir")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	localPath := filepath.Join(piconDir, cacheRef+".png")

	// 1. CACHE HIT
	if _, err := os.Stat(localPath); err == nil {
		logger := log.WithComponentFromContext(r.Context(), "picon")
		logger.Info().Str("ref", ref).Msg("Serving from cache")
		http.ServeFile(w, r, localPath)
		return
	}

	// 2. CACHE MISS -> Download
	upstreamBase := s.cfg.PiconBase
	if upstreamBase == "" {
		upstreamBase = s.cfg.OWIBase
	}
	if upstreamBase == "" {
		http.Error(w, "Picon backend not configured", http.StatusServiceUnavailable)
		return
	}

	// Use processRef (Colons) for upstream URL generation as Enigma2 expects colons or underscores depending on config
	// Usually PiconURL converts to underscores internally, but let's be safe.
	// Actually openwebif.PiconURL *already* converts to underscores!
	// So passing processRef (colons) is fine.
	upstreamURL := openwebif.PiconURL(upstreamBase, processRef)
	logger := log.WithComponentFromContext(r.Context(), "picon")

	// Acquire semaphore to protect upstream limit
	select {
	case s.piconSemaphore <- struct{}{}:
		defer func() { <-s.piconSemaphore }()
	case <-r.Context().Done():
		return // Client gave up
	}

	logger.Info().Str("ref", processRef).Str("upstream_url", upstreamURL).Msg("Picon: Downloading to cache")

	client := http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Get(upstreamURL)

	// Fallback Logic
	// Enter fallback/error handling if: Request failed OR Status is not OK (e.g. 404, 500, 403)
	if err != nil || (resp != nil && resp.StatusCode != http.StatusOK) {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			// It's a 404, we might try fallback
			log.L().Debug().Msg("Internal: Upstream returned 404, attempting fallback logic")
		}
		// It's 500, 403, etc.
		if resp != nil {
			_ = resp.Body.Close()
		}

		// Normalize processRef (HD->SD fallback)
		// e.g. 1:0:19... -> 1:0:1...
		normalizedRef := openwebif.NormalizeServiceRefForPicon(processRef)
		if normalizedRef != processRef {
			fallbackURL := openwebif.PiconURL(upstreamBase, normalizedRef)
			logger.Info().
				Str("original_ref", processRef).
				Str("normalized_ref", normalizedRef).
				Str("fallback_url", fallbackURL).
				Msg("Picon: attempting fallback to SD picon")

			respFallback, errFallback := client.Get(fallbackURL)
			if errFallback == nil && respFallback.StatusCode == http.StatusOK {
				// Success! Use the fallback response
				resp = respFallback
				goto ServePicon
			}

			// Fallback failed
			if respFallback != nil {
				_ = respFallback.Body.Close()
			}
			logger.Debug().Err(errFallback).Msg("SD picon fallback failed")
		}

		// If we are here, both original and fallback failed
		if err != nil {
			logger.Warn().Err(err).Str("url", upstreamURL).Msg("upstream fetch failed")
			http.Error(w, "Picon upstream unavailable", http.StatusBadGateway)
			return
		} else if resp != nil && resp.StatusCode != http.StatusNotFound {
			logger.Warn().Int("status", resp.StatusCode).Str("url", upstreamURL).Msg("upstream returned error")
			// Pass through 5xx errors from upstream
			if resp.StatusCode >= 500 {
				http.Error(w, "Picon upstream error", http.StatusBadGateway)
			} else {
				http.NotFound(w, r)
			}
			return
		} else {
			logger.Debug().Str("url", upstreamURL).Msg("upstream returned 404 (picon not found)")
		}

		http.NotFound(w, r)
		return
	}

ServePicon:
	defer func() {
		if resp != nil {
			_ = resp.Body.Close()
		}
	}()

	// 3. SAVE TO CACHE
	tempFile, err := os.CreateTemp(piconDir, "picon-*.tmp")
	if err != nil {
		logger.Error().Err(err).Msg("failed to create temp picon file")
		_, _ = io.Copy(w, resp.Body)
		return
	}
	defer func() {
		_ = tempFile.Close()
		_ = os.Remove(tempFile.Name())
	}()

	if _, err := io.Copy(tempFile, resp.Body); err != nil {
		logger.Error().Err(err).Msg("failed to write to temp picon file")
		http.Error(w, "Failed to cache picon", http.StatusInternalServerError)
		return
	}
	_ = tempFile.Close() // Close before rename on Windows

	if err := os.Rename(tempFile.Name(), localPath); err != nil {
		logger.Error().Err(err).Msg("failed to rename temp picon file to cache")
		// If rename fails, serve from the temp file if it still exists
		if _, statErr := os.Stat(tempFile.Name()); statErr == nil {
			http.ServeFile(w, r, tempFile.Name())
		} else {
			http.Error(w, "Failed to cache picon", http.StatusInternalServerError)
		}
		return
	}

	// Fix permissions so file can be read by http.ServeFile
	if err := os.Chmod(localPath, 0600); err != nil {
		logger.Warn().Err(err).Msg("failed to set picon file permissions")
	}

	// 4. SERVE
	http.ServeFile(w, r, localPath)
}

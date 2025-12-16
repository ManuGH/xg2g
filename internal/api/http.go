// SPDX-License-Identifier: MIT

// Package api provides HTTP server functionality for the xg2g application.
package api

import (
	"bytes"
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"

	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ManuGH/xg2g/internal/api/middleware"
	"github.com/ManuGH/xg2g/internal/channels"
	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/dvr"
	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/ManuGH/xg2g/internal/hdhr"
	"github.com/ManuGH/xg2g/internal/health"
	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/proxy"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"golang.org/x/sync/singleflight"
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
	cb             *CircuitBreaker
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

	proxy ProxyServer // Interface for proxy interactions
}

// ProxyServer defines the interface for proxy interactions required by the API.
type ProxyServer interface {
	GetSessions() []*proxy.StreamSession
	Terminate(id string) bool
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
	Get() config.AppConfig
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

	root, err := filepath.Abs(s.cfg.DataDir)
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

	// Initialize Series Manager (DVR v2)
	sm := dvr.NewManager(cfg.DataDir)
	if err := sm.Load(); err != nil {
		log.L().Error().Err(err).Msg("failed to load series rules")
	}

	// Initialize OpenWebIF Client
	// Options can be tuned if needed (e.g. timeout, caching)
	owiClient := openwebif.New(cfg.OWIBase)

	s := &Server{
		cfg:            cfg,
		snap:           config.BuildSnapshot(cfg),
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
	s.seriesEngine = dvr.NewSeriesEngine(sm, owiClient, s)

	// Default refresh function
	s.refreshFn = jobs.Refresh
	// Initialize a conservative default circuit breaker (3 failures -> 30s open)
	s.cb = NewCircuitBreaker(3, 30*time.Second)

	// Initialize HDHomeRun emulation if enabled
	logger := log.WithComponent("api")
	hdhrCfg := hdhr.GetConfigFromEnv(logger, cfg.DataDir)
	if hdhrCfg.Enabled {
		s.hdhr = hdhr.NewServer(hdhrCfg, cm)
		logger.Info().
			Bool("hdhr_enabled", true).
			Str("device_id", hdhrCfg.DeviceID).
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
	s.healthManager.RegisterChecker(health.NewReceiverChecker(func() error {
		if cfg.OWIBase == "" {
			return fmt.Errorf("receiver not configured")
		}
		// Use client for check if possible, or keep simple HTTP
		// For now keeping simple check to avoid dependency circularity issues during startup
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
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

// HDHomeRunServer returns the HDHomeRun server instance if enabled
func (s *Server) HDHomeRunServer() *hdhr.Server {
	return s.hdhr
}

// GetSeriesEngine returns the SeriesEngine instance (for scheduler wiring)
func (s *Server) GetSeriesEngine() *dvr.SeriesEngine {
	return s.seriesEngine
}

func (s *Server) routes() http.Handler {
	r := chi.NewRouter()

	// Apply middleware stack (order matters for correctness and performance)
	// 1. RequestID - generate unique ID for request correlation
	r.Use(chimiddleware.RequestID)
	// 2. Recoverer - panic recovery to prevent server crashes
	r.Use(chimiddleware.Recoverer)
	// 3. Global Rate Limiting - protect all endpoints from DoS (OWASP 2025)
	r.Use(middleware.APIRateLimit())
	// 4. Metrics - track all requests (before tracing for accurate timing)
	r.Use(middleware.Metrics())
	// 5. Tracing - distributed tracing with OpenTelemetry (with context propagation)
	r.Use(middleware.Tracing("xg2g-api"))
	// 6. Logging - structured request/response logging
	r.Use(log.Middleware())
	// 7. Security headers - add security headers to all responses
	r.Use(securityHeadersMiddleware)

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
	r.With(s.authMiddleware).Post("/api/auth/session", http.HandlerFunc(s.handleSessionLogin))

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
	// Legacy Picon Path (for compatibility)
	r.Get("/picon/{ref}.png", s.handlePicons)
	r.Head("/picon/{ref}.png", s.handlePicons)

	// Video Streaming (HLS/MPEG-TS)
	r.Get("/stream/{ref}/preflight", s.handleStreamPreflight)
	r.Handle("/stream/*", s.authRequired(s.handleStreamProxy))

	// Harden file server: disable directory listing and use a secure handler
	r.Handle("/files/*", http.StripPrefix("/files/", s.secureFileServer()))
	return r
}

func (s *Server) handleStreamPreflight(w http.ResponseWriter, r *http.Request) {
	// 1. Verify Token (Query Param)
	token := s.cfg.APIToken
	reqToken := r.URL.Query().Get("token")

	if token != "" && subtle.ConstantTimeCompare([]byte(reqToken), []byte(token)) != 1 {
		http.Error(w, "Invalid token", http.StatusForbidden)
		return
	}

	// 2. Set Cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "X-API-Token",
		Value:    token,
		Path:     "/", // simpler scope
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400, // 24h
	})

	w.WriteHeader(http.StatusOK)
}

// allow loading styles/images from common CDNs for Plyr, allow unsafe-inline for React/Plyr dynamic styles
const defaultCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https://cdn.plyr.io; media-src 'self' blob: data: https://cdn.plyr.io; connect-src 'self' https://cdn.plyr.io; frame-ancestors 'none'"

// securityHeadersMiddleware adds common security headers to all responses.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Strict Transport Security (HSTS)
		w.Header().Set("Strict-Transport-Security", "max-age=15552000; includeSubDomains")

		// Content Security Policy (CSP)
		w.Header().Set("Content-Security-Policy", defaultCSP)

		// X-Content-Type-Options
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// X-Frame-Options
		w.Header().Set("X-Frame-Options", "DENY")

		// Referrer-Policy
		w.Header().Set("Referrer-Policy", "no-referrer")

		next.ServeHTTP(w, r)
	})
}

// authRequired is a middleware that enforces API token authentication for a handler.
// It implements a "fail-closed" strategy: if no token is configured, access is denied.
func (s *Server) authRequired(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := log.WithComponentFromContext(r.Context(), "auth")
		token := s.cfg.APIToken

		if token == "" {
			if s.cfg.AuthAnonymous {
				logger.Warn().Str("event", "auth.anonymous").Msg("XG2G_AUTH_ANONYMOUS=true, allowing unauthenticated access")
				next.ServeHTTP(w, r)
				return
			}
			logger.Error().Str("event", "auth.fail_closed").Msg("XG2G_API_TOKEN not set and XG2G_AUTH_ANONYMOUS!=true. Denying access.")
			http.Error(w, "Unauthorized: Authentication required", http.StatusUnauthorized)
			return
		}

		reqToken := r.Header.Get("X-API-Token")
		// Fallback 1: Check 'token' query parameter (Critical for Native HLS/Safari)
		if reqToken == "" {
			reqToken = r.URL.Query().Get("token")
		}
		// Fallback 2: Check 'X-API-Token' cookie (Session-based auth for HLS segments)
		if reqToken == "" {
			if cookie, err := r.Cookie("X-API-Token"); err == nil {
				reqToken = cookie.Value
			}
		}

		if reqToken == "" {
			logger.Warn().Str("event", "auth.missing_header").Msg("authorization header/param/cookie missing")

			// Audit log: missing authentication
			if s.auditLogger != nil {
				s.auditLogger.AuthMissing(clientIP(r), r.URL.Path)
			}
			http.Error(w, "Unauthorized: Missing API token", http.StatusUnauthorized)
			return
		}

		// Use constant-time comparison to prevent timing attacks
		if subtle.ConstantTimeCompare([]byte(reqToken), []byte(token)) != 1 {
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

// SetProxy configures the proxy server dependency
func (s *Server) SetProxy(p ProxyServer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.proxy = p
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

	// Audit log: refresh started
	bouquets := strings.Split(s.cfg.Bouquet, ",")
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
	err := s.cb.Call(func() error {
		var err error
		s.mu.RLock()
		snap := s.snap
		s.mu.RUnlock()
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
		if errors.Is(err, errCircuitOpen) {
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
	if s.auditLogger != nil {
		return withMiddlewares(s.routes(), s.auditLogger)
	}
	return withMiddlewares(s.routes())
}

// authMiddleware is a middleware that enforces API token authentication.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := s.cfg.APIToken
		if token == "" {
			if s.cfg.AuthAnonymous {
				// Auth Explicitly Disabled
				logger := log.WithComponentFromContext(r.Context(), "auth")
				logger.Warn().Str("event", "auth.anonymous").Msg("XG2G_AUTH_ANONYMOUS=true, allowing unauthenticated access")
				next.ServeHTTP(w, r)
				return
			}

			// Fail-Closed (Default)
			logger := log.WithComponentFromContext(r.Context(), "auth")
			logger.Error().Str("event", "auth.fail_closed").Msg("XG2G_API_TOKEN not set and XG2G_AUTH_ANONYMOUS!=true. Denying access.")
			http.Error(w, "Unauthorized: Authentication required (Configure XG2G_API_TOKEN or XG2G_AUTH_ANONYMOUS=true)", http.StatusUnauthorized)
			return
		}

		// Check Bearer token
		authHeader := r.Header.Get("Authorization")
		logger := log.WithComponentFromContext(r.Context(), "auth")

		if authHeader == "" {
			// Check for Session Cookie (for Native Players/HLS)
			if cookie, err := r.Cookie("xg2g_session"); err == nil && cookie.Value != "" {
				authHeader = "Bearer " + cookie.Value
			}
		}

		if authHeader == "" {
			// Security alert: Missing auth header
			logger.Warn().
				Str("event", "auth.missing_header").
				Str("remote_addr", r.RemoteAddr).
				Str("path", r.URL.Path).
				Str("user_agent", r.Header.Get("User-Agent")).
				Msg("unauthorized access attempt - missing authorization header")
			http.Error(w, "Unauthorized: Missing Authorization header", http.StatusUnauthorized)
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			// Security alert: Invalid header format
			logger.Warn().
				Str("event", "auth.malformed_header").
				Str("remote_addr", r.RemoteAddr).
				Str("path", r.URL.Path).
				Msg("unauthorized access attempt - malformed authorization header")
			http.Error(w, "Unauthorized: Invalid Authorization header format", http.StatusUnauthorized)
			return
		}

		reqToken := parts[1]

		// Use constant-time comparison
		if subtle.ConstantTimeCompare([]byte(reqToken), []byte(token)) != 1 {
			// Security alert: Invalid token (potential brute force)
			logger.Warn().
				Str("event", "auth.invalid_token").
				Str("remote_addr", r.RemoteAddr).
				Str("path", r.URL.Path).
				Str("user_agent", r.Header.Get("User-Agent")).
				Msg("SECURITY ALERT: invalid bearer token - potential unauthorized access attempt")
			http.Error(w, "Forbidden: Invalid token", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
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

			// If H.264 stream repair is enabled, rewrite URL to use proxy host and port
			// This ensures Plex gets streams from the xg2g proxy (with H.264 repair) instead of direct receiver access
			s.mu.RLock()
			snap := s.snap
			s.mu.RUnlock()
			if snap.Runtime.Transcoder.H264RepairEnabled {
				if parsedURL, err := url.Parse(streamURL); err == nil {
					// Get proxy listen address (default :18000)
					proxyListen := strings.TrimSpace(snap.Runtime.StreamProxy.ListenAddr)
					if proxyListen == "" {
						proxyListen = ":18000"
					}
					// Extract port from proxy listen address (format is ":18000" or "0.0.0.0:18000")
					proxyPort := strings.TrimPrefix(proxyListen, ":")
					if colonIdx := strings.LastIndex(proxyPort, ":"); colonIdx != -1 {
						proxyPort = proxyPort[colonIdx+1:]
					}

					// Get the proxy host from the request (e.g., "10.10.55.14:8080")
					// This is the IP address that the client used to connect to the API server
					proxyHost := r.Host
					if colonIdx := strings.LastIndex(proxyHost, ":"); colonIdx != -1 {
						proxyHost = proxyHost[:colonIdx] // Extract just the IP without port
					}

					// Rewrite the URL to use proxy host and port
					// Example: http://10.10.55.64:8001/... -> http://10.10.55.14:18000/...
					parsedURL.Host = proxyHost + ":" + proxyPort
					streamURL = parsedURL.String()
				}
			}

			// If Plex Force HLS is enabled, add /hls/ prefix for proxy's HLS handler
			// This can work together with H264 repair (host/port rewrite above, /hls/ prefix here)
			if s.hdhr != nil && s.hdhr.PlexForceHLS() {
				// Note: The /hls/ handler exists on the stream proxy (port 18000), not the API server (port 8080)
				// Parse the stream URL (possibly already rewritten by H264 repair above)
				if parsedURL, err := url.Parse(streamURL); err == nil {
					// Only rewrite if /hls/ prefix doesn't already exist
					if !strings.HasPrefix(parsedURL.Path, "/hls/") {
						// Extract service reference from path (everything after last slash)
						if lastSlash := strings.LastIndex(parsedURL.Path, "/"); lastSlash != -1 {
							serviceRef := parsedURL.Path[lastSlash+1:]
							// Prepend /hls/ to the service reference, keeping the same host:port
							parsedURL.Path = "/hls/" + serviceRef
							streamURL = parsedURL.String()
						}
					}
				}
			}

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
	subFS, err := fs.Sub(uiFS, "dist")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "UI not available", http.StatusInternalServerError)
		})
	}
	fileServer := http.FileServer(http.FS(subFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Explicitly attach CSP so the main UI HTML allows blob: media (Safari HLS)
		w.Header().Set("Content-Security-Policy", defaultCSP)

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
	return withMiddlewares(server.routes())
}

// handlePicons proxies picon requests to the backend receiver and caches them locally
// Path: /picon/{ref}.png
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

// handleStreamProxy proxies stream requests to the internal stream server (port 18000).
// This avoids CORS issues and allows using relative paths from the WebUI.
func (s *Server) handleStreamProxy(w http.ResponseWriter, r *http.Request) {
	// For simple WebUI compatibility: /stream/{service_ref}/playlist.m3u8
	// We proxy directly to port 18000 WITHOUT the /hls/ prefix to let the proxy
	// handle it as a regular stream request (not HLS forced)

	// Determine upstream host/port
	// Allow configuring the proxy host for split deployments (e.g. Docker containers)
	s.mu.RLock()
	snap := s.snap
	s.mu.RUnlock()

	upstreamHost := strings.TrimSpace(snap.Runtime.StreamProxy.UpstreamHost)
	if upstreamHost == "" {
		upstreamHost = "127.0.0.1"
	}
	proxyPort := "18000"
	if port := portFromListenAddr(strings.TrimSpace(snap.Runtime.StreamProxy.ListenAddr)); port != "" {
		proxyPort = port
	}

	targetURL, _ := url.Parse("http://" + net.JoinHostPort(upstreamHost, proxyPort))
	targetHost := targetURL.Host
	origQuery := r.URL.RawQuery

	// Create local logger
	logger := log.WithComponent("proxy")
	logger.Info().Str("target", targetURL.String()).Msg("starting stream proxy")

	// Create reverse proxy
	p := httputil.NewSingleHostReverseProxy(targetURL)

	// Customize the director to rewrite path AND preserve query
	originalDirector := p.Director
	p.Director = func(req *http.Request) {
		originalDirector(req)

		// Rewrite path logic...
		trimmed := strings.TrimPrefix(req.URL.Path, "/stream/")
		trimmed = strings.TrimSuffix(trimmed, "/")
		parts := strings.SplitN(trimmed, "/", 2)
		serviceRef := parts[0]
		var remainder string
		if len(parts) > 1 {
			remainder = parts[1]
		}

		switch {
		case serviceRef == "":
			req.URL.Path = "/"
		case remainder == "":
			// Direct MPEG-TS path (/stream/{ref})
			req.URL.Path = "/" + serviceRef
		default:
			// HLS manifest/segments (/stream/{ref}/playlist.m3u8, .../segment.ts)
			req.URL.Path = "/hls/" + serviceRef + "/" + remainder
		}

		// Ensure Host header matches upstream proxy
		req.Host = targetHost

		// CRITICAL: Preserve Query Parameters!
		// We use query params for profile selection (e.g. ?llhls=1).
		req.URL.RawQuery = origQuery
	}

	// Important for streaming: flush immediately
	p.FlushInterval = 100 * time.Millisecond

	// Error handler
	p.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		logger.Error().Err(err).Str("path", r.URL.Path).Msg("proxy error")
		w.WriteHeader(http.StatusBadGateway)
	}

	// Token Propagation: Rewrite M3U8 playlists to include token in segment URLs
	// This makes HLS work on players that don't support Cookies/Headers (e.g., specific SmartTVs, Safari AVPlayer sometimes)
	token := r.URL.Query().Get("token")
	if token != "" {
		p.ModifyResponse = func(resp *http.Response) error {
			// Only rewrite playlists
			path := resp.Request.URL.Path
			if resp.StatusCode == 200 && (strings.HasSuffix(path, ".m3u8") || strings.HasSuffix(path, ".m3u")) {
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					_ = resp.Body.Close() // Ensure body is closed on error
					return err
				}
				_ = resp.Body.Close() // Close the original body after reading

				// Rewrite: Append ?token=... to lines ending in .ts, .aac, .m4s, .mp4, or inside lines (matches non-comment lines)
				// Simple approach: append to lines that look like file paths/URLs and don't already have query params?
				// Better: Regex or simple suffix check. Most HLS segments are relative.
				lines := strings.Split(string(body), "\n")
				var newLines []string
				for _, line := range lines {
					trim := strings.TrimSpace(line)
					if trim != "" && !strings.HasPrefix(trim, "#") {
						// This is a segment or variant URL
						if strings.Contains(trim, "?") {
							newLines = append(newLines, trim+"&token="+token)
						} else {
							newLines = append(newLines, trim+"?token="+token)
						}
					} else {
						newLines = append(newLines, line)
					}
				}
				newBody := []byte(strings.Join(newLines, "\n"))

				resp.Body = io.NopCloser(bytes.NewReader(newBody))
				resp.ContentLength = int64(len(newBody))
				resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(newBody)))
			}
			return nil
		}
	}

	// Serve
	p.ServeHTTP(w, r)
}

func portFromListenAddr(listen string) string {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		return ""
	}

	// Most common formats:
	// - ":18000"
	// - "0.0.0.0:18000"
	// - "127.0.0.1:18000"
	// - "[::]:18000"
	if _, port, err := net.SplitHostPort(listen); err == nil {
		return port
	}

	// Accept a bare port ("18000") as well.
	if !strings.Contains(listen, ":") {
		return listen
	}

	// Fallback: last colon split (handles slightly malformed values).
	if idx := strings.LastIndex(listen, ":"); idx != -1 && idx+1 < len(listen) {
		return listen[idx+1:]
	}
	return ""
}

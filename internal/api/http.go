// SPDX-License-Identifier: MIT

// Package api provides HTTP server functionality for the xg2g application.
package api

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/ManuGH/xg2g/internal/channels"
	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/hdhr"
	"github.com/ManuGH/xg2g/internal/health"
	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/proxy"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

//go:embed ui/*
var uiFS embed.FS

// Server represents the HTTP API server for xg2g.
type Server struct {
	mu             sync.RWMutex
	refreshing     atomic.Bool // serialize refreshes via atomic flag
	cfg            config.AppConfig
	status         jobs.Status
	cb             *CircuitBreaker
	hdhr           *hdhr.Server      // HDHomeRun emulation server
	configHolder   ConfigHolder      // Optional: for hot config reload support
	auditLogger    AuditLogger       // Optional: for audit logging
	healthManager  *health.Manager   // Health and readiness checks
	channelManager *channels.Manager // Channel management
	// refreshFn allows tests to stub the refresh operation; defaults to jobs.Refresh
	refreshFn      func(context.Context, config.AppConfig, *openwebif.StreamDetector) (*jobs.Status, error)
	streamDetector *openwebif.StreamDetector
	startTime      time.Time
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
func New(cfg config.AppConfig, detector *openwebif.StreamDetector) *Server {
	// Initialize channel manager
	cm := channels.NewManager(cfg.DataDir)
	if err := cm.Load(); err != nil {
		log.L().Error().Err(err).Msg("failed to load channel states")
	}

	s := &Server{
		cfg:            cfg,
		streamDetector: detector,
		channelManager: cm,
		status: jobs.Status{
			Version: cfg.Version, // Initialize version from config
		},
		startTime: time.Now(),
	}
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
	playlistName := os.Getenv("XG2G_PLAYLIST_FILENAME")
	if strings.TrimSpace(playlistName) == "" {
		playlistName = "playlist.m3u"
	}
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
		// Quick HEAD request to check if receiver is reachable
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

// HDHomeRunServer returns the HDHomeRun server instance if enabled
func (s *Server) HDHomeRunServer() *hdhr.Server {
	return s.hdhr
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
	r.Handle("/ui/*", http.StripPrefix("/ui", s.uiHandler()))
	r.Get("/ui", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusMovedPermanently)
	})
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusTemporaryRedirect)
	})

	// WebUI API endpoints (v3.0.0+)
	r.Route("/api", func(r chi.Router) {
		r.Get("/health", s.handleAPIHealth)
		r.Get("/config", s.handleAPIConfig)
		r.Post("/config/reload", s.handleConfigReloadV1)
		r.Post("/channels/toggle", s.handleAPIToggleChannel)
		r.Post("/channels/toggle-all", s.handleAPIToggleAllChannels)
		r.Get("/bouquets", s.handleAPIBouquets)
		r.Get("/channels", s.handleAPIChannels)
		r.Get("/logs/recent", s.handleAPILogs)

		// File Management
		r.Get("/files/status", s.handleAPIFileStatus)
		r.Post("/m3u/regenerate", s.handleAPIRegenerate)
		r.Get("/m3u/download", s.handleAPIPlaylistDownload)
		r.Get("/xmltv/download", s.handleAPIXMLTVDownload)
	})

	// V1 API (current stable version)
	s.registerV1Routes(r)

	// V2 API (future - behind feature flag)
	if featureEnabled("API_V2") {
		s.registerV2Routes(r)
	}

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

	// Harden file server: disable directory listing and use a secure handler
	r.Handle("/files/*", http.StripPrefix("/files/", s.secureFileServer()))
	return r
}

// securityHeadersMiddleware adds common security headers to all responses.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'")
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
			logger.Error().Str("event", "auth.fail_closed").Msg("XG2G_API_TOKEN is not configured, access denied")
			http.Error(w, "Unauthorized: API token not configured on server", http.StatusUnauthorized)
			return
		}

		reqToken := r.Header.Get("X-API-Token")
		if reqToken == "" {
			logger.Warn().Str("event", "auth.missing_header").Msg("authorization header missing")

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
		st, err = s.refreshFn(jobCtx, s.cfg, s.streamDetector)
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

// AuthMiddleware is a middleware that enforces API token authentication.
// It checks the "X-API-Token" header against the configured token.
// If the token is missing or invalid, it responds with an error.
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := os.Getenv("XG2G_API_TOKEN")
		if token == "" {
			// If no token is set, auth is disabled
			next.ServeHTTP(w, r)
			return
		}

		reqToken := r.Header.Get("X-API-Token")
		logger := log.FromContext(r.Context()).With().Str("component", "auth").Logger()

		if reqToken == "" {
			logger.Warn().Str("event", "auth.missing_header").Msg("authorization header missing")
			http.Error(w, "Unauthorized: Missing API token", http.StatusUnauthorized)
			return
		}

		// Use constant-time comparison to prevent timing attacks
		if subtle.ConstantTimeCompare([]byte(reqToken), []byte(token)) != 1 {
			logger.Warn().Str("event", "auth.invalid_token").Msg("invalid api token")
			http.Error(w, "Forbidden: Invalid API token", http.StatusForbidden)
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
			if proxy.IsH264RepairEnabled() {
				if parsedURL, err := url.Parse(streamURL); err == nil {
					// Get proxy listen address from environment (default :18000)
					proxyListen := os.Getenv("XG2G_PROXY_LISTEN")
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

// registerV1Routes registers all v1 API endpoints
func (s *Server) registerV1Routes(r chi.Router) {
	// Import the v1 package handler
	// Note: This will be done after we fix the import cycle
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/status", s.handleStatusV1)
		// Apply rate limiting and CSRF protection to expensive refresh operation
		r.With(middleware.RefreshRateLimit(), middleware.CSRFProtection()).
			Post("/refresh", s.authRequired(s.handleRefreshV1))
		// Config management endpoints
		r.Route("/config", func(r chi.Router) {
			r.With(middleware.CSRFProtection()).
				Post("/reload", s.authRequired(s.handleConfigReloadV1))
		})
		// Web UI endpoints (read-only dashboard)
		r.Route("/ui", func(r chi.Router) {
			r.Get("/status", s.handleUIStatus)    // Enhanced status for dashboard
			r.Get("/urls", s.handleUIUrls)        // M3U/XMLTV URLs
			r.Post("/refresh", s.handleUIRefresh) // Refresh trigger (no auth for UI)
		})
	})
}

// registerV2Routes registers all v2 API endpoints (placeholder for future)
func (s *Server) registerV2Routes(r chi.Router) {
	// V2 API implementation will go here
	// Example: different path structure, enhanced response formats, etc.
	r.Route("/api/v2", func(r chi.Router) {
		r.Get("/status", s.handleStatusV2Placeholder)
	})
}

// handleStatusV1 wraps the v1 handler
func (s *Server) handleStatusV1(w http.ResponseWriter, r *http.Request) {
	logger := log.WithComponentFromContext(r.Context(), "api.v1")

	status := s.GetStatus()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-API-Version", "1")

	resp := map[string]any{
		"status":   "ok",
		"version":  status.Version,
		"lastRun":  status.LastRun,
		"channels": status.Channels,
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Error().Err(err).Msg("failed to encode v1 status response")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	logger.Debug().
		Str("event", "v1.status.success").
		Str("version", status.Version).
		Time("lastRun", status.LastRun).
		Int("channels", status.Channels).
		Msg("v1 status request handled")
}

// handleRefreshV1 wraps the refresh handler for v1
func (s *Server) handleRefreshV1(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-API-Version", "1")
	s.handleRefresh(w, r)
}

// handleConfigReloadV1 handles POST /api/v1/config/reload - triggers config file reload.
func (s *Server) handleConfigReloadV1(w http.ResponseWriter, r *http.Request) {
	logger := log.WithComponentFromContext(r.Context(), "api.v1")
	w.Header().Set("X-API-Version", "1")

	actor := r.RemoteAddr // Default actor

	// Check if config holder is available
	if s.configHolder == nil {
		if s.auditLogger != nil {
			s.auditLogger.ConfigReload(actor, "failure", map[string]string{
				"error": "hot reload not enabled",
			})
		}
		RespondError(w, r, http.StatusServiceUnavailable, ErrServiceUnavailable,
			map[string]string{"reason": "hot reload not enabled"})
		logger.Warn().
			Str("event", "config.reload_unavailable").
			Msg("config reload requested but hot reload not enabled")
		return
	}

	// Trigger reload
	if err := s.configHolder.Reload(r.Context()); err != nil {
		if s.auditLogger != nil {
			s.auditLogger.ConfigReload(actor, "failure", map[string]string{
				"error": err.Error(),
			})
		}
		RespondError(w, r, http.StatusInternalServerError, ErrInternalServer,
			map[string]string{"error": err.Error()})
		logger.Error().
			Err(err).
			Str("event", "config.reload_failed").
			Msg("config reload failed")
		return
	}

	// Update server config from holder
	s.mu.Lock()
	s.cfg = s.configHolder.Get()
	s.mu.Unlock()

	// Audit log success
	if s.auditLogger != nil {
		s.auditLogger.ConfigReload(actor, "success", map[string]string{
			"method": "api",
		})
	}

	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{
		"status":  "ok",
		"message": "configuration reloaded successfully",
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Error().Err(err).Msg("failed to encode config reload response")
		return
	}

	logger.Info().
		Str("event", "config.reload_success").
		Msg("configuration reloaded via API")
}

// SetConfigHolder sets the config holder for hot reload support (optional).
// Must be called before routes are registered if hot reload is desired.
func (s *Server) SetConfigHolder(holder ConfigHolder) {
	s.configHolder = holder
}

// SetAuditLogger sets the audit logger for security event logging (optional).
func (s *Server) SetAuditLogger(logger AuditLogger) {
	s.auditLogger = logger
}

// handleUIStatus handles GET /api/v1/ui/status - enhanced status for Web UI dashboard.
// Returns aggregated status including health checks, receiver info, channels, and EPG.
func (s *Server) handleUIStatus(w http.ResponseWriter, r *http.Request) {
	logger := log.WithComponentFromContext(r.Context(), "api.ui")

	// Get current status
	s.mu.RLock()
	status := s.status
	cfg := s.cfg
	s.mu.RUnlock()

	// Get health check results (verbose mode to include all checks)
	healthStatus := s.healthManager.Health(r.Context(), true)

	// Build receiver info
	receiverInfo := map[string]any{
		"base_url":   cfg.OWIBase,
		"configured": cfg.OWIBase != "",
	}

	// Try to get receiver latency from health check
	if receiverCheck, ok := healthStatus.Checks["receiver_connection"]; ok {
		receiverInfo["reachable"] = receiverCheck.Status == health.StatusHealthy
		if receiverCheck.Error != "" {
			receiverInfo["error"] = receiverCheck.Error
		}
	}

	// Build channels info
	channelsInfo := map[string]any{
		"count":        status.Channels,
		"last_updated": status.LastRun,
	}

	// Build EPG info
	epgInfo := map[string]any{
		"enabled":      cfg.EPGEnabled,
		"programmes":   status.EPGProgrammes,
		"last_updated": status.LastRun,
	}

	// Check if EPG is stale (>48h old)
	if !status.LastRun.IsZero() {
		age := time.Since(status.LastRun)
		epgInfo["stale"] = age > 48*time.Hour
		epgInfo["age_hours"] = int(age.Hours())
	}

	// Build response
	resp := map[string]any{
		"health": map[string]any{
			"status": string(healthStatus.Status),
			"checks": healthStatus.Checks,
		},
		"receiver": receiverInfo,
		"channels": channelsInfo,
		"epg":      epgInfo,
		"version":  status.Version,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-API-Version", "1")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Error().Err(err).Msg("failed to encode UI status response")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	logger.Debug().
		Str("event", "ui.status.success").
		Str("health_status", string(healthStatus.Status)).
		Int("channels", status.Channels).
		Msg("UI status request handled")
}

// handleUIUrls handles GET /api/v1/ui/urls - returns M3U and XMLTV URLs.
func (s *Server) handleUIUrls(w http.ResponseWriter, r *http.Request) {
	logger := log.WithComponentFromContext(r.Context(), "api.ui")

	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()

	// Build base URL from request (use Host header)
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	baseURL := fmt.Sprintf("%s://%s", scheme, r.Host)

	// Get playlist filename from config or env
	playlistName := os.Getenv("XG2G_PLAYLIST_FILENAME")
	if strings.TrimSpace(playlistName) == "" {
		playlistName = "playlist.m3u"
	}

	// Build URLs
	m3uURL := fmt.Sprintf("%s/files/%s", baseURL, playlistName)
	xmltvURL := ""
	if cfg.XMLTVPath != "" {
		xmltvURL = fmt.Sprintf("%s/%s", baseURL, cfg.XMLTVPath)
	}

	resp := map[string]any{
		"m3u_url":   m3uURL,
		"xmltv_url": xmltvURL,
		"hdhr_url":  fmt.Sprintf("%s/device.xml", baseURL),
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-API-Version", "1")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Error().Err(err).Msg("failed to encode UI urls response")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	logger.Debug().
		Str("event", "ui.urls.success").
		Str("m3u_url", m3uURL).
		Str("xmltv_url", xmltvURL).
		Msg("UI URLs request handled")
}

// handleUIRefresh handles POST /api/v1/ui/refresh - triggers channel/EPG refresh.
// This is a simplified refresh endpoint for the Web UI (no authentication required).
func (s *Server) handleUIRefresh(w http.ResponseWriter, r *http.Request) {
	logger := log.WithComponentFromContext(r.Context(), "api.ui")

	// Try to acquire the refresh flag atomically
	if !s.refreshing.CompareAndSwap(false, true) {
		logger.Warn().Str("event", "ui.refresh.conflict").Msg("refresh already in progress")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":       "error",
			"message":      "A refresh operation is already in progress",
			"triggered_at": time.Now(),
		})
		return
	}

	// Release the flag when done
	defer s.refreshing.Store(false)

	// Trigger refresh
	logger.Info().Str("event", "ui.refresh.started").Msg("refresh triggered from UI")

	newStatus, err := s.refreshFn(r.Context(), s.cfg, s.streamDetector)
	if err != nil {
		logger.Error().Err(err).Str("event", "ui.refresh.failed").Msg("refresh failed")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":       "error",
			"message":      "Refresh failed",
			"error":        err.Error(),
			"triggered_at": time.Now(),
		})
		return
	}

	// Update status
	s.mu.Lock()
	s.status = *newStatus
	s.mu.Unlock()

	logger.Info().
		Str("event", "ui.refresh.success").
		Int("channels", newStatus.Channels).
		Int("bouquets", newStatus.Bouquets).
		Msg("refresh completed successfully")

	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{
		"status":       "success",
		"message":      "Refresh completed successfully",
		"triggered_at": time.Now(),
		"channels":     newStatus.Channels,
		"bouquets":     newStatus.Bouquets,
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Error().Err(err).Msg("failed to encode UI refresh response")
		return
	}
}

// uiHandler returns a handler that serves the embedded Web UI
func (s *Server) uiHandler() http.Handler {
	subFS, err := fs.Sub(uiFS, "ui")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "UI not available", http.StatusInternalServerError)
		})
	}
	return http.FileServer(http.FS(subFS))
}

// handleStatusV2Placeholder is a placeholder for v2 status endpoint
func (s *Server) handleStatusV2Placeholder(w http.ResponseWriter, r *http.Request) {
	logger := log.WithComponentFromContext(r.Context(), "api.v2")

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-API-Version", "2")

	resp := map[string]string{
		"message": "API v2 is under development",
		"status":  "preview",
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Error().Err(err).Msg("failed to encode v2 placeholder response")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// featureEnabled checks if a feature flag is enabled via environment variable
func featureEnabled(flag string) bool {
	val := os.Getenv("XG2G_FEATURE_" + flag)
	return strings.ToLower(val) == "true" || val == "1"
}

// NewRouter creates and configures a new router with all middlewares and handlers.
// This includes the logging middleware, security headers, and the API routes.
func NewRouter(cfg config.AppConfig) http.Handler {
	server := New(cfg, nil)
	return withMiddlewares(server.routes())
}

// SPDX-License-Identifier: MIT

// Package api provides HTTP server functionality for the xg2g application.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ManuGH/xg2g/internal/hdhr"
	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/api/middleware"
	"github.com/go-chi/chi/v5"
)

// Server represents the HTTP API server for xg2g.
type Server struct {
	mu         sync.RWMutex
	refreshing atomic.Bool // serialize refreshes via atomic flag
	cfg        jobs.Config
	status     jobs.Status
	cb         *CircuitBreaker
	hdhr       *hdhr.Server // HDHomeRun emulation server
	// refreshFn allows tests to stub the refresh operation; defaults to jobs.Refresh
	refreshFn func(context.Context, jobs.Config) (*jobs.Status, error)
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
func New(cfg jobs.Config) *Server {
	s := &Server{
		cfg: cfg,
		status: jobs.Status{
			Version: cfg.Version, // Initialize version from config
		},
	}
	// Default refresh function
	s.refreshFn = jobs.Refresh
	// Initialize a conservative default circuit breaker (3 failures -> 30s open)
	s.cb = NewCircuitBreaker(3, 30*time.Second)

	// Initialize HDHomeRun emulation if enabled
	logger := log.WithComponent("api")
	hdhrCfg := hdhr.GetConfigFromEnv(logger)
	if hdhrCfg.Enabled {
		s.hdhr = hdhr.NewServer(hdhrCfg)
		logger.Info().
			Bool("hdhr_enabled", true).
			Str("device_id", hdhrCfg.DeviceID).
			Msg("HDHomeRun emulation enabled")
	}

	return s
}

// HDHomeRunServer returns the HDHomeRun server instance if enabled
func (s *Server) HDHomeRunServer() *hdhr.Server {
	return s.hdhr
}

// SetStatus updates the server status (test helper)
func (s *Server) SetStatus(status jobs.Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
}

// SetRefreshFunc sets a custom refresh function (test helper)
func (s *Server) SetRefreshFunc(fn func(context.Context, jobs.Config) (*jobs.Status, error)) {
	s.refreshFn = fn
}

func (s *Server) routes() http.Handler {
	r := chi.NewRouter()

	// Apply middleware stack (order matters)
	// 1. Metrics - track all requests
	r.Use(middleware.Metrics())
	// 2. Tracing - distributed tracing with OpenTelemetry
	r.Use(middleware.Tracing("xg2g-api"))
	// 3. Logging - structured request/response logging
	r.Use(log.Middleware())
	// 4. Security headers - add security headers to all responses
	r.Use(securityHeadersMiddleware)

	// Health checks (versionless - infrastructure endpoints)
	r.Get("/healthz", s.handleHealth)
	r.Get("/readyz", s.handleReady)

	// Legacy API endpoints (deprecated - maintain backward compatibility)
	// These will be removed in a future major version
	r.Route("/api", func(r chi.Router) {
		r.Use(deprecationMiddleware(DeprecationConfig{
			SunsetVersion: "2.0.0",
			SunsetDate:    "2025-12-31T23:59:59Z",
			SuccessorPath: "/api/v1",
		}))
		r.Get("/status", s.handleStatus)
		r.Post("/refresh", s.authRequired(s.handleRefresh))
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
			http.Error(w, "Unauthorized: Missing API token", http.StatusUnauthorized)
			return
		}

		// Use constant-time comparison to prevent timing attacks
		if subtle.ConstantTimeCompare([]byte(reqToken), []byte(token)) != 1 {
			logger.Warn().Str("event", "auth.invalid_token").Msg("invalid api token")
			http.Error(w, "Forbidden: Invalid API token", http.StatusForbidden)
			return
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

// HandleRefreshInternal exposes the refresh handler for versioned APIs
// This allows different API versions to wrap the core refresh logic
func (s *Server) HandleRefreshInternal(w http.ResponseWriter, r *http.Request) {
	s.handleRefresh(w, r)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	logger := log.WithComponentFromContext(r.Context(), "api")

	s.mu.RLock()
	status := s.status
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	// Include an explicit status indicator alongside current status fields
	resp := map[string]any{
		"status":   "ok",
		"version":  status.Version,
		"lastRun":  status.LastRun,
		"channels": status.Channels,
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Error().Err(err).Str("event", "status.encode_error").Msg("failed to encode status response")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	logger.Debug().
		Str("event", "status.success").
		Str("version", status.Version).
		Time("lastRun", status.LastRun).
		Int("channels", status.Channels).
		Str("status", "ok").
		Msg("status request handled")
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	logger := log.WithComponentFromContext(r.Context(), "api")

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
		st, err = s.refreshFn(jobCtx, s.cfg)
		return err
	})
	duration := time.Since(start)

	w.Header().Set("Content-Type", "application/json")
	if err != nil {
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
	logger := log.WithComponentFromContext(r.Context(), "api")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		logger.Error().Err(err).Str("event", "health.encode_error").Msg("failed to encode health response")
	}
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	logger := log.WithComponentFromContext(r.Context(), "api")
	s.mu.RLock()
	status := s.status
	s.mu.RUnlock()

	// Check if artifacts exist and are readable
	playlistName := os.Getenv("XG2G_PLAYLIST_FILENAME")
	if strings.TrimSpace(playlistName) == "" {
		playlistName = "playlist.m3u"
	}
	playlistOK := false
	if playlistPath, err := s.dataFilePath(playlistName); err != nil {
		logger.Warn().Err(err).Str("event", "ready.invalid_playlist_path").Msg("playlist path outside data directory")
	} else {
		playlistOK = checkFile(r.Context(), playlistPath)
	}

	xmltvOK := true // Assume OK if not configured
	if strings.TrimSpace(s.cfg.XMLTVPath) != "" {
		if xmltvPath, err := s.dataFilePath(s.cfg.XMLTVPath); err != nil {
			xmltvOK = false
			logger.Warn().Err(err).Str("event", "ready.invalid_xmltv_path").Msg("xmltv path outside data directory")
		} else {
			xmltvOK = checkFile(r.Context(), xmltvPath)
		}
	}

	ready := !status.LastRun.IsZero() && status.Error == "" && playlistOK && xmltvOK
	w.Header().Set("Content-Type", "application/json")
	if !ready {
		w.WriteHeader(http.StatusServiceUnavailable)
		if err := json.NewEncoder(w).Encode(map[string]string{"status": "not-ready"}); err != nil {
			logger.Error().Err(err).Str("event", "ready.encode_error").Msg("failed to encode readiness response")
		}
		logger.Debug().
			Str("event", "ready.status").
			Str("state", "not-ready").
			Time("lastRun", status.LastRun).
			Str("error", status.Error).
			Bool("playlistOK", playlistOK).
			Bool("xmltvOK", xmltvOK).
			Msg("readiness probe")
		return
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ready"}); err != nil {
		logger.Error().Err(err).Str("event", "ready.encode_error").Msg("failed to encode readiness response")
	}
	logger.Debug().
		Str("event", "ready.status").
		Str("state", "ready").
		Time("lastRun", status.LastRun).
		Msg("readiness probe")
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
			currentChannel.URL = line
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
		r.Post("/refresh", s.authRequired(s.handleRefreshV1))
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
func NewRouter(cfg jobs.Config) http.Handler {
	server := New(cfg)
	return withMiddlewares(server.routes())
}

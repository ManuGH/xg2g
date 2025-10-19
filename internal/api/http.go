// SPDX-License-Identifier: MIT
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ManuGH/xg2g/internal/hdhr"
	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/gorilla/mux"
	"golang.org/x/text/unicode/norm"
)

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

func (s *Server) routes() http.Handler {
	r := mux.NewRouter()
	// Do not auto-clean or redirect paths; keep encoded path for security checks
	r.SkipClean(true)
	r.UseEncodedPath()
	r.Use(log.Middleware()) // Apply structured logging to all routes
	r.Use(securityHeadersMiddleware)

	// Public routes
	r.HandleFunc("/api/status", s.handleStatus).Methods("GET")
	r.HandleFunc("/healthz", s.handleHealth).Methods("GET")
	r.HandleFunc("/readyz", s.handleReady).Methods("GET")

	// HDHomeRun emulation endpoints (if enabled)
	if s.hdhr != nil {
		r.HandleFunc("/discover.json", s.hdhr.HandleDiscover).Methods("GET")
		r.HandleFunc("/lineup_status.json", s.hdhr.HandleLineupStatus).Methods("GET")
		r.HandleFunc("/lineup.json", s.handleLineupJSON).Methods("GET")
		r.HandleFunc("/lineup.json", s.hdhr.HandleLineupPost).Methods("POST")
		r.HandleFunc("/lineup.post", s.hdhr.HandleLineupPost).Methods("GET", "POST")
	}

	// Authenticated routes - only protect mutative endpoints
	r.HandleFunc("/api/refresh", s.authRequired(s.handleRefresh)).Methods("POST")

	// Harden file server: disable directory listing and use a secure handler
	r.PathPrefix("/files/").Handler(http.StripPrefix("/files/", s.secureFileServer()))
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

	ctx := r.Context()
	start := time.Now()
	var st *jobs.Status
	// Run the refresh via circuit breaker; it will mark failures and handle panics
	err := s.cb.Call(func() error {
		var err error
		st, err = s.refreshFn(ctx, s.cfg)
		return err
	})
	duration := time.Since(start)

	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		// Distinguish open circuit (fast-fail) from internal error
		if err == errCircuitOpen {
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
	logger.Info().
		Str("event", "refresh.success").
		Str("method", r.Method).
		Int("channels", st.Channels).
		Int64("duration_ms", duration.Milliseconds()).
		Str("status", "success").
		Msg("refresh completed")

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
	playlistOK := checkFile(r.Context(), filepath.Join(s.cfg.DataDir, playlistName))
	xmltvOK := true // Assume OK if not configured
	if s.cfg.XMLTVPath != "" {
		xmltvOK = checkFile(r.Context(), filepath.Join(s.cfg.DataDir, s.cfg.XMLTVPath))
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

// checkFile verifies if a file exists and is readable.
func checkFile(ctx context.Context, path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.IsDir() {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	if err := f.Close(); err != nil {
		// Log the error, but the function's outcome is already determined.
		log.FromContext(ctx).Warn().Err(err).Str("path", path).Msg("failed to close file during check")
	}
	return true
}

// isPathTraversal performs robust checks against path traversal attempts.
// It decodes the input multiple times to catch double-encoding, applies
// Unicode normalization, and searches for dangerous sequences including NULs.
func isPathTraversal(p string) bool {
	// Work on a copy
	decoded := p
	// Attempt multiple decode passes to catch double/triple encodings
	for i := 0; i < 3; i++ {
		prev := decoded
		if d, err := url.PathUnescape(decoded); err == nil {
			decoded = d
		} else {
			// As a fallback, try query unescape in case of stray '+' or query-like encoding
			if d2, err2 := url.QueryUnescape(decoded); err2 == nil {
				decoded = d2
			}
		}
		if decoded == prev {
			break
		}
	}

	lower := strings.ToLower(decoded)
	// Immediate dangerous byte patterns, independent of platform
	dangerSubstrings := []string{
		"..",        // parent traversal
		"..\\",      // windows-style backslash
		"%00",       // encoded NUL
		"\x00",      // literal NUL escape (defense-in-depth; may not appear literally)
		"%c0%ae",    // overlong UTF-8 for '.'
		"%e0%80%ae", // another overlong variant
	}
	for _, pat := range dangerSubstrings {
		if strings.Contains(lower, pat) {
			return true
		}
	}
	// Literal NUL after decoding
	if strings.Contains(decoded, "\x00") || strings.IndexByte(decoded, 0x00) >= 0 {
		return true
	}

	// Normalize and check again for dot-dot
	normalized := strings.ToLower(norm.NFC.String(decoded))
	if strings.Contains(normalized, "..") || strings.Contains(normalized, "..\\") {
		return true
	}

	return false
}

// secureFileServer creates a handler that serves files from the data directory
// with several security checks in place.
func (s *Server) secureFileServer() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger := log.WithComponentFromContext(r.Context(), "api")

		if r.Method != "GET" {
			logger.Warn().Str("event", "file_req.denied").Str("path", r.URL.Path).Str("reason", "method_not_allowed").Msg("method not allowed")
			recordFileRequestDenied("method_not_allowed")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		path := r.URL.Path
		// Enhanced traversal detection including multiple URL-decode passes,
		// Unicode normalization, mixed-case encodings, and NUL bytes.
		if isPathTraversal(path) {
			logger.Warn().Str("event", "file_req.denied").Str("path", r.URL.Path).Str("reason", "path_escape").Msg("detected traversal sequence")
			recordFileRequestDenied("path_escape")
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if strings.HasSuffix(path, "/") || path == "" {
			logger.Warn().Str("event", "file_req.denied").Str("path", r.URL.Path).Str("reason", "directory_listing").Msg("directory listing forbidden")
			recordFileRequestDenied("directory_listing")
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		absDataDir, err := filepath.Abs(s.cfg.DataDir)
		if err != nil {
			logger.Error().Err(err).Str("event", "file_req.internal_error").Msg("could not get absolute data dir")
			recordFileRequestDenied("internal_error")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		fullPath := filepath.Join(absDataDir, path)

		// Evaluate symlinks and clean the path
		realPath, err := filepath.EvalSymlinks(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				logger.Info().Str("event", "file_req.not_found").Str("path", fullPath).Msg("file not found")
				recordFileRequestDenied("not_found")
				http.Error(w, "Not found", http.StatusNotFound)
				return
			}
			logger.Error().Err(err).Str("event", "file_req.internal_error").Str("path", fullPath).Msg("could not evaluate symlinks")
			recordFileRequestDenied("internal_error")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// Also evaluate symlinks on the data directory itself to get a consistent base path.
		realDataDir, err := filepath.EvalSymlinks(absDataDir)
		if err != nil {
			logger.Error().Err(err).Str("event", "file_req.internal_error").Msg("could not evaluate symlinks on data dir")
			recordFileRequestDenied("internal_error")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// Security check: ensure the real path is within the real data directory
		if !strings.HasPrefix(realPath, realDataDir) {
			logger.Warn().Str("event", "file_req.denied").Str("path", path).Str("resolved_path", realPath).Str("reason", "path_escape").Msg("path escapes data directory")
			recordFileRequestDenied("path_escape")
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		// Security check: ensure we are not serving a directory
		info, err := os.Stat(realPath)
		if err != nil {
			logger.Error().Err(err).Str("event", "file_req.internal_error").Str("path", realPath).Msg("could not stat real path")
			recordFileRequestDenied("internal_error")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if info.IsDir() {
			logger.Warn().Str("event", "file_req.denied").Str("path", path).Str("reason", "directory_listing").Msg("resolved path is a directory")
			recordFileRequestDenied("directory_listing")
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		// --- ETag Caching Implementation ---
		f, err := os.Open(realPath)
		if err != nil {
			logger.Error().Err(err).Str("event", "file_req.internal_error").Str("path", realPath).Msg("could not open real path for serving")
			recordFileRequestDenied("internal_error")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		defer func() {
			if err := f.Close(); err != nil {
				logger.Warn().Err(err).Str("path", realPath).Msg("failed to close file")
			}
		}()

		// Re-fetch stat info from the opened file handle
		info, err = f.Stat()
		if err != nil {
			logger.Error().Err(err).Str("event", "file_req.internal_error").Str("path", realPath).Msg("could not stat opened file")
			recordFileRequestDenied("internal_error")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// Generate a weak ETag based on file modtime and size.
		// W/ prefix indicates a weak validator, suitable for content that is semantically
		// equivalent but not necessarily byte-for-byte identical.
		etag := fmt.Sprintf(`W/"%x-%x"`, info.ModTime().UnixNano(), info.Size())
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "public, max-age=3600") // Also set cache-control

		// Check if the client already has the same version of the file.
		if match := r.Header.Get("If-None-Match"); match != "" {
			if match == etag {
				recordFileCacheHit()
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}

		// All checks passed, serve the file content.
		// http.ServeContent is preferred over http.ServeFile when we already have an
		// open file, as it handles Range requests and sets Content-Type,
		// Content-Length, and Last-Modified headers correctly.

		// Set explicit charset for XML/M3U files to ensure proper UTF-8 handling
		if strings.HasSuffix(strings.ToLower(info.Name()), ".xml") {
			w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		} else if strings.HasSuffix(strings.ToLower(info.Name()), ".m3u") {
			w.Header().Set("Content-Type", "audio/x-mpegurl; charset=utf-8")
		}

		logger.Info().Str("event", "file_req.allowed").Str("path", path).Msg("serving file")
		recordFileRequestAllowed()
		recordFileCacheMiss()
		http.ServeContent(w, r, info.Name(), info.ModTime(), f)
	})
}

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
	m3uPath := filepath.Join(s.cfg.DataDir, "playlist.m3u")
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
			// Format: #EXTINF:-1 tvg-chno="1" ... tvg-name="Channel Name",Display Name

			// Extract channel number
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

// NewRouter creates and configures a new router with all middlewares and handlers.
// This includes the logging middleware, security headers, and the API routes.
func NewRouter(cfg jobs.Config) http.Handler {
	server := New(cfg)
	return withMiddlewares(server.routes())
}

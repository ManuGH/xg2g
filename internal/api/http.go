// SPDX-License-Identifier: MIT
package api

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/gorilla/mux"
)

type Server struct {
	mu        sync.RWMutex
	refreshMu sync.Mutex // NEW: serialize refreshes
	cfg       jobs.Config
	status    jobs.Status
}

func New(cfg jobs.Config) *Server {
	return &Server{
		cfg:    cfg,
		status: jobs.Status{},
	}
}

func (s *Server) routes() http.Handler {
	r := mux.NewRouter()
	r.Use(log.Middleware()) // Apply structured logging to all routes

	// Public routes
	r.HandleFunc("/api/status", s.handleStatus).Methods("GET")
	r.HandleFunc("/healthz", s.handleHealth).Methods("GET")
	r.HandleFunc("/readyz", s.handleReady).Methods("GET")

	// Authenticated routes
	authRouter := r.PathPrefix("/api").Subrouter()
	authRouter.Use(s.authMiddleware)
	authRouter.HandleFunc("/refresh", s.handleRefresh).Methods("POST")

	// Harden file server: disable directory listing and use a secure handler
	r.PathPrefix("/files/").Handler(http.StripPrefix("/files/", s.secureFileServer()))
	return r
}

// authMiddleware protects handlers that require authentication.
// If no API token is configured, it allows the request.
// If a token is configured, it expects a "Bearer <token>" in the Authorization header.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If no token is configured, authentication is disabled.
		if s.cfg.APIToken == "" {
			next.ServeHTTP(w, r)
			return
		}

		logger := log.WithComponentFromContext(r.Context(), "auth")
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			logger.Warn().Str("event", "auth.missing_header").Msg("authorization header missing")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			logger.Warn().Str("event", "auth.invalid_header").Msg("authorization header format must be Bearer {token}")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Use constant-time comparison to prevent timing attacks
		token := []byte(parts[1])
		expectedToken := []byte(s.cfg.APIToken)
		if subtle.ConstantTimeCompare(token, expectedToken) != 1 {
			logger.Warn().Str("event", "auth.invalid_token").Msg("invalid api token")
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		// Token is valid
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	logger := log.WithComponentFromContext(r.Context(), "api")

	s.mu.RLock()
	status := s.status
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		logger.Error().Err(err).Str("event", "status.encode_error").Msg("failed to encode status response")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	logger.Debug().
		Str("event", "status.success").
		Time("lastRun", status.LastRun).
		Int("channels", status.Channels).
		Str("status", "ok").
		Msg("status request handled")
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	logger := log.WithComponentFromContext(r.Context(), "api")

	// NEW: only allow a single refresh at a time
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()

	ctx := r.Context()
	start := time.Now()
	st, err := jobs.Refresh(ctx, s.cfg)
	duration := time.Since(start)

	w.Header().Set("Content-Type", "application/json")
	if err != nil {
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
	playlistOK := checkFile(filepath.Join(s.cfg.DataDir, "playlist.m3u"))
	xmltvOK := true // Assume OK if not configured
	if s.cfg.XMLTVPath != "" {
		xmltvOK = checkFile(filepath.Join(s.cfg.DataDir, s.cfg.XMLTVPath))
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
func checkFile(path string) bool {
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
	f.Close()
	return true
}

// secureFileServer creates a handler that serves files from the data directory
// with several security checks in place.
func (s *Server) secureFileServer() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Apply security headers to all file responses
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "public, max-age=300, must-revalidate")

		logger := log.WithComponentFromContext(r.Context(), "api")

		if r.Method != "GET" {
			logger.Warn().Str("event", "file_req.denied").Str("path", r.URL.Path).Str("reason", "method_not_allowed").Msg("method not allowed")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		path := r.URL.Path
		if strings.HasSuffix(path, "/") || path == "" {
			logger.Warn().Str("event", "file_req.denied").Str("path", r.URL.Path).Str("reason", "directory_listing").Msg("directory listing forbidden")
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		absDataDir, err := filepath.Abs(s.cfg.DataDir)
		if err != nil {
			logger.Error().Err(err).Str("event", "file_req.internal_error").Msg("could not get absolute data dir")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		fullPath := filepath.Join(absDataDir, path)

		// Evaluate symlinks and clean the path
		realPath, err := filepath.EvalSymlinks(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				logger.Info().Str("event", "file_req.not_found").Str("path", fullPath).Msg("file not found")
				http.Error(w, "Not found", http.StatusNotFound)
				return
			}
			logger.Error().Err(err).Str("event", "file_req.internal_error").Str("path", fullPath).Msg("could not evaluate symlinks")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// Also evaluate symlinks on the data directory itself to get a consistent base path.
		realDataDir, err := filepath.EvalSymlinks(absDataDir)
		if err != nil {
			logger.Error().Err(err).Str("event", "file_req.internal_error").Msg("could not evaluate symlinks on data dir")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// Security check: ensure the real path is within the real data directory
		if !strings.HasPrefix(realPath, realDataDir) {
			logger.Warn().Str("event", "file_req.denied").Str("path", path).Str("resolved_path", realPath).Str("reason", "path_escape").Msg("path escapes data directory")
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		// Security check: ensure we are not serving a directory
		info, err := os.Stat(realPath)
		if err != nil {
			logger.Error().Err(err).Str("event", "file_req.internal_error").Str("path", realPath).Msg("could not stat real path")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if info.IsDir() {
			logger.Warn().Str("event", "file_req.denied").Str("path", path).Str("reason", "directory_listing").Msg("resolved path is a directory")
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		// All checks passed, serve the file
		logger.Info().Str("event", "file_req.allowed").Str("path", path).Msg("serving file")
		http.ServeFile(w, r, realPath)
	})
}

func (s *Server) Handler() http.Handler {
	return withMiddlewares(s.routes())
}

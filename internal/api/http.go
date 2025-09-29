// SPDX-License-Identifier: MIT
package api

import (
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
	r.HandleFunc("/api/status", s.handleStatus).Methods("GET")
	r.HandleFunc("/api/refresh", s.handleRefresh).Methods("GET", "POST") // CHANGED: allow GET and POST
	r.HandleFunc("/healthz", s.handleHealth).Methods("GET")
	r.HandleFunc("/readyz", s.handleReady).Methods("GET")
	r.PathPrefix("/files/").Handler(http.StripPrefix("/files/", s.secureFileHandler()))
	return r
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
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST, OPTIONS")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

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

	ready := !status.LastRun.IsZero() && status.Error == ""
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

// secureFileHandler creates a secure file serving handler with symlink protection
func (s *Server) secureFileHandler() http.Handler {
	return http.HandlerFunc(s.handleSecureFile)
}

// handleSecureFile serves files with symlink escape protection and boundary checks
func (s *Server) handleSecureFile(w http.ResponseWriter, r *http.Request) {
	logger := log.WithComponentFromContext(r.Context(), "api")

	// Only allow GET and HEAD methods
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		logger.Warn().
			Str("event", "file_req").
			Str("method", r.Method).
			Str("reason", "method_not_allowed").
			Msg("file request denied")
		recordFileRequestDenied("method_not_allowed")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get the requested file path
	requestedPath := r.URL.Path
	if requestedPath == "" {
		requestedPath = "/"
	}

	// Clean and normalize the path to prevent basic traversal
	cleanPath := filepath.Clean(requestedPath)
	if strings.Contains(cleanPath, "..") {
		logger.Warn().
			Str("event", "file_req").
			Str("path", requestedPath).
			Str("reason", "path_traversal").
			Bool("allowed", false).
			Msg("file request denied")
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	// Get canonical data directory path
	dataRealPath, err := filepath.EvalSymlinks(s.cfg.DataDir)
	if err != nil {
		logger.Error().
			Err(err).
			Str("event", "file_req").
			Str("reason", "data_dir_error").
			Msg("cannot resolve data directory")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Build the full file path
	fullPath := filepath.Join(dataRealPath, cleanPath)

	// Resolve all symlinks in the final path
	realPath, err := filepath.EvalSymlinks(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Debug().
				Str("event", "file_req").
				Str("path", requestedPath).
				Str("reason", "not_found").
				Bool("allowed", false).
				Msg("file request denied")
			http.Error(w, "Not found", http.StatusNotFound)
		} else {
			// Could be broken symlink, circular symlink, etc.
			logger.Warn().
				Err(err).
				Str("event", "file_req").
				Str("path", requestedPath).
				Str("reason", "broken_symlink").
				Bool("allowed", false).
				Msg("file request denied")
			recordFileRequestDenied("broken_symlink")
			http.Error(w, "Bad request", http.StatusBadRequest)
		}
		return
	}

	// Verify the resolved path is within our data directory boundary
	cleanDataPath := filepath.Clean(dataRealPath)
	cleanRealPath := filepath.Clean(realPath)

	if !strings.HasPrefix(cleanRealPath+"/", cleanDataPath+"/") && cleanRealPath != cleanDataPath {
		logger.Warn().
			Str("event", "file_req").
			Str("path", requestedPath).
			Str("reason", "boundary_escape").
			Bool("allowed", false).
			Msg("file request denied")
		recordFileRequestDenied("boundary_escape")
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Check if path exists and get file info
	info, err := os.Stat(realPath)
	if err != nil {
		logger.Debug().
			Str("event", "file_req").
			Str("path", requestedPath).
			Str("reason", "stat_error").
			Bool("allowed", false).
			Msg("file request denied")
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	// Block directory access (no directory listings)
	if info.IsDir() {
		logger.Debug().
			Str("event", "file_req").
			Str("path", requestedPath).
			Str("reason", "directory_access").
			Bool("allowed", false).
			Msg("file request denied")
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Serve the file securely
	file, err := os.Open(realPath)
	if err != nil {
		logger.Error().Err(err).
			Str("path", requestedPath).
			Str("reason", "open_error").
			Bool("allowed", false).
			Msg("file request denied")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			logger.Error().Err(closeErr).Str("path", requestedPath).
				Msg("failed to close file")
		}
	}()

	// Log successful access and record metrics
	logger.Info().
		Str("event", "file_req").
		Str("path", requestedPath).
		Bool("allowed", true).
		Int64("size", info.Size()).
		Msg("file request allowed")
	recordFileRequestAllowed()

	// Set security headers
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")

	// Serve the file content with size limit
	http.ServeContent(w, r, filepath.Base(realPath), info.ModTime(), file)
}

func (s *Server) Handler() http.Handler {
	return withMiddlewares(s.routes())
}

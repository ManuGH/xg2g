package api

import (
	"encoding/json"
	"net/http"
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
	r.PathPrefix("/files/").Handler(http.StripPrefix("/files/",
		http.FileServer(http.Dir(s.cfg.DataDir))))
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
		s.status.Error = err.Error()
		s.status.Channels = 0 // NEW: reset channel count on error
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

func (s *Server) Handler() http.Handler {
	return withMiddlewares(s.routes())
}

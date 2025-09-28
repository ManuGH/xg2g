package api

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
)

type Server struct {
	mu        sync.RWMutex
	refreshMu sync.Mutex // NEW: serialize refreshes
	cfg       jobs.Config
	status    jobs.Status
	log       *zerolog.Logger
}

func New(cfg jobs.Config) *Server {
	return &Server{
		cfg:    cfg,
		status: jobs.Status{},
		log:    logger("api"),
	}
}

func (s *Server) routes() http.Handler {
	r := mux.NewRouter()
	r.HandleFunc("/api/status", s.handleStatus).Methods("GET")
	r.HandleFunc("/api/refresh", s.handleRefresh).Methods("GET", "POST") // CHANGED: allow GET and POST
	r.PathPrefix("/files/").Handler(http.StripPrefix("/files/",
		http.FileServer(http.Dir(s.cfg.DataDir))))
	return r
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(s.status); err != nil {
		// best-effort: write to stderr
		_ = err
	}
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
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

		s.log.Error().Err(err).Msg("refresh failed")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	recordRefreshMetrics(duration, st.Channels)
	s.log.Info().Int("channels", st.Channels).Dur("duration", duration).Msg("refresh completed")
	s.mu.Lock()
	s.status = *st
	s.mu.Unlock()

	if err := json.NewEncoder(w).Encode(st); err != nil {
		_ = err
	}
}

func (s *Server) Handler() http.Handler {
	return withMiddlewares(s.routes())
}

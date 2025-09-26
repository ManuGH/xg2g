package api

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/gorilla/mux"
	"github.com/ManuGH/xg2g/internal/jobs"
)

type Server struct {
	mu        sync.RWMutex
	refreshMu sync.Mutex
	cfg       jobs.Config
	status    jobs.Status
}

func New(cfg jobs.Config) *Server {
	return &Server{cfg: cfg, status: jobs.Status{}}
}

func (s *Server) routes() http.Handler {
	r := mux.NewRouter()
	r.HandleFunc("/api/status", s.handleStatus).Methods("GET")
	r.HandleFunc("/api/refresh", s.handleRefresh).Methods("POST")
	r.PathPrefix("/files/").Handler(http.StripPrefix("/files/",
		http.FileServer(http.Dir(s.cfg.DataDir))))
	return r
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.status)
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	// nur ein Refresh gleichzeitig
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()

	ctx := r.Context()
	st, err := jobs.Refresh(ctx, s.cfg)

	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		s.mu.Lock()
		s.status.Error = err.Error()
		s.status.Channels = 0
		s.mu.Unlock()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.mu.Lock()
	s.status = *st
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(st)
}

func (s *Server) Handler() http.Handler { return s.routes() }

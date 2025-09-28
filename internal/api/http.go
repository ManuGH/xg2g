package api

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/gorilla/mux"
)

type Server struct {
	mu        sync.RWMutex
	refreshMu sync.Mutex // NEU: Refresh serialisieren
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
	r.HandleFunc("/api/refresh", s.handleRefresh).Methods("GET", "POST") // GEÄNDERT
	r.PathPrefix("/files/").Handler(http.StripPrefix("/files/",
		http.FileServer(http.Dir(s.cfg.DataDir))))
	return r
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.status)
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	// NEU: Nur ein Refresh gleichzeitig
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()

	ctx := r.Context()
	st, err := jobs.Refresh(ctx, s.cfg)

	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		s.mu.Lock()
		s.status.Error = err.Error()
		s.status.Channels = 0 // NEU: Reset bei Fehler
		s.mu.Unlock()

		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.mu.Lock()
	s.status = *st
	s.mu.Unlock()

	json.NewEncoder(w).Encode(st)
}

func (s *Server) Handler() http.Handler {
	return s.routes()
}

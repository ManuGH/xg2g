package api

import "net/http"

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.healthManager.ServeHealth(w, r)
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	s.healthManager.ServeReady(w, r)
}

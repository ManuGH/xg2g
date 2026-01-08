// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"net/http"

	"github.com/ManuGH/xg2g/internal/control/read"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// CreateIntent implements POST /intents.
func (s *Server) CreateIntent(w http.ResponseWriter, r *http.Request) {
	s.ScopeMiddleware(ScopeV3Write)(http.HandlerFunc(s.handleV3Intents)).ServeHTTP(w, r)
}

// ListSessions implements GET /sessions.
func (s *Server) ListSessions(w http.ResponseWriter, r *http.Request, params ListSessionsParams) {
	_ = params
	s.ScopeMiddleware(ScopeV3Admin)(http.HandlerFunc(s.handleV3SessionsDebug)).ServeHTTP(w, r)
}

// GetSessionState implements GET /sessions/{sessionID}.
func (s *Server) GetSessionState(w http.ResponseWriter, r *http.Request, sessionID openapi_types.UUID) {
	_ = sessionID
	s.ScopeMiddleware(ScopeV3Read)(http.HandlerFunc(s.handleV3SessionState)).ServeHTTP(w, r)
}

// ServeHLS implements GET /sessions/{sessionID}/hls/{filename}.
func (s *Server) ServeHLS(w http.ResponseWriter, r *http.Request, sessionID openapi_types.UUID, filename string) {
	_ = sessionID
	_ = filename
	s.ScopeMiddleware(ScopeV3Read)(http.HandlerFunc(s.handleV3HLS)).ServeHTTP(w, r)
}

// ServeHLSHead implements HEAD /sessions/{sessionID}/hls/{filename}.
// Safari uses HEAD requests to check Content-Length before streaming.
// This delegates to the same handler as GET (handleV3HLS), which internally
// uses http.ServeContent that automatically handles HEAD by sending only headers.
func (s *Server) ServeHLSHead(w http.ResponseWriter, r *http.Request, sessionID openapi_types.UUID, filename string) {
	_ = sessionID
	_ = filename
	s.ScopeMiddleware(ScopeV3Read)(http.HandlerFunc(s.handleV3HLS)).ServeHTTP(w, r)
}

// TriggerSystemScan implements POST /api/v3/system/scan
func (s *Server) TriggerSystemScan(w http.ResponseWriter, r *http.Request) {
	s.ScopeMiddleware(ScopeV3Admin)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.RLock()
		scanner := s.v3Scan
		s.mu.RUnlock()

		if scanner == nil {
			RespondError(w, r, http.StatusServiceUnavailable, &APIError{
				Code:    "SCAN_UNAVAILABLE",
				Message: "Smart Profile Scanner is not initialized",
			})
			return
		}

		// Run in background
		if started := scanner.RunBackground(); started {
			writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
		} else {
			writeJSON(w, http.StatusOK, map[string]string{"status": "already_running"})
		}
	})).ServeHTTP(w, r)
}

// GetSystemScanStatus implements GET /api/v3/system/scan
func (s *Server) GetSystemScanStatus(w http.ResponseWriter, r *http.Request) {
	s.ScopeMiddleware(ScopeV3Admin)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.RLock()
		ss := s.scanSource
		s.mu.RUnlock()

		st, err := read.GetScanStatus(r.Context(), ss)
		if err != nil {
			writeProblem(w, r, http.StatusInternalServerError, "system/internal_error", "Scan Status Error", "INTERNAL_ERROR", err.Error(), nil)
			return
		}

		state := ScanStatusState(st.State)
		resp := ScanStatus{
			State:           &state,
			StartedAt:       &st.StartedAt,
			TotalChannels:   &st.TotalChannels,
			ScannedChannels: &st.ScannedChannels,
			UpdatedCount:    &st.UpdatedCount,
		}
		if st.FinishedAt > 0 {
			resp.FinishedAt = &st.FinishedAt
		}
		if st.LastError != "" {
			resp.LastError = &st.LastError
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})).ServeHTTP(w, r)
}

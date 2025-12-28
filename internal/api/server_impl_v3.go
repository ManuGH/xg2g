// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"net/http"

	openapi_types "github.com/oapi-codegen/runtime/types"
)

// CreateIntent implements POST /intents.
func (s *Server) CreateIntent(w http.ResponseWriter, r *http.Request) {
	s.scopeMiddleware(ScopeV3Write)(http.HandlerFunc(s.handleV3Intents)).ServeHTTP(w, r)
}

// ListSessions implements GET /sessions.
func (s *Server) ListSessions(w http.ResponseWriter, r *http.Request, params ListSessionsParams) {
	_ = params
	s.scopeMiddleware(ScopeV3Admin)(http.HandlerFunc(s.handleV3SessionsDebug)).ServeHTTP(w, r)
}

// GetSessionState implements GET /sessions/{sessionID}.
func (s *Server) GetSessionState(w http.ResponseWriter, r *http.Request, sessionID openapi_types.UUID) {
	_ = sessionID
	s.scopeMiddleware(ScopeV3Read)(http.HandlerFunc(s.handleV3SessionState)).ServeHTTP(w, r)
}

// ServeHLS implements GET /sessions/{sessionID}/hls/{filename}.
func (s *Server) ServeHLS(w http.ResponseWriter, r *http.Request, sessionID openapi_types.UUID, filename string) {
	_ = sessionID
	_ = filename
	s.scopeMiddleware(ScopeV3Read)(http.HandlerFunc(s.handleV3HLS)).ServeHTTP(w, r)
}

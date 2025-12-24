// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type v3Route struct {
	method  string
	pattern string
	scopes  []Scope
	handler http.HandlerFunc
}

func (s *Server) v3Routes() []v3Route {
	return []v3Route{
		{
			method:  http.MethodPost,
			pattern: "/api/v3/intents",
			scopes:  []Scope{ScopeV3Write},
			handler: s.handleV3Intents,
		},
		{
			method:  http.MethodGet,
			pattern: "/api/v3/sessions",
			scopes:  []Scope{ScopeV3Read},
			handler: s.handleV3SessionsDebug,
		},
		{
			method:  http.MethodGet,
			pattern: "/api/v3/sessions/{sessionID}",
			scopes:  []Scope{ScopeV3Read},
			handler: s.handleV3SessionState,
		},
		{
			method:  http.MethodGet,
			pattern: "/api/v3/sessions/{sessionID}/hls/{filename}",
			scopes:  []Scope{ScopeV3Read},
			handler: s.handleV3HLS,
		},
	}
}

func (s *Server) registerV3Routes(r chi.Router) {
	for _, route := range s.v3Routes() {
		if len(route.scopes) == 0 {
			panic("v3 route missing scopes: " + route.method + " " + route.pattern)
		}
		r.With(s.authMiddleware, s.scopeMiddleware(route.scopes...)).Method(route.method, route.pattern, route.handler)
	}
}

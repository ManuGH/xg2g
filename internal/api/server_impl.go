// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"net/http"

	"context" // This import is necessary for context.Context

	"github.com/ManuGH/xg2g/internal/control/auth"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
)

// StartRecordingCacheEvicter delegates to the v3 handler.
func (s *Server) StartRecordingCacheEvicter(ctx context.Context) {
	s.started.Store(true)
	s.v3Handler.StartRecordingCacheEvicter(ctx)
}

func (s *Server) tokenPrincipal(token string) (*auth.Principal, bool) {
	return s.v3Handler.TokenPrincipal(token)
}

func (s *Server) scopeMiddleware(required ...v3.Scope) func(http.Handler) http.Handler {
	return s.v3Handler.ScopeMiddleware(required...)
}

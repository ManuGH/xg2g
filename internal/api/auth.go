// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"net/http"

	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	"github.com/ManuGH/xg2g/internal/control/auth"
	"github.com/ManuGH/xg2g/internal/log"
)

// authMiddleware is a middleware that enforces API token authentication.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := s.GetConfig()
		hasTokens := cfg.APIToken != "" || len(cfg.APITokens) > 0

		if !hasTokens {
			// Fail-Closed: token-only access
			log.FromContext(r.Context()).Error().Str("event", "auth.fail_closed").Msg("No API tokens configured. Denying access.")
			v3.RespondError(w, r, http.StatusUnauthorized, v3.ErrUnauthorized)
			return
		}

		// Use unified token extraction
		reqToken := extractToken(r)

		logger := log.FromContext(r.Context()).With().Str("component", "auth").Logger()

		if reqToken == "" {
			logger.Warn().Str("event", "auth.missing_header").Msg("authorization header/cookie missing")
			v3.RespondError(w, r, http.StatusUnauthorized, v3.ErrUnauthorized)
			return
		}

		// Use constant-time comparison to prevent timing attacks
		principal, ok := s.tokenPrincipal(reqToken)
		if !ok {
			logger.Warn().Str("event", "auth.invalid_token").Msg("invalid api token")
			v3.RespondError(w, r, http.StatusUnauthorized, v3.ErrUnauthorized)
			return
		}

		// Token is valid - add principal to context
		ctx := auth.WithPrincipal(r.Context(), principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// setupValidateMiddleware enforces admin auth for setup validation.
func (s *Server) setupValidateMiddleware(next http.Handler) http.Handler {
	return s.authMiddleware(s.scopeMiddleware(v3.ScopeV3Admin)(next))
}

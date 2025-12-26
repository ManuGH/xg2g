// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"context"
	"net/http"

	"github.com/ManuGH/xg2g/internal/log"
)

// ctxPrincipalKey is used to store the authenticated principal in context
//
//nolint:unused // Legacy types - kept for future use
type ctxPrincipalKey struct{}

// Note: securityHeaders is defined in middleware.go

// authMiddleware is a middleware that enforces API token authentication.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.RLock()
		authAnon := s.cfg.AuthAnonymous
		hasTokens := s.cfg.APIToken != "" || len(s.cfg.APITokens) > 0
		s.mu.RUnlock()

		if !hasTokens {
			if authAnon {
				// Auth Explicitly Disabled
				next.ServeHTTP(w, r)
				return
			}
			// Fail-Closed (Default)
			log.FromContext(r.Context()).Error().Str("event", "auth.fail_closed").Msg("No API tokens configured and XG2G_AUTH_ANONYMOUS!=true. Denying access.")
			RespondError(w, r, http.StatusUnauthorized, ErrUnauthorized)
			return
		}

		// Use unified token extraction
		// For general API, we do NOT allow query parameter tokens, strictly enforcing Header/Cookie.
		reqToken := extractToken(r)

		logger := log.FromContext(r.Context()).With().Str("component", "auth").Logger()

		if reqToken == "" {
			logger.Warn().Str("event", "auth.missing_header").Msg("authorization header/cookie missing")
			RespondError(w, r, http.StatusUnauthorized, ErrUnauthorized)
			return
		}

		// Use constant-time comparison to prevent timing attacks
		if _, ok := s.tokenScopes(reqToken); !ok {
			logger.Warn().Str("event", "auth.invalid_token").Msg("invalid api token")
			RespondError(w, r, http.StatusUnauthorized, ErrUnauthorized)
			return
		}

		// Token is valid - add principal to context
		ctx := context.WithValue(r.Context(), ctxPrincipalKey{}, "authenticated")
		// Also store the token in context? Not stricly needed if we always re-extract or config is source.
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// CreateSession creates a secure HTTP-only session cookie exchange for the provided Bearer token.
// POST /api/v3/auth/session
// Requires Authentication (via Header) to be successful first.
func (s *Server) CreateSession(w http.ResponseWriter, r *http.Request) {
	// 1. Re-extract the token that was successfully validated
	// We prefer the Bearer token from the header for this "login" exchange.
	// We allow Header or Cookie (if refreshing). NO Query.
	reqToken := extractToken(r)

	// Fallback: If logic fails, use configured token if auth enabled (Single User Mode)
	s.mu.RLock()
	cfgToken := s.cfg.APIToken
	forceHTTPS := s.cfg.ForceHTTPS
	hasTokens := s.cfg.APIToken != "" || len(s.cfg.APITokens) > 0
	authAnon := s.cfg.AuthAnonymous
	s.mu.RUnlock()

	if reqToken == "" {
		if !hasTokens || authAnon {
			// If anonymous auth is enabled, or no tokens configured, we allow "login" (returns 200 OK)
			// This signals to the frontend that no explicit auth is needed.
			w.WriteHeader(http.StatusOK)
			return
		}
		if cfgToken == "" {
			RespondError(w, r, http.StatusUnauthorized, ErrUnauthorized)
			return
		}
		reqToken = cfgToken
	} else {
		if _, ok := s.tokenScopes(reqToken); !ok {
			RespondError(w, r, http.StatusUnauthorized, ErrUnauthorized)
			return
		}
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "xg2g_session",
		Value:    reqToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil || forceHTTPS, // auto-detect or force
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400, // 24h
	})

	w.WriteHeader(http.StatusOK) // 200 OK
}

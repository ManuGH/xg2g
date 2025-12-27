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
		cfg := s.GetConfig()
		authAnon := cfg.AuthAnonymous
		hasTokens := cfg.APIToken != "" || len(cfg.APITokens) > 0

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
		// Also store the token in context? Not strictly needed if we always re-extract or config is source.
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

	// No implicit fallback; token must be presented.
	cfg := s.GetConfig()
	forceHTTPS := cfg.ForceHTTPS
	hasTokens := cfg.APIToken != "" || len(cfg.APITokens) > 0
	authAnon := cfg.AuthAnonymous

	// The client MUST present a valid token to exchange it for a session cookie.
	if reqToken == "" {
		if !hasTokens && authAnon {
			// If anonymous auth is enabled AND no tokens are configured, we allow "login" (returns 200 OK)
			// This signals to the frontend that no explicit auth is needed.
			w.WriteHeader(http.StatusOK)
			return
		}
		// Fail if no token presented and auth is required
		RespondError(w, r, http.StatusUnauthorized, ErrUnauthorized)
		return
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

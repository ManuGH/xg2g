// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"net/http"
	"strings"

	"github.com/ManuGH/xg2g/internal/control/auth"
	"github.com/ManuGH/xg2g/internal/log"
)

// ctxPrincipalKey is used to store the authenticated principal in context
//
//nolint:unused // Legacy types - kept for future use
type ctxPrincipalKey struct{}

// Note: securityHeaders is defined in middleware.go

// authMiddlewareImpl is the implementation of the API token authentication middleware.
func (s *Server) authMiddlewareImpl(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := s.GetConfig()
		hasTokens := cfg.APIToken != "" || len(cfg.APITokens) > 0

		if !hasTokens {
			// Fail-Closed: token-only access
			log.FromContext(r.Context()).Error().Str("event", "auth.fail_closed").Msg("No API tokens configured. Denying access.")
			RespondError(w, r, http.StatusUnauthorized, ErrUnauthorized)
			return
		}

		// Use unified token extraction
		// For general API, we allow both Header (Bearer) and Cookie (xg2g_session).
		reqToken, authSource := extractTokenDetailed(r)

		logger := log.FromContext(r.Context()).With().Str("component", "auth").Logger()

		if reqToken != "" {
			logger.Debug().Str("method", authSource).Msg("authenticated request")
		}

		if reqToken == "" {
			logger.Warn().Str("event", "auth.missing_header").Msg("authorization header/cookie missing")
			RespondError(w, r, http.StatusUnauthorized, ErrUnauthorized)
			return
		}

		// Security Invariant (P3-Auth): Media endpoints (HLS/Direct Stream) REQUIRE session cookies.
		// Bearer tokens are for API only. This prevents certain classes of leakage/hotlinking.
		if isMediaRequest(r) && !strings.Contains(authSource, "cookie") {
			logger.Warn().
				Str("event", "auth.media_forbidden_source").
				Str("source", authSource).
				Str("path", r.URL.Path).
				Msg("media request denied: bearer token not allowed for media (cookie required)")
			RespondError(w, r, http.StatusUnauthorized, ErrUnauthorized)
			return
		}

		// Use constant-time comparison to prevent timing attacks
		principal, ok := s.TokenPrincipal(reqToken)
		if !ok {
			logger.Warn().Str("event", "auth.invalid_token").Msg("invalid api token")
			RespondError(w, r, http.StatusUnauthorized, ErrUnauthorized)
			return
		}

		// Token is valid - add principal to context
		ctx := auth.WithPrincipal(r.Context(), principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// CreateSession creates a secure HTTP-only session cookie exchange for the provided Bearer token.
// POST /api/v3/auth/session
// Requires Authentication (via Header) to be successful first.
func (s *Server) CreateSession(w http.ResponseWriter, r *http.Request) {
	// 1. Re-extract the token that was successfully validated.
	// Require Bearer header for this "login" exchange.
	reqToken := extractBearerToken(r)

	// No implicit fallback; token must be presented.
	cfg := s.GetConfig()
	forceHTTPS := cfg.ForceHTTPS

	// The client MUST present a valid bearer token to exchange it for a session cookie.
	if reqToken == "" {
		// Fail if no token presented and auth is required
		RespondError(w, r, http.StatusUnauthorized, ErrUnauthorized)
		return
	} else {
		if _, ok := s.TokenPrincipal(reqToken); !ok {
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

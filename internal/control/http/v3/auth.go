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
		// For general API, we allow Header (Bearer) and session cookie.
		// Legacy X-API-Token vectors are optional behind config flag.
		allowLegacySources := !cfg.APIDisableLegacyTokenSources
		reqToken, authSource := extractTokenDetailedWithLegacyPolicy(r, allowLegacySources)

		logger := log.FromContext(r.Context()).With().
			Str("component", "auth").
			Str("source", authSource).
			Str("uri", r.RequestURI).
			Logger()

		if reqToken != "" {
			logger.Debug().Msg("authenticated request")
			if strings.Contains(authSource, "X-API-Token") {
				logger.Warn().
					Str("event", "auth.legacy_token_source").
					Msg("legacy token source accepted; migrate to Authorization Bearer or xg2g_session and set XG2G_API_DISABLE_LEGACY_TOKEN_SOURCES=true")
			}
		} else {
			logger.Warn().
				Str("event", "auth.missing_token").
				Msg("authorization token missing from all enabled sources")
			RespondError(w, r, http.StatusUnauthorized, ErrUnauthorized)
			return
		}

		// Security Invariant (P3-Auth): Media endpoints (HLS/Direct Stream) normally REQUIRE session cookies.
		if isMediaRequest(r) && !strings.Contains(authSource, "cookie") {
			logger.Warn().
				Str("event", "auth.media_no_cookie").
				Msg("media request attempted without session cookie (bearer not allowed for media)")
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
		Path:     "/api/v3/",
		HttpOnly: true,
		Secure:   r.TLS != nil || forceHTTPS, // auto-detect or force
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400, // 24h
	})

	w.WriteHeader(http.StatusOK) // 200 OK
}

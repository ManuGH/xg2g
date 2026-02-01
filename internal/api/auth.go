// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"net/http"
	"strings"

	"github.com/ManuGH/xg2g/internal/control/auth"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
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
		reqToken, authSource := auth.ExtractTokenDetailed(r)
		logger := log.FromContext(r.Context()).With().Str("component", "auth").Logger()

		if reqToken != "" {
			logger.Debug().Str("method", authSource).Msg("authenticated request")
		}

		if reqToken == "" {
			logger.Warn().Str("event", "auth.missing_header").Msg("authorization header/cookie missing")
			v3.RespondError(w, r, http.StatusUnauthorized, v3.ErrUnauthorized)
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

func isMediaRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	path := r.URL.Path
	if strings.HasPrefix(path, "/api/v3/recordings/") &&
		(strings.HasSuffix(path, "/stream.mp4") || strings.HasSuffix(path, "/playlist.m3u8")) {
		return true
	}
	if strings.HasPrefix(path, "/api/v3/sessions/") && strings.Contains(path, "/hls/") {
		return true
	}
	return false
}

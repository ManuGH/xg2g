// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/control/auth"
	"github.com/ManuGH/xg2g/internal/control/middleware"
	"github.com/ManuGH/xg2g/internal/log"
)

const (
	sessionCookieName          = "xg2g_session"
	sessionCookieMaxAgeSeconds = 86400
	defaultAuthSessionTTL      = 24 * time.Hour
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
		reqToken, authSource := s.extractTokenDetailedWithLegacyPolicy(r, allowLegacySources)

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
		principal, ok := s.TokenPrincipal(r.Context(), reqToken)
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

	// The client MUST present a valid bearer token to exchange it for a session cookie.
	if reqToken == "" {
		// Fail if no token presented and auth is required
		RespondError(w, r, http.StatusUnauthorized, ErrUnauthorized)
		return
	} else {
		if _, ok := s.TokenPrincipal(r.Context(), reqToken); !ok {
			RespondError(w, r, http.StatusUnauthorized, ErrUnauthorized)
			return
		}
	}

	effectiveHTTPS := s.requestIsHTTPS(r)
	if !effectiveHTTPS && !requestRemoteIsLoopback(r) {
		RespondError(w, r, http.StatusBadRequest, ErrHTTPSRequired, "session exchange requires HTTPS or a trusted HTTPS proxy; plain HTTP is only accepted from loopback")
		return
	}

	store := s.authSessionStoreOrDefault()
	if existingCookie, err := r.Cookie(sessionCookieName); err == nil {
		s.deleteAuthSession(existingCookie.Value)
	}

	sessionID, err := store.CreateSession(reqToken, s.authSessionTTLOrDefault())
	if err != nil {
		RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, "failed to create session")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     "/api/v3/",
		HttpOnly: true,
		Secure:   effectiveHTTPS,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   sessionCookieMaxAgeSeconds,
	})

	w.WriteHeader(http.StatusOK) // 200 OK
}

func (s *Server) authSessionStoreOrDefault() auth.SessionTokenStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.authSessionStore == nil {
		s.authSessionStore = auth.NewInMemorySessionTokenStore()
	}
	return s.authSessionStore
}

func (s *Server) authSessionTTLOrDefault() time.Duration {
	s.mu.RLock()
	ttl := s.authSessionTTL
	s.mu.RUnlock()
	if ttl <= 0 {
		return defaultAuthSessionTTL
	}
	return ttl
}

func (s *Server) resolveSessionToken(sessionID string) (string, bool) {
	return s.authSessionStoreOrDefault().ResolveSessionToken(sessionID)
}

func (s *Server) deleteAuthSession(sessionID string) {
	s.authSessionStoreOrDefault().InvalidateSession(sessionID)
}

func (s *Server) requestIsHTTPS(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	if !strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https") {
		return false
	}

	trustedProxies, err := middleware.ParseCIDRs(splitCSVNonEmpty(strings.TrimSpace(s.GetConfig().TrustedProxies)))
	if err != nil {
		log.L().Warn().Err(err).Msg("invalid trusted proxies configuration for auth session exchange")
		return false
	}
	remoteIP := requestRemoteIP(r)
	return remoteIP != nil && middleware.IsIPAllowed(remoteIP, trustedProxies)
}

func requestRemoteIsLoopback(r *http.Request) bool {
	ip := requestRemoteIP(r)
	return ip != nil && ip.IsLoopback()
}

func requestRemoteIP(r *http.Request) net.IP {
	if r == nil {
		return nil
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	if host == "" {
		return nil
	}
	return net.ParseIP(host)
}

func splitCSVNonEmpty(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

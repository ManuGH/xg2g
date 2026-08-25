// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/control/auth"
	"github.com/ManuGH/xg2g/internal/control/middleware"
	householddomain "github.com/ManuGH/xg2g/internal/household"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/problemcode"
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
type dpopRequestContextKey struct{}

// Note: securityHeaders is defined in middleware.go

// authMiddlewareImpl is the implementation of the API token authentication middleware.
func (s *Server) authMiddlewareImpl(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if raw, ok := r.Context().Value(bearerAuthScopesKey).([]string); ok && len(raw) == 0 {
			next.ServeHTTP(w, r)
			return
		}

		// A media request may instead carry a playback ticket: the credential
		// class a player uses, valid only here and only for the session named
		// in this path. Checked before anything else because a ticket is not a
		// token and must never be fed to the token pipeline.
		if principal, ok := s.playbackTicketPrincipal(r); ok {
			next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), principal)))
			return
		}

		cfg := s.GetConfig()
		hasTokens := cfg.APIToken != "" || len(cfg.APITokens) > 0 || s.hasDeviceAuthStore()

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

		// Security Invariant (P3-Auth): Media endpoints (HLS/Direct Stream) REQUIRE
		// the real session cookie. Match the canonical source EXACTLY — a substring
		// "cookie" check let the legacy "X-API-Token cookie" source satisfy this.
		if isMediaRequest(r) && authSource != auth.SessionCookieSource {
			logger.Warn().
				Str("event", "auth.media_no_cookie").
				Msg("media request attempted without session cookie (bearer not allowed for media)")
			RespondError(w, r, http.StatusUnauthorized, ErrUnauthorized)
			return
		}

		reqCtx := context.WithValue(r.Context(), dpopRequestContextKey{}, r)
		result := s.TokenPrincipal(reqCtx, reqToken)
		if !result.OK() {
			logger.Warn().
				Str("event", "auth.rejected").
				Str("outcome", string(result.Outcome)).
				Msg("authentication rejected")
			writeAuthRejection(w, r, result)
			return
		}
		principal := result.Principal
		// Recorded from the source that actually produced the token, so downstream
		// code reads how this caller authenticated instead of guessing from headers.
		if principal != nil {
			principal.Credential = auth.SourceCredentialKind(authSource)
		}

		// State metadata for a successful request, set before the handler
		// writes anything. Idempotent and not retry-relevant: a client that
		// ignores it behaves exactly as before.
		setDeviceStateHeader(w, result)

		// Token is valid - add principal to context
		ctx := auth.WithPrincipal(r.Context(), principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// CreateSession creates a secure HTTP-only session cookie exchange for the provided Bearer or DPoP token.
// POST /api/v3/auth/session
// Requires Authentication (via Header) to be successful first.
func (s *Server) CreateSession(w http.ResponseWriter, r *http.Request) {
	// 1. Re-extract the token that was successfully validated.
	reqToken, authSource := s.extractTokenDetailedWithLegacyPolicy(r, false)

	// The client MUST present a valid Bearer or DPoP token to exchange it for a session cookie.
	if reqToken == "" || (authSource != auth.BearerSource && authSource != auth.DPoPSource) {
		RespondError(w, r, http.StatusUnauthorized, ErrUnauthorized)
		return
	}
	principal := auth.PrincipalFromContext(r.Context())
	if principal == nil {
		reqCtx := context.WithValue(r.Context(), dpopRequestContextKey{}, r)
		res := s.TokenPrincipal(reqCtx, reqToken)
		if !res.OK() {
			RespondError(w, r, http.StatusUnauthorized, ErrUnauthorized)
			return
		}
		principal = res.Principal
	}

	sessionID, err := s.issueCookieSession(w, r, reqToken, principal, s.authSessionTTLOrDefault())
	if err != nil {
		if errors.Is(err, ErrHTTPSRequired) {
			RespondError(w, r, http.StatusBadRequest, ErrHTTPSRequired, "session exchange requires HTTPS or a trusted HTTPS proxy; plain HTTP is only accepted from loopback")
			return
		}
		RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, "failed to create session")
		return
	}

	w.Header().Set("Cache-Control", "no-store, no-cache, private")
	writeJSON(w, http.StatusOK, map[string]any{
		"session_id": sessionID,
		"sessionId":  sessionID,
		"cookie":     sessionCookieName,
		"path":       "/api/v3/",
		"expires_in": int(s.authSessionTTLOrDefault() / time.Second),
	})
}

// DeleteSession clears the auth session cookie and any active household unlock cookie.
// DELETE /api/v3/auth/session
func (s *Server) DeleteSession(w http.ResponseWriter, r *http.Request, params DeleteSessionParams) {
	_ = params
	profile := householdProfileFromRequestContext(r)
	accessState := householdAccessStateFromRequestContext(r)
	if accessState.PinConfigured && profile.Kind == householddomain.ProfileKindChild && !accessState.Unlocked {
		writeRegisteredProblem(w, r, http.StatusForbidden, "household/pin_required", "Household Pin Required", problemcode.CodeForbidden, "Logging out from the child profile requires the configured household pin", nil)
		return
	}

	if existingCookie, err := r.Cookie(sessionCookieName); err == nil {
		s.deleteAuthSession(existingCookie.Value)
	}
	if existingCookie, err := r.Cookie(householdUnlockCookieName); err == nil {
		s.deleteHouseholdUnlock(existingCookie.Value)
	}

	secure := s.requestIsHTTPS(r)
	clearServerCookie(w, sessionCookieName, "/api/v3/", secure)
	clearServerCookie(w, householdUnlockCookieName, "/api/v3/", secure)
	w.WriteHeader(http.StatusNoContent)
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
	if token, ok := s.authSessionStoreOrDefault().ResolveSessionToken(sessionID); ok && token != "" {
		return token, true
	}
	if idSvc := s.getIdentityService(); idSvc != nil {
		if sess, _, err := idSvc.ValidateWebSession(context.Background(), sessionID, "", ""); err == nil && sess != nil {
			return sessionID, true
		}
	}
	return "", false
}

func (s *Server) deleteAuthSession(sessionID string) {
	s.authSessionStoreOrDefault().InvalidateSession(sessionID)
	if idSvc := s.getIdentityService(); idSvc != nil {
		_ = idSvc.RevokeWebSession(context.Background(), sessionID)
	}
}

func (s *Server) issueCookieSession(w http.ResponseWriter, r *http.Request, token string, principal *auth.Principal, ttl time.Duration) (string, error) {
	if strings.TrimSpace(token) == "" && principal == nil {
		return "", auth.ErrInvalidSessionToken
	}
	if ttl <= 0 {
		return "", auth.ErrInvalidSessionTTL
	}

	effectiveHTTPS := s.requestIsHTTPS(r)
	if !effectiveHTTPS && !requestRemoteIsLoopback(r) {
		return "", ErrHTTPSRequired
	}

	if existingCookie, err := r.Cookie(sessionCookieName); err == nil {
		s.deleteAuthSession(existingCookie.Value)
	}

	sessionID, err := s.authSessionStoreOrDefault().CreateSessionWithPrincipal(token, principal, ttl)
	if err != nil {
		return "", err
	}

	maxAge := int(ttl / time.Second)
	if maxAge <= 0 {
		maxAge = sessionCookieMaxAgeSeconds
	}
	setServerCookie(w, &http.Cookie{ // #nosec G124 -- HttpOnly+SameSite=Lax set; Secure=effectiveHTTPS is request-derived, opaque to gosec
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     "/api/v3/",
		HttpOnly: true,
		Secure:   effectiveHTTPS,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	})
	return sessionID, nil
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

	remoteIP := requestRemoteIP(r)
	if remoteIP == nil {
		return false
	}
	if remoteIP.IsLoopback() || remoteIP.IsPrivate() {
		return true
	}

	trustedProxies, err := middleware.ParseCIDRs(splitCSVNonEmpty(strings.TrimSpace(s.GetConfig().TrustedProxies)))
	if err != nil {
		log.L().Warn().Err(err).Msg("invalid trusted proxies configuration for auth session exchange")
		return false
	}
	return middleware.IsIPAllowed(remoteIP, trustedProxies)
}

func requestRemoteIsLoopback(r *http.Request) bool {
	ip := requestRemoteIP(r)
	return ip != nil && ip.IsLoopback()
}

var tailscaleSubnet = &net.IPNet{
	IP:   net.IPv4(100, 64, 0, 0),
	Mask: net.CIDRMask(10, 32),
}

func requestRemoteIsPrivateOrLoopback(r *http.Request) bool {
	ip := requestRemoteIP(r)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || tailscaleSubnet.Contains(ip))
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

func householdProfileFromRequestContext(r *http.Request) householddomain.Profile {
	if r == nil {
		return householddomain.CreateDefaultProfile()
	}
	if profile := householddomain.ProfileFromContext(r.Context()); profile != nil {
		return *profile
	}
	return householddomain.CreateDefaultProfile()
}

func householdAccessStateFromRequestContext(r *http.Request) householddomain.AccessState {
	if r == nil {
		return householddomain.AccessState{}
	}
	return householddomain.AccessStateFromContext(r.Context())
}

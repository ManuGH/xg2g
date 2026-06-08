// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

const (
	sessionCookieName = "xg2g_session"
	legacyCookieName  = "X-API-Token"

	// Source descriptions returned by ExtractTokenDetailedWithOptions. The v3
	// auth middleware enforces the media-endpoint session-cookie invariant by
	// comparing against SessionCookieSource EXACTLY — never by substring, since
	// the legacy LegacyCookieSource ("X-API-Token cookie") also contains the
	// word "cookie" and would otherwise satisfy the check.
	BearerSource        = "Authorization header (Bearer)"
	SessionCookieSource = "xg2g_session cookie"
	LegacyHeaderSource  = "X-API-Token header"
	LegacyCookieSource  = "X-API-Token cookie"
)

// TokenExtractOptions controls accepted token sources during request parsing.
type TokenExtractOptions struct {
	AllowLegacySources  bool
	ResolveSessionToken func(sessionID string) (string, bool)
}

// ExtractToken retrieves the API token from the request.
// It enforces strict parity with the API's extraction logic.
func ExtractToken(r *http.Request) string {
	t, _ := ExtractTokenDetailed(r)
	return t
}

// ExtractTokenDetailed retrieves the API token and its source from the request.
// Legacy sources are disabled by default.
// Sources:
// 1. Authorization: Bearer <token>
// 2. Cookie: xg2g_session
// 3. Header: X-API-Token (Legacy, optional)
// 4. Cookie: X-API-Token (Legacy, optional and last resort)
func ExtractTokenDetailed(r *http.Request) (string, string) {
	return ExtractTokenDetailedWithOptions(r, TokenExtractOptions{
		AllowLegacySources: false,
	})
}

// ExtractTokenDetailedWithOptions retrieves the API token and source with configurable legacy-source handling.
func ExtractTokenDetailedWithOptions(r *http.Request, opts TokenExtractOptions) (string, string) {
	if r == nil {
		return "", ""
	}

	// 1. Authorization Header
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(auth[7:]), BearerSource
	}

	// 2. Cookie
	if sessionID := ExtractSessionToken(r); sessionID != "" {
		if opts.ResolveSessionToken != nil {
			if token, ok := opts.ResolveSessionToken(sessionID); ok && token != "" {
				return token, SessionCookieSource
			}
			return "", ""
		}
		return sessionID, SessionCookieSource
	}

	if !opts.AllowLegacySources {
		return "", ""
	}

	// 3. Legacy Header
	if t := r.Header.Get("X-API-Token"); t != "" {
		return t, LegacyHeaderSource
	}

	// 4. Check for legacy Cookie (X-API-Token) as last resort
	if c, err := r.Cookie(legacyCookieName); err == nil && c.Value != "" {
		return c.Value, LegacyCookieSource
	}

	return "", ""
}

// ExtractSessionToken retrieves only the session cookie token (xg2g_session).
func ExtractSessionToken(r *http.Request) string {
	if r == nil {
		return ""
	}
	if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
		return c.Value
	}
	return ""
}

// AuthorizeToken returns true if got matches expected using constant-time comparison.
// Empty tokens are always treated as unauthorized.
func AuthorizeToken(got, expected string) bool {
	if strings.TrimSpace(expected) == "" || got == "" {
		return false
	}
	if len(got) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}

// AuthorizeRequest extracts a token from r and validates it against expectedToken.
func AuthorizeRequest(r *http.Request, expectedToken string) bool {
	if r == nil {
		return false
	}
	return AuthorizeToken(ExtractToken(r), expectedToken)
}

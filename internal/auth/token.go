// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/ManuGH/xg2g/internal/log"
)

// ExtractToken retrieves the API token from the request.
// It enforces strict parity with the API's extraction logic.
// 1. Authorization: Bearer <token>
// 2. Cookie: xg2g_session
// 3. Header: X-API-Token (Legacy)
// 4. Query: ?token= (If enabled)
// 5. Cookie: X-API-Token (Legacy, last resort)
func ExtractToken(r *http.Request, allowQuery bool) string {
	// 1. Authorization Header
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(auth[7:])
	}

	// 2. Cookie
	if c, err := r.Cookie("xg2g_session"); err == nil && c.Value != "" {
		return c.Value
	}

	// 3. Legacy Header
	if t := r.Header.Get("X-API-Token"); t != "" {
		return t
	}

	// 4. Query Parameter (if allowed) - DEPRECATED
	if allowQuery {
		if t := r.URL.Query().Get("token"); t != "" {
			// Log deprecation warning
			log.L().Warn().
				Str("path", r.URL.Path).
				Str("remote_addr", r.RemoteAddr).
				Msg("DEPRECATED: Query parameter authentication is insecure (tokens logged in proxies/browsers) and will be removed in v3.0. Use Authorization header instead.")
			return t
		}
	}

	// 5. Check for legacy Cookie (X-API-Token) as last resort
	if c, err := r.Cookie("X-API-Token"); err == nil && c.Value != "" {
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
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}

// AuthorizeRequest extracts a token from r and validates it against expectedToken.
func AuthorizeRequest(r *http.Request, expectedToken string, allowQuery bool) bool {
	if r == nil {
		return false
	}
	return AuthorizeToken(ExtractToken(r, allowQuery), expectedToken)
}

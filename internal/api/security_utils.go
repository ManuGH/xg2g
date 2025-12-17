package api

import (
	"net/http"
	"regexp"
	"strings"
)

var segmentAllowList = regexp.MustCompile(`^[a-zA-Z0-9._-]+\.(ts|m4s|mp4|aac|vtt|m3u8)$`)

// extractToken retrieves the API token from the request using a unified strategy:
// 1. Authorization: Bearer <token>
// 2. Cookie: xg2g_session
// 3. Header: X-API-Token (Legacy)
// 4. Query: ?token= (Optional, for streams)
func extractToken(r *http.Request, allowQuery bool) string {
	// 1. Authorization Header
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
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

	// 4. Query Parameter (if allowed)
	if allowQuery {
		if t := r.URL.Query().Get("token"); t != "" {
			return t
		}
	}

	// 5. Check for legacy Cookie (X-API-Token) as last resort
	if c, err := r.Cookie("X-API-Token"); err == nil && c.Value != "" {
		return c.Value
	}

	return ""
}

// SPDX-License-Identifier: MIT

package api

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/ManuGH/xg2g/internal/log"
)

// ctxPrincipalKey is used to store the authenticated principal in context
//nolint:unused // Legacy types - kept for future use
type ctxPrincipalKey struct{}

// Note: securityHeaders is defined in middleware.go

// authenticate is a middleware that validates API tokens
// If no token is configured in the environment, authentication is disabled (open access)
//nolint:unused // Legacy auth function
func authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get token from Server config would be better, but for now use env
		// This matches the existing AuthMiddleware behavior
		token := r.Context().Value(serverConfigKey{})
		if token == nil || token.(string) == "" {
			// No token configured - authentication disabled
			next.ServeHTTP(w, r)
			return
		}

		reqToken := parseBearer(r.Header.Get("Authorization"))
		if reqToken == "" {
			// Fallback to X-API-Token header for backward compatibility
			reqToken = r.Header.Get("X-API-Token")
		}

		logger := log.FromContext(r.Context()).With().Str("component", "auth").Logger()

		if reqToken == "" {
			logger.Warn().Str("event", "auth.missing_header").Msg("authorization header missing")
			writeUnauthorized(w)
			return
		}

		// Use constant-time comparison to prevent timing attacks
		if subtle.ConstantTimeCompare([]byte(reqToken), []byte(token.(string))) != 1 {
			logger.Warn().Str("event", "auth.invalid_token").Msg("invalid api token")
			writeUnauthorized(w)
			return
		}

		// Token is valid - add principal to context
		ctx := context.WithValue(r.Context(), ctxPrincipalKey{}, "authenticated")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// parseBearer extracts the token from a "Bearer <token>" authorization header
//nolint:unused // Helper function - kept for future use
func parseBearer(auth string) string {
	const prefix = "Bearer "
	if strings.HasPrefix(auth, prefix) {
		return strings.TrimSpace(auth[len(prefix):])
	}
	return ""
}

// principalFrom extracts the authenticated principal from the request context
//nolint:unused // Helper function - kept for future use
func principalFrom(ctx context.Context) string {
	if p := ctx.Value(ctxPrincipalKey{}); p != nil {
		return p.(string)
	}
	return ""
}

// serverConfigKey is used to pass server config through context
//nolint:unused // Legacy type - kept for future use
type serverConfigKey struct{}

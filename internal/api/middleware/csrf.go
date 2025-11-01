// SPDX-License-Identifier: MIT

package middleware

import (
	"net/http"
	"net/url"
	"os"
	"strings"
)

// CSRFProtection creates a middleware that protects against Cross-Site Request Forgery (CSRF) attacks.
// It validates the Origin and Referer headers for state-changing requests (POST, PUT, DELETE, PATCH).
//
// The middleware checks:
// 1. Origin header matches allowed origins (preferred, per Fetch Standard)
// 2. Referer header matches allowed origins (fallback for older browsers)
// 3. Allows same-origin requests by default
//
// Configuration via environment variable:
//   - XG2G_ALLOWED_ORIGINS: Comma-separated list of allowed origins (e.g., "http://localhost:8080,https://example.com")
//   - If not set, only same-origin requests are allowed
//
// Example usage:
//
//	r.Use(middleware.CSRFProtection())
func CSRFProtection() func(http.Handler) http.Handler {
	// Load allowed origins from environment
	allowedOrigins := getAllowedOrigins()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only check state-changing methods (GET and HEAD are safe)
			if r.Method != http.MethodPost && r.Method != http.MethodPut &&
				r.Method != http.MethodDelete && r.Method != http.MethodPatch {
				next.ServeHTTP(w, r)
				return
			}

			// Extract request origin from Origin or Referer header
			requestOrigin := getRequestOrigin(r)
			if requestOrigin == "" {
				// No origin information available - reject for safety
				http.Error(w, "Forbidden: Missing origin information", http.StatusForbidden)
				return
			}

			// Check if request origin is allowed
			if !isOriginAllowed(requestOrigin, allowedOrigins, r) {
				http.Error(w, "Forbidden: Cross-origin request not allowed", http.StatusForbidden)
				return
			}

			// Origin is valid - proceed with request
			next.ServeHTTP(w, r)
		})
	}
}

// getAllowedOrigins loads allowed origins from XG2G_ALLOWED_ORIGINS environment variable.
// Returns a map for O(1) lookup performance.
func getAllowedOrigins() map[string]bool {
	originsEnv := os.Getenv("XG2G_ALLOWED_ORIGINS")
	if originsEnv == "" {
		return nil // nil means only same-origin requests allowed
	}

	origins := make(map[string]bool)
	for _, origin := range strings.Split(originsEnv, ",") {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			// Normalize origin (remove trailing slash)
			origin = strings.TrimSuffix(origin, "/")
			origins[origin] = true
		}
	}

	return origins
}

// getRequestOrigin extracts the origin from the request.
// It checks Origin header first (preferred), then falls back to Referer.
func getRequestOrigin(r *http.Request) string {
	// 1. Check Origin header (standard for CORS and modern browsers)
	origin := r.Header.Get("Origin")
	if origin != "" {
		return strings.TrimSuffix(origin, "/")
	}

	// 2. Fallback to Referer header (for older browsers or non-CORS requests)
	referer := r.Header.Get("Referer")
	if referer == "" {
		return ""
	}

	// Extract origin from referer URL
	refererURL, err := url.Parse(referer)
	if err != nil {
		return ""
	}

	// Reconstruct origin from referer (scheme + host)
	refererOrigin := refererURL.Scheme + "://" + refererURL.Host
	return strings.TrimSuffix(refererOrigin, "/")
}

// isOriginAllowed checks if the request origin is allowed.
// It implements same-origin policy with configurable allowed origins.
func isOriginAllowed(requestOrigin string, allowedOrigins map[string]bool, r *http.Request) bool {
	// If no allowed origins configured, use same-origin policy
	if allowedOrigins == nil {
		return isSameOrigin(requestOrigin, r)
	}

	// Check if origin is in allowed list
	if allowedOrigins[requestOrigin] {
		return true
	}

	// Always allow same-origin requests even if not in allowed list
	return isSameOrigin(requestOrigin, r)
}

// isSameOrigin checks if the request origin matches the request's target origin.
func isSameOrigin(requestOrigin string, r *http.Request) bool {
	// Construct expected origin from request
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}

	// Check X-Forwarded-Proto for proxy scenarios
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}

	// Use Host header (includes port if non-standard)
	host := r.Host
	if host == "" {
		return false
	}

	expectedOrigin := scheme + "://" + host
	return requestOrigin == expectedOrigin
}

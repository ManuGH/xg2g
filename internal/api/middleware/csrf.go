// SPDX-License-Identifier: MIT

package middleware

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
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
		if normalized, ok := normalizeOrigin(origin); ok {
			origins[normalized] = true
		}
	}

	return origins
}

// getRequestOrigin extracts the origin from the request.
// It checks Origin header first (preferred), then falls back to Referer.
func getRequestOrigin(r *http.Request) string {
	// 1. Check Origin header (standard for CORS and modern browsers)
	if origin := r.Header.Get("Origin"); origin != "" {
		normalized, ok := normalizeOrigin(origin)
		if !ok {
			return ""
		}
		return normalized
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
	scheme := strings.ToLower(strings.TrimSpace(refererURL.Scheme))
	if (scheme != "http" && scheme != "https") || refererURL.Host == "" {
		return ""
	}

	// Reconstruct origin from referer (scheme + host)
	normalized, ok := normalizeOrigin(scheme + "://" + refererURL.Host)
	if !ok {
		return ""
	}
	return normalized
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
	if proto := strings.ToLower(firstHeaderValue(r.Header.Get("X-Forwarded-Proto"))); proto == "http" || proto == "https" {
		scheme = proto
	}

	// Use Host header (includes port if non-standard)
	expectedOrigin, ok := normalizeOrigin(scheme + "://" + r.Host)
	if !ok {
		return false
	}

	return requestOrigin == expectedOrigin
}

func firstHeaderValue(raw string) string {
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, ",")
	return strings.TrimSpace(parts[0])
}

func normalizeOrigin(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}

	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", false
	}
	if u.Path != "" && u.Path != "/" {
		return "", false
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}

	host, port, ok := parseAndValidateHostPort(u.Host)
	if !ok {
		return "", false
	}

	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}

	return scheme + "://" + normalizeAuthority(host, port), true
}

func normalizeAuthority(host, port string) string {
	if port == "" {
		if strings.Contains(host, ":") {
			return "[" + host + "]"
		}
		return host
	}
	return net.JoinHostPort(host, port)
}

func parseAndValidateHostPort(hostport string) (string, string, bool) {
	hostport = strings.TrimSpace(hostport)
	if hostport == "" || strings.ContainsAny(hostport, "/\\@ \t\r\n") {
		return "", "", false
	}

	host := hostport
	port := ""

	if h, p, err := net.SplitHostPort(hostport); err == nil {
		host = h
		port = p
	} else if strings.Count(hostport, ":") == 1 && !strings.HasPrefix(hostport, "[") {
		parts := strings.SplitN(hostport, ":", 2)
		if len(parts) == 2 {
			host = parts[0]
			port = parts[1]
		}
	}

	host = strings.Trim(host, "[]")
	if !isValidHost(host) {
		return "", "", false
	}
	if port != "" {
		p, err := strconv.Atoi(port)
		if err != nil || p < 1 || p > 65535 {
			return "", "", false
		}
	}

	return host, port, true
}

func isValidHost(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return true
	}
	if strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") || strings.Contains(host, "..") {
		return false
	}

	labels := strings.Split(host, ".")
	for _, label := range labels {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, c := range label {
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	return true
}

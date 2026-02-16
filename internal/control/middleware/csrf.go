package middleware

import (
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/ManuGH/xg2g/internal/control/http/problem"
)

// CSRFProtection creates a middleware that protects against Cross-Site Request Forgery (CSRF) attacks.
// It validates the Origin and Referer headers for state-changing requests (POST, PUT, DELETE, PATCH).
//
// The middleware enforces a strict fail-closed policy:
// 1. Safe methods (GET, HEAD, OPTIONS) are allowed.
// 2. Unsafe methods REQUIRE a valid Origin or Referer.
// 3. If no allowedOrigins configured: Only strict same-origin (no proxies) allowed.
// 4. If allowedOrigins configured: Explicit origins or strict same-origin allowed.
func CSRFProtection(allowedOrigins []string) func(http.Handler) http.Handler {
	// Create map for O(1) lookup
	var originsMap map[string]bool
	if len(allowedOrigins) > 0 {
		originsMap = make(map[string]bool)
		for _, origin := range allowedOrigins {
			trimmed := strings.TrimSpace(origin)
			if trimmed == "" {
				continue
			}
			if trimmed == "*" {
				originsMap["*"] = true
				continue
			}
			if normalized, ok := normalizeOrigin(trimmed); ok {
				originsMap[normalized] = true
			}
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. Safe methods are always allowed
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			// 2. Extract request origin (MUST be present for unsafe methods)
			requestOrigin := getRequestOrigin(r)
			if requestOrigin == "" {
				writeCSRFProblem(w, r, "Missing origin or referer header")
				return
			}

			// 3. Check if origin is allowed
			if !isOriginAllowed(requestOrigin, originsMap, r) {
				writeCSRFProblem(w, r, "CSRF check failed: origin not trusted")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func writeCSRFProblem(w http.ResponseWriter, r *http.Request, detail string) {
	problem.Write(w, r, http.StatusForbidden, "auth/csrf", "Forbidden", "CSRF_FORBIDDEN", detail, nil)
}

// getRequestOrigin extracts the origin from the request headers.
// It checks Origin header first, then falls back to Referer.
func getRequestOrigin(r *http.Request) string {
	if normalizedOrigin, ok := normalizeOrigin(r.Header.Get("Origin")); ok {
		return normalizedOrigin
	}

	referer := r.Header.Get("Referer")
	if referer == "" {
		return ""
	}

	refererURL, err := url.Parse(referer)
	if err != nil {
		return ""
	}

	if refererURL.Scheme == "" || refererURL.Host == "" {
		return ""
	}

	refererOrigin := refererURL.Scheme + "://" + refererURL.Host
	normalizedRefererOrigin, ok := normalizeOrigin(refererOrigin)
	if !ok {
		return ""
	}
	return normalizedRefererOrigin
}

// isOriginAllowed implements the core CSRF decision logic (Option A).
func isOriginAllowed(requestOrigin string, allowedOrigins map[string]bool, r *http.Request) bool {
	// 1. If explicitly allowed in config (including wildcard)
	if allowedOrigins != nil {
		if allowedOrigins["*"] || allowedOrigins[requestOrigin] {
			return true
		}
	}

	// 2. Fallback to strict same-origin check
	// Same-origin is only trusted if NO proxy headers are present.
	if hasProxyHeaders(r) {
		return false
	}

	return requestOrigin == getStrictSameOrigin(r)
}

// hasProxyHeaders checks for the presence of common proxy/forwarding headers.
// Blindly trusting these without a trust-boundary is a security bug.
func hasProxyHeaders(r *http.Request) bool {
	headers := []string{
		"Forwarded",
		"X-Forwarded-For",
		"X-Forwarded-Host",
		"X-Forwarded-Proto",
		"X-Forwarded-Server",
	}
	for _, h := range headers {
		if r.Header.Get(h) != "" {
			return true
		}
	}
	return false
}

// getStrictSameOrigin reconstructs the expected origin from the local host name
// and connection state, ignoring all forwarding headers.
func getStrictSameOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}

	host := r.Host
	if host == "" {
		return ""
	}

	origin, ok := normalizeOrigin(scheme + "://" + host)
	if !ok {
		return ""
	}
	return origin
}

func normalizeOrigin(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", false
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}

	host := strings.ToLower(parsed.Hostname())
	if host == "" || strings.ContainsAny(host, " \t\r\n/@\\") {
		return "", false
	}

	port := parsed.Port()
	if port != "" {
		portNum, err := strconv.Atoi(port)
		if err != nil || portNum < 1 || portNum > 65535 {
			return "", false
		}
	}

	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}

	authority := host
	if ip := net.ParseIP(host); ip != nil && strings.Contains(host, ":") {
		authority = "[" + host + "]"
	}
	if port != "" {
		authority = net.JoinHostPort(host, port)
	}

	return scheme + "://" + authority, true
}

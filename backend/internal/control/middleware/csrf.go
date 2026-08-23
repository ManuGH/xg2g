package middleware

import (
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/ManuGH/xg2g/internal/control/auth"
	"github.com/ManuGH/xg2g/internal/control/http/problem"
)

// CSRFProtection creates a middleware that protects against Cross-Site Request Forgery (CSRF) attacks.
// It validates the Origin and Referer headers for state-changing requests (POST, PUT, DELETE, PATCH).
//
// The middleware enforces a strict fail-closed policy:
//  1. Safe methods (GET, HEAD, OPTIONS) are allowed.
//  2. Unsafe methods carrying an ambient credential - one the browser attaches on
//     its own - REQUIRE a valid Origin or Referer. Those are the only requests a
//     foreign page can forge, because they are the only ones that arrive with the
//     victim's credentials already attached.
//  3. If no allowedOrigins configured: Only strict same-origin (no proxies) allowed.
//
// Requests presenting no ambient credential are passed to authentication, which
// refuses them unless they carry a valid credential of their own. Taking that
// branch grants nothing: it is authentication, not this middleware, that decides
// whether a caller may act.
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

			// 2. Forgery needs a credential the browser attaches on its own.
			//
			// A cross-site page can make a browser send this request, but it cannot
			// put credentials on it that it does not already hold. It rides on the
			// ones the browser adds unprompted - cookies. A request that presents
			// *only* a credential the caller had to set deliberately carries nothing
			// a foreign page could have supplied, so there is nothing here to forge.
			//
			// Only that case is exempt. A request presenting no credential at all
			// keeps the full check: the absence of a credential says nothing about
			// the caller, and reading it as trustworthy would weaken every endpoint
			// reachable without authentication.
			//
			// This is not "an Authorization header switches CSRF off". Nothing is
			// granted by taking this branch: a request that presents no valid
			// credential is refused by authentication a few frames further on, and
			// omitting the cookie unlocks nothing that having it would have. The
			// browser paths keep the full check, which is where forgery is possible
			// at all.
			//
			// It is also what lets the native app reach the API over a LAN address,
			// an HTTPS domain or a VPN without any of them being written down: its
			// credentials are device-bound and request-bound, so its security never
			// rested on which URL it happened to arrive by.
			if auth.RequestCredentialKind(r) == auth.CredentialExplicit {
				next.ServeHTTP(w, r)
				return
			}

			// 3. Extract request origin (MUST be present for unsafe methods)
			requestOrigin := getRequestOrigin(r)
			if requestOrigin == "" {
				writeCSRFProblem(w, r, "Missing origin or referer header")
				return
			}

			// 4. Check if origin is allowed
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

func isOriginAllowed(requestOrigin string, allowedOrigins map[string]bool, r *http.Request) bool {
	// 1. If explicitly allowed in config.
	// Wildcard origins are intentionally ignored for CSRF decisions on unsafe
	// methods because they would trust any cross-site origin.
	if allowedOrigins != nil {
		if allowedOrigins[requestOrigin] {
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

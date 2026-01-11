// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package middleware

import (
	"net"
	"net/http"
	"strings"
)

// DefaultCSP allows loading styles/images from common CDNs for Plyr,
// and allows unsafe-inline for React/Plyr dynamic styles.
const DefaultCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https://cdn.plyr.io; media-src 'self' blob: data: https://cdn.plyr.io; connect-src 'self' https://cdn.plyr.io; frame-ancestors 'none'"

// SecurityHeaders returns a middleware that adds common security headers to all responses.
// It requires trustedProxies to safely evaluate X-Forwarded-Proto headers.
func SecurityHeaders(csp string, trustedProxies []*net.IPNet) func(http.Handler) http.Handler {
	if csp == "" {
		csp = DefaultCSP
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Strict Transport Security (HSTS)
			// Only honor X-Forwarded-Proto if the remote IP is a trusted proxy.
			isHTTPS := r.TLS != nil
			if !isHTTPS {
				proto := r.Header.Get("X-Forwarded-Proto")
				if strings.EqualFold(proto, "https") {
					// Check trust
					ipStr, _, _ := net.SplitHostPort(r.RemoteAddr)
					if ipStr == "" {
						ipStr = r.RemoteAddr
					}
					ip := net.ParseIP(ipStr)
					if ip != nil && IsIPAllowed(ip, trustedProxies) {
						isHTTPS = true
					}
				}
			}

			if isHTTPS {
				w.Header().Set("Strict-Transport-Security", "max-age=15552000; includeSubDomains")
			}

			// Content Security Policy (CSP)
			w.Header().Set("Content-Security-Policy", csp)

			// X-Content-Type-Options
			w.Header().Set("X-Content-Type-Options", "nosniff")

			// X-Frame-Options
			w.Header().Set("X-Frame-Options", "DENY")

			// Referrer-Policy
			w.Header().Set("Referrer-Policy", "no-referrer")

			next.ServeHTTP(w, r)
		})
	}
}

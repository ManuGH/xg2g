// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// SPDX-License-Identifier: MIT

package middleware

import (
	"net/http"
	"strings"
)

// DefaultCSP allows loading styles/images from common CDNs for Plyr,
// and allows unsafe-inline for React/Plyr dynamic styles.
const DefaultCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https://cdn.plyr.io; media-src 'self' blob: data: https://cdn.plyr.io; connect-src 'self' https://cdn.plyr.io; frame-ancestors 'none'"

// SecurityHeaders returns a middleware that adds common security headers to all responses.
func SecurityHeaders(csp string) func(http.Handler) http.Handler {
	if csp == "" {
		csp = DefaultCSP
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Strict Transport Security (HSTS)
			if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
				w.Header().Set("Strict-Transport-Security", "max-age=15552000; includeSubDomains")
			}

			// Content Security Policy (CSP)
			w.Header().Set("Content-Security-Policy", csp)

			// X-Content-Type-Options
			w.Header().Set("X-Content-Type-Options", "nosniff")

			// X-Frame-Options
			w.Header().Set("X-Frame-Options", "DENY")

			// X-XSS-Protection (legacy header for older browsers)
			w.Header().Set("X-XSS-Protection", "1; mode=block")

			// Referrer-Policy
			w.Header().Set("Referrer-Policy", "no-referrer")

			next.ServeHTTP(w, r)
		})
	}
}

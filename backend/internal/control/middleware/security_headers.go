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

// DefaultCSP keeps the app fully same-origin: no external script/style/img/
// media/connect origins (the previous https://cdn.plyr.io allowance was dead —
// the player is hls.js, not Plyr). style-src allows 'unsafe-inline' for React's
// inline styles only; inline <script> remains disallowed. font-src allows
// 'self' and data: because the self-hosted variable fonts are bundled and some
// are inlined as data: URIs — without it the browser blocks them and falls back
// to system fonts everywhere.
const DefaultCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; font-src 'self' data:; img-src 'self' data: blob:; media-src 'self' blob: data:; connect-src 'self'; frame-ancestors 'none'"

// DefaultPermissionsPolicy denies powerful features the app does not use.
const DefaultPermissionsPolicy = "camera=(), microphone=(), geolocation=(), payment=(), usb=()"

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
				w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
			}

			// Content Security Policy (CSP)
			w.Header().Set("Content-Security-Policy", csp)

			// X-Content-Type-Options
			w.Header().Set("X-Content-Type-Options", "nosniff")

			// X-Frame-Options
			w.Header().Set("X-Frame-Options", "DENY")

			// Referrer-Policy
			w.Header().Set("Referrer-Policy", "no-referrer")

			// Permissions-Policy: deny powerful features the app does not use.
			w.Header().Set("Permissions-Policy", DefaultPermissionsPolicy)

			// Cross-origin opener policy. The app is fully same-origin (see
			// DefaultCSP: no external script/style/img/media/connect origins, and
			// the web UI is served by this same backend), so COOP severs
			// window.opener links to cross-origin openers.
			//
			// Cross-Origin-Resource-Policy is deliberately absent from this global
			// middleware: the backend serves public static resources (e.g. picon
			// logos under /logos/{filename}) that cross-origin clients load via
			// <img> tags. Setting CORP: same-origin globally would block those
			// no-cors cross-origin loads. Individual handlers or route groups that
			// need to restrict resource embedding should set CORP explicitly.
			//
			// Cross-Origin-Embedder-Policy is also omitted: require-corp would
			// force every cross-origin media/blob the player touches to carry
			// CORP/CORS and would break HLS playback for no real gain here.
			w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")

			next.ServeHTTP(w, r)
		})
	}
}

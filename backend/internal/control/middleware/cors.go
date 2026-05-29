// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package middleware

import (
	"net/http"
	"strings"
)

// CORS returns a middleware that sets Cross-Origin Resource Sharing headers.
// It supports a strict allowed origins list.
func CORS(allowedOrigins []string, allowCredentials bool) func(http.Handler) http.Handler {
	// Create map for O(1) lookup
	allowed := make(map[string]bool)
	for _, origin := range allowedOrigins {
		allowed[origin] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// Logic:
			// 1. If origin matches allowed list -> Allow
			// 2. If valid origin but not in list -> Block (don't set headers)
			// 3. If no origin header -> Allow (direct tools, same-origin)
			// However, for browser security, we only set Allow-Origin if Origin header is present.

			// Special case: "*" in configuration allows all origins (with optional credentials).
			allowAll := allowed["*"]

			// Only emit CORS response headers when the origin is actually allowed.
			// Access-Control-Allow-Methods/Headers/Expose-Headers/Max-Age are only
			// meaningful alongside an allowed Allow-Origin; emitting them for
			// disallowed or origin-less callers leaks the API's method/header
			// surface and is what "Allow-* on disallowed origins" flagged.
			if origin != "" && (allowAll || allowed[origin]) {
				if allowAll {
					// Wildcard: emit * instead of reflecting the request Origin.
					// Per the Fetch spec, Access-Control-Allow-Origin: * cannot carry
					// Access-Control-Allow-Credentials: true.  When allowCredentials is
					// true alongside a wildcard origin list, we suppress the credentials
					// header — reflecting the actual origin would allow any arbitrary
					// website to make credentialed cross-origin requests, which is a
					// classic CORS misconfiguration.
					w.Header().Set("Access-Control-Allow-Origin", "*")
				} else {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					if allowCredentials {
						w.Header().Set("Access-Control-Allow-Credentials", "true")
					}
				}
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, DELETE, PUT, PATCH")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Request-ID, X-API-Token, Authorization")
				w.Header().Set("Access-Control-Expose-Headers", "Retry-After, Content-Length, Date, X-Request-ID")
				w.Header().Set("Access-Control-Max-Age", "600")
			}

			// Always set Vary: Origin to prevent cache poisoning/confusion
			vary := w.Header().Get("Vary")
			if vary == "" {
				w.Header().Set("Vary", "Origin, Access-Control-Request-Method, Access-Control-Request-Headers")
			} else {
				if !strings.Contains(vary, "Origin") {
					w.Header().Set("Vary", vary+", Origin")
				}
			}

			if r.Method == http.MethodOptions {
				w.Header().Set("Allow", "GET, POST, OPTIONS, DELETE, PUT, PATCH")
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

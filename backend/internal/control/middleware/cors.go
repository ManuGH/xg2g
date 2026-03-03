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

			if origin != "" {
				if allowAll || allowed[origin] {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					if allowCredentials {
						w.Header().Set("Access-Control-Allow-Credentials", "true")
					}
				}
				// If not allowed, we don't set the header, browser blocks it.
			}

			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, DELETE, PUT, PATCH")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Request-ID, X-API-Token, Authorization")
			w.Header().Set("Access-Control-Expose-Headers", "Retry-After, Content-Length, Date, X-Request-ID")
			w.Header().Set("Access-Control-Max-Age", "600")

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

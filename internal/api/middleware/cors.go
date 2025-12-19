package middleware

import (
	"net/http"
)

// CORS returns a middleware that sets Cross-Origin Resource Sharing headers.
// It supports a strict allowed origins list.
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	// Create map for O(1) lookup
	allowed := make(map[string]bool)
	for _, origin := range allowedOrigins {
		allowed[origin] = true
	}

	// Add default development origins if empty (pragmatic default)
	// But only if no explicit origins provided. User can provide "*" to allow all.
	if len(allowedOrigins) == 0 {
		allowed["http://localhost:3000"] = true
		allowed["http://localhost:8080"] = true
		allowed["http://localhost:5173"] = true
		allowed["http://127.0.0.1:3000"] = true
		allowed["http://127.0.0.1:8080"] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// Logic:
			// 1. If origin matches allowed list -> Allow
			// 2. If valid origin but not in list -> Block (don't set headers)
			// 3. If no origin header -> Allow (direct tools, same-origin)

			// Special case: "*" in configuration allows all
			allowAll := allowed["*"]

			if origin != "" {
				if allowAll || allowed[origin] {
					w.Header().Set("Access-Control-Allow-Origin", origin)
				}
				// If not allowed, we don't set the header, browser blocks it.
			} else {
				// No origin header (curl, backend-to-backend).
				// We can default to "*" for non-browser clients to be friendly,
				// or just do nothing (same-origin policy applies).
				// Previous behavior was "*".
				w.Header().Set("Access-Control-Allow-Origin", "*")
			}

			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, DELETE, PUT, PATCH")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Request-ID, X-API-Token, Authorization")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.Header().Set("Vary", "Origin, Access-Control-Request-Method, Access-Control-Request-Headers")

			if r.Method == http.MethodOptions {
				w.Header().Set("Allow", "GET, POST, OPTIONS, DELETE, PUT, PATCH")
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

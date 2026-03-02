// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.
package middleware

import (
	"encoding/json"
	"net/http"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/ManuGH/xg2g/internal/log"
)

// Recoverer ensures that panics inside any downstream handler
// do not crash the process. It logs the panic with context and returns a 500 JSON.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				// Build stack trace
				buf := make([]byte, 8192)
				n := runtime.Stack(buf, false)
				stack := string(buf[:n])

				// Correlate with request ID if present
				reqID := log.RequestIDFromContext(r.Context())

				// Sanitize path label for logging
				pathLabel := r.URL.Path
				if !utf8.ValidString(pathLabel) {
					pathLabel = strings.ToValidUTF8(pathLabel, "")
				}

				// Log with structured fields
				logger := log.WithComponentFromContext(r.Context(), "panic-recovery")
				logger.Error().
					Str("event", "panic.recovered").
					Str("method", r.Method).
					Str("path", pathLabel).
					Str("remote_addr", r.RemoteAddr).
					Str("requestId", reqID).
					Interface("panic_value", rec).
					Str("stack_trace", stack).
					Msg("panic recovered in HTTP handler")

				// Best-effort JSON error response
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error":      "Internal server error",
					"requestId": reqID,
					"message":    "An unexpected error occurred. Please try again later.",
				})
			}
		}()

		next.ServeHTTP(w, r)
	})
}

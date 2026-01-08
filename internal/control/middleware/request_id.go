// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.
package middleware

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/ManuGH/xg2g/internal/log"
)

// RequestID generates or uses existing X-Request-ID header and propagates it through context and response.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get request ID from header or generate new one
		reqID := r.Header.Get("X-Request-ID")
		if reqID == "" {
			reqID = uuid.New().String()
		}

		// Set response header
		w.Header().Set("X-Request-ID", reqID)

		// Add request ID to context
		ctx := log.ContextWithRequestID(r.Context(), reqID)

		// Continue
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

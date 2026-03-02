// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.
package middleware

import (
	"net/http"

	"github.com/google/uuid"

	controlhttp "github.com/ManuGH/xg2g/internal/control/http"
	"github.com/ManuGH/xg2g/internal/log"
)

// RequestID adds a unique ID to every request.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get(controlhttp.HeaderRequestID)
		if reqID == "" {
			reqID = uuid.New().String()
		}
		w.Header().Set(controlhttp.HeaderRequestID, reqID)
		ctx := log.ContextWithRequestID(r.Context(), reqID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

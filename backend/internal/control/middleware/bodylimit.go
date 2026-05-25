// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package middleware

import "net/http"

// DefaultMaxRequestBodyBytes is the canonical ceiling for inbound request
// bodies on the control API. It is a backstop, not a tight quota: every real
// control payload (config blobs, EPG service lists, intents) is far smaller, so
// the cap only trips on abusive or runaway requests. Endpoints that need a
// tighter bound (e.g. /intents) wrap their own MaxBytesReader inside this one.
const DefaultMaxRequestBodyBytes int64 = 4 << 20 // 4 MiB

// MaxBodyBytes caps the size of inbound request bodies to limit bytes. It wraps
// r.Body with http.MaxBytesReader so that any handler reading past the ceiling
// fails its decode instead of buffering unbounded input — bounding memory use
// across every endpoint without per-handler wiring. A limit <= 0 disables the
// cap (passthrough).
func MaxBodyBytes(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if limit <= 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			next.ServeHTTP(w, r)
		})
	}
}

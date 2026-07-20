// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ManuGH/xg2g/internal/log"
)

// legacyAPIMiddleware records metrics and warnings for non-v3 API endpoints per SPEC_MODERNIZATION_2026.md §A1.1
// and gates access when api.legacy_enabled is false per §A1.2.
// It intercepts any request starting with "/api/" that is not part of canonical "/api/v3/" routes,
// increments xg2g_legacy_api_requests_total{path,client}, and logs a WARN message.
// If APILegacyEnabled is false, it returns 410 Gone with a problem+json body.
func (s *Server) legacyAPIMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasPrefix(path, "/api/") && !strings.HasPrefix(path, "/api/v3/") && path != "/api/v3" {
			client := getClientLabel(r)
			recordLegacyAPIMetric(path, client)
			log.L().Warn().
				Str("path", path).
				Str("client", client).
				Str("remote_addr", r.RemoteAddr).
				Msg("legacy API endpoint accessed (deprecated, migrate to /api/v3)")

			if !s.GetConfig().APILegacyEnabled {
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(http.StatusGone)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"type":   "https://xg2g.example.invalid/problems/legacy-api-gone",
					"title":  "Legacy API Retired",
					"status": http.StatusGone,
					"detail": "This legacy API endpoint has been retired and disabled by configuration. Please migrate to the /api/v3 endpoints.",
				})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func getClientLabel(r *http.Request) string {
	ua := strings.TrimSpace(r.Header.Get("User-Agent"))
	if ua == "" {
		return "unknown"
	}
	// Take the first token of User-Agent or cap at 64 characters to prevent Prometheus cardinality explosion.
	parts := strings.Fields(ua)
	if len(parts) > 0 {
		client := parts[0]
		if len(client) > 64 {
			return client[:64]
		}
		return client
	}
	return "unknown"
}

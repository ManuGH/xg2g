// SPDX-License-Identifier: MIT

// Package api provides HTTP server functionality for the xg2g application.
package api

import (
	"fmt"
	"net/http"
)

// DeprecationConfig holds configuration for API deprecation warnings
type DeprecationConfig struct {
	SunsetVersion string // Version when the deprecated API will be removed
	SunsetDate    string // Date when the deprecated API will be removed (RFC3339 format)
	SuccessorPath string // Path to the successor API version
}

// deprecationMiddleware adds deprecation headers to responses
// This follows RFC 8594 (Sunset header) and standard deprecation practices
func deprecationMiddleware(cfg DeprecationConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Add deprecation headers
			w.Header().Set("Deprecation", "true")

			if cfg.SunsetDate != "" {
				// Sunset header (RFC 8594) indicates when the API will be removed
				w.Header().Set("Sunset", cfg.SunsetDate)
			}

			if cfg.SuccessorPath != "" {
				// Link header points to the successor version
				w.Header().Set("Link", fmt.Sprintf(`<%s>; rel="successor-version"`, cfg.SuccessorPath))
			}

			// Warning header provides human-readable deprecation message (RFC 7234)
			warningMsg := "This API is deprecated"
			if cfg.SuccessorPath != "" {
				warningMsg += fmt.Sprintf(". Use %s instead", cfg.SuccessorPath)
			}
			if cfg.SunsetVersion != "" {
				warningMsg += fmt.Sprintf(". Will be removed in version %s", cfg.SunsetVersion)
			}
			w.Header().Set("Warning", fmt.Sprintf(`299 - "%s"`, warningMsg))

			next.ServeHTTP(w, r)
		})
	}
}

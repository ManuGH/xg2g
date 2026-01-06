// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"net/http"

	"github.com/ManuGH/xg2g/internal/auth"
)

// extractToken delegates to the shared internal/auth package to ensure parity with valid proxy auth.
func extractToken(r *http.Request) string {
	// 1. Try standard Header/Cookie extraction
	t := auth.ExtractToken(r)
	if t != "" {
		return t
	}
	// 2. Fallback to Query Parameter (needed for direct media playback)
	return r.URL.Query().Get("token")
}

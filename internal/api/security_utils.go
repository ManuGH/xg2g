// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"net/http"
	"regexp"

	"github.com/ManuGH/xg2g/internal/auth"
)

var segmentAllowList = regexp.MustCompile(`^[a-zA-Z0-9._-]+\.(ts|m4s|mp4|aac|vtt|m3u8)$`)

// extractToken delegates to the shared internal/auth package to ensure parity with valid proxy auth.
func extractToken(r *http.Request, allowQuery bool) string {
	return auth.ExtractToken(r, allowQuery)
}

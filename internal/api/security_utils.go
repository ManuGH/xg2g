// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"net/http"
	"strings"

	"github.com/ManuGH/xg2g/internal/control/auth"
)

// extractToken delegates to the shared internal/auth package to ensure parity with valid proxy auth.
func extractToken(r *http.Request) string {
	return auth.ExtractToken(r)
}

func isMediaRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	path := r.URL.Path
	if strings.HasPrefix(path, "/api/v3/recordings/") &&
		(strings.HasSuffix(path, "/stream.mp4") || strings.HasSuffix(path, "/playlist.m3u8")) {
		return true
	}
	if strings.HasPrefix(path, "/api/v3/sessions/") && strings.Contains(path, "/hls/") {
		return true
	}
	return false
}

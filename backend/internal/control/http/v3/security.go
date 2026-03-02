// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"net/http"
	"strings"

	"github.com/ManuGH/xg2g/internal/control/auth"
)

func (s *Server) extractTokenDetailedWithLegacyPolicy(r *http.Request, allowLegacySources bool) (string, string) {
	return auth.ExtractTokenDetailedWithOptions(r, auth.TokenExtractOptions{
		AllowLegacySources:  allowLegacySources,
		ResolveSessionToken: s.resolveSessionToken,
	})
}

func extractBearerToken(r *http.Request) string {
	if r == nil {
		return ""
	}
	if header := r.Header.Get("Authorization"); strings.HasPrefix(header, "Bearer ") {
		return strings.TrimSpace(header[7:])
	}
	return ""
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

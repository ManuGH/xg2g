// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"net/http"
)

// handleV3HLS serves HLS playlists and segments.
// Authorization: Requires v3:read scope (enforced by route middleware).
func (s *Server) handleV3HLS(w http.ResponseWriter, r *http.Request) {
	s.hlsProcessor().HandleV3HLS(w, r)
}

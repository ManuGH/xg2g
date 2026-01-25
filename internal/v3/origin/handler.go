//go:build v3
// +build v3

// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package origin

import (
	"net/http"
	"strings"

	"github.com/ManuGH/xg2g/internal/v3/model"
	"github.com/ManuGH/xg2g/internal/v3/store"
)

const minimalManifest = "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:10\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-ENDLIST\n"

// NewHLSOriginHandler is a v3 origin facade: state lookup + dumb file/cache serve.
// MVP behavior:
//  - READY: delegate to downstream handler (e.g. static file server)
//  - STARTING: return minimal .m3u8 (Safari-friendly)
//  - FAILED/UNKNOWN/NOTFOUND: 404 or 503
func NewHLSOriginHandler(st store.StateStore, downstream http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, ".m3u8") {
			downstream.ServeHTTP(w, r)
			return
		}

		// expected: /hls/{sessionId}/master.m3u8
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) < 2 {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		sessionID := parts[1]

		sess, err := st.GetSession(r.Context(), sessionID)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		switch sess.State {
		case model.SessionReady:
			downstream.ServeHTTP(w, r)
			return
		case model.SessionStarting:
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(minimalManifest))
			return
		case model.SessionFailed, model.SessionCancelled:
			http.Error(w, "not found", http.StatusNotFound)
			return
		default:
			http.Error(w, "starting", http.StatusServiceUnavailable)
			return
		}
	})
}

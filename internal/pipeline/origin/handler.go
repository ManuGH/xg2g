//go:build v3
// +build v3

// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package origin

import (
	"net/http"
	"strings"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/platform/httpx"
)

const minimalManifest = "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:10\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-ENDLIST\n"

// NewHLSOriginHandler is a v3 origin facade: state lookup + dumb file/cache serve.
// Behavior:
//   - READY: delegate to downstream handler (e.g. static file server)
//   - STARTING/PRIMING/NEW: return minimal .m3u8 (Safari-friendly)
//   - DRAINING/STOPPING: delegate to downstream (segments may still be available)
//   - FAILED/CANCELLED/STOPPED: 404
//   - UNKNOWN: 503
//   - default (unhandled state): 503
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
		case model.SessionStarting, model.SessionPriming, model.SessionNew:
			w.Header().Set("Content-Type", httpx.ContentTypeHLSPlaylist)
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(minimalManifest))
			return
		case model.SessionDraining, model.SessionStopping:
			// Session is winding down but may still have segments available
			downstream.ServeHTTP(w, r)
			return
		case model.SessionFailed, model.SessionCancelled, model.SessionStopped:
			http.Error(w, "stream ended", http.StatusNotFound)
			return
		case model.SessionUnknown:
			http.Error(w, "session state unknown", http.StatusServiceUnavailable)
			return
		default:
			http.Error(w, "unhandled session state", http.StatusServiceUnavailable)
			return
		}
	})
}

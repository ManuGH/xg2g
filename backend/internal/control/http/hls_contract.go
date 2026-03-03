package http

import (
	"fmt"
	"net/http"
	"time"

	"github.com/ManuGH/xg2g/internal/control/http/problem"
	"github.com/ManuGH/xg2g/internal/platform/httpx"
)

const (
	ContentTypeHLSPlaylist = httpx.ContentTypeHLSPlaylist
	ContentTypeHLSSegment  = httpx.ContentTypeHLSSegment
	ContentTypeFMP4Segment = httpx.ContentTypeFMP4Segment
)

// WriteHLSPlaylistHeaders applies deterministic headers for HLS playlists.
func WriteHLSPlaylistHeaders(w http.ResponseWriter, modTime time.Time) {
	w.Header().Set("Content-Type", ContentTypeHLSPlaylist)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Last-Modified", modTime.UTC().Format(http.TimeFormat))
	// Signal READY state to simplify client buffering logic (Phase 11 Fix)
	w.Header().Set("X-Playback-Session-State", "READY")
}

// WriteHLSSegmentHeaders applies deterministic headers for HLS segments.
// size is used for Range-related headers if needed in the future,
// though ParseRange currently handles the heavy lifting.
func WriteHLSSegmentHeaders(w http.ResponseWriter, modTime time.Time, isInit bool, isFMP4 bool) {
	if isFMP4 || isInit {
		w.Header().Set("Content-Type", ContentTypeFMP4Segment)
	} else {
		w.Header().Set("Content-Type", ContentTypeHLSSegment)
	}

	if isInit {
		w.Header().Set("Cache-Control", "public, max-age=3600")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=60")
	}

	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Encoding", "identity")
	w.Header().Set("Last-Modified", modTime.UTC().Format(http.TimeFormat))
}

// Write416 writes a 416 Range Not Satisfiable response.
func Write416(w http.ResponseWriter, size int64) {
	w.Header().Set("Content-Range", Format416ContentRange(size))
	w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
}

// WritePreparingHLS writes a 503 Preparing response for HLS with RFC 7807 body.
func WritePreparingHLS(w http.ResponseWriter, r *http.Request, recordingID, state string, retryAfter int) {
	w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
	problem.Write(w, r, http.StatusServiceUnavailable,
		"recordings/preparing",
		"Preparing",
		"PREPARING",
		"Recording is being prepared for playback",
		map[string]any{
			"recording_id": recordingID,
			"state":        state,
		})
}

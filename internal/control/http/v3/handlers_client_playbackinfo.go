package v3

import (
	"encoding/json"
	"net/http"

	v3recordings "github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
)

// Responsibility: Client compatibility - PlaybackInfo (DirectPlay vs Transcode decision).
// Non-goals: Full third-party server implementation; only the minimal shape required by clients.
//
// Endpoint (to wire manually): POST /Items/{itemId}/PlaybackInfo
func (s *Server) PostItemsPlaybackInfo(w http.ResponseWriter, r *http.Request, itemId string) {
	var req v3recordings.ClientPlaybackRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	resp, playbackErr := s.recordingsProcessor().ResolveClientPlayback(r.Context(), itemId, req)
	if playbackErr != nil {
		s.writeClientPlaybackError(w, r, playbackErr)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) writeClientPlaybackError(w http.ResponseWriter, r *http.Request, err *v3recordings.ClientPlaybackError) {
	if err == nil {
		writeProblem(w, r, http.StatusInternalServerError, "recordings/internal", "Internal Error", "INTERNAL_ERROR", "An unexpected error occurred", nil)
		return
	}

	switch err.Kind {
	case v3recordings.ClientPlaybackErrorUnavailable:
		writeProblem(w, r, http.StatusServiceUnavailable, "system/unavailable", "Service Unavailable", "UNAVAILABLE", err.Message, nil)
	case v3recordings.ClientPlaybackErrorInvalidInput:
		writeProblem(w, r, http.StatusBadRequest, "recordings/invalid", "Invalid Request", "INVALID_INPUT", err.Message, nil)
	case v3recordings.ClientPlaybackErrorNotFound:
		writeProblem(w, r, http.StatusNotFound, "recordings/not-found", "Not Found", "NOT_FOUND", err.Message, nil)
	case v3recordings.ClientPlaybackErrorPreparing:
		retryAfterSeconds := err.RetryAfterSeconds
		if retryAfterSeconds <= 0 {
			retryAfterSeconds = 5
		}
		probeState := err.ProbeState
		if probeState == "" {
			probeState = "IN_FLIGHT"
		}
		w.Header().Set("Retry-After", "5")
		writeProblem(w, r, http.StatusServiceUnavailable, "recordings/preparing", "Media is being analyzed", "RECORDING_PREPARING", err.Message, map[string]any{
			"retryAfterSeconds": retryAfterSeconds,
			"probeState":        probeState,
		})
	case v3recordings.ClientPlaybackErrorUpstreamUnavailable:
		writeProblem(w, r, http.StatusServiceUnavailable, "recordings/upstream_unavailable", "Upstream Unavailable", "UPSTREAM_UNAVAILABLE", err.Message, nil)
	default:
		writeProblem(w, r, http.StatusInternalServerError, "recordings/internal", "Internal Error", "INTERNAL_ERROR", "An unexpected error occurred", nil)
	}
}

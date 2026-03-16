package v3

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	v3recordings "github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

// Responsibility: Client compatibility - PlaybackInfo (DirectPlay vs Transcode decision).
// Non-goals: Full third-party server implementation; only the minimal shape required by clients.
//
// Endpoint (to wire manually): POST /Items/{itemId}/PlaybackInfo
func (s *Server) PostItemsPlaybackInfo(w http.ResponseWriter, r *http.Request, itemId string) {
	var req v3recordings.ClientPlaybackRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeRegisteredProblem(w, r, http.StatusBadRequest, "recordings/invalid", "Invalid Request", problemcode.CodeInvalidInput, "Failed to parse request body: "+err.Error(), nil)
			return
		}
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
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "recordings/internal", "Internal Error", problemcode.CodeInternalError, "An unexpected error occurred", nil)
		return
	}

	switch err.Kind {
	case v3recordings.ClientPlaybackErrorUnavailable:
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "system/unavailable", "Service Unavailable", problemcode.CodeUnavailable, err.Message, nil)
	case v3recordings.ClientPlaybackErrorInvalidInput:
		writeRegisteredProblem(w, r, http.StatusBadRequest, "recordings/invalid", "Invalid Request", problemcode.CodeInvalidInput, err.Message, nil)
	case v3recordings.ClientPlaybackErrorNotFound:
		writeRegisteredProblem(w, r, http.StatusNotFound, "recordings/not-found", "Not Found", problemcode.CodeNotFound, err.Message, nil)
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
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "recordings/preparing", "Media is being analyzed", problemcode.CodeRecordingPreparing, err.Message, map[string]any{
			"retryAfterSeconds": retryAfterSeconds,
			"probeState":        probeState,
		})
	case v3recordings.ClientPlaybackErrorUpstreamUnavailable:
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "recordings/upstream_unavailable", "Upstream Unavailable", problemcode.CodeUpstreamUnavailable, err.Message, nil)
	default:
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "recordings/internal", "Internal Error", problemcode.CodeInternalError, "An unexpected error occurred", nil)
	}
}

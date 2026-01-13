package v3

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ManuGH/xg2g/internal/control/clientplayback"
	"github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/log"
)

// Responsibility: Client compatibility - PlaybackInfo (DirectPlay vs Transcode decision).
// Non-goals: Full third-party server implementation; only the minimal shape required by clients.
//
// Endpoint (to wire manually): POST /Items/{itemId}/PlaybackInfo
func (s *Server) PostItemsPlaybackInfo(w http.ResponseWriter, r *http.Request, itemId string) {
	// 1) Parse request body (DeviceProfile)
	var req clientplayback.PlaybackInfoRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req) // decision logic is fail-closed
	}

	// 2) Resolve source truth
	s.mu.RLock()
	svc := s.recordingsService
	s.mu.RUnlock()
	if svc == nil {
		writeProblem(w, r, http.StatusServiceUnavailable, "system/unavailable", "Service Unavailable", "UNAVAILABLE", "Recordings service is not initialized", nil)
		return
	}

	res, err := svc.ResolvePlayback(r.Context(), itemId, "generic")
	if err != nil {
		class := recordings.Classify(err)
		msg := err.Error()
		switch class {
		case recordings.ClassInvalidArgument:
			writeProblem(w, r, http.StatusBadRequest, "recordings/invalid", "Invalid Request", "INVALID_INPUT", msg, nil)
		case recordings.ClassNotFound:
			writeProblem(w, r, http.StatusNotFound, "recordings/not-found", "Not Found", "NOT_FOUND", msg, nil)
		case recordings.ClassPreparing:
			w.Header().Set("Retry-After", "5")
			writeProblem(w, r, http.StatusServiceUnavailable, "recordings/preparing", "Preparing", "PREPARING", msg, nil)
		case recordings.ClassUpstream:
			writeProblem(w, r, http.StatusBadGateway, "recordings/upstream", "Upstream Error", "UPSTREAM_ERROR", msg, nil)
		default:
			log.L().Error().Err(err).Str("id", itemId).Msg("client playbackinfo resolution failed")
			writeProblem(w, r, http.StatusInternalServerError, "recordings/internal", "Internal Error", "INTERNAL_ERROR", "An unexpected error occurred", nil)
		}
		return
	}

	// 3) Decision (strict fail-closed)
	dec := clientplayback.Decide(&req, clientplayback.Truth{
		Container:  res.Container,
		VideoCodec: res.VideoCodec,
		AudioCodec: res.AudioCodec,
	})

	// Observability
	evt := log.L().Info().
		Str("event", "client.playback_decision").
		Str("id", itemId).
		Str("decision", string(dec)).
		Str("strategy", res.Strategy)

	if res.DurationSource != nil {
		evt.Str("duration_source", string(*res.DurationSource))
	}
	if res.Container != nil {
		evt.Str("container", *res.Container)
	}
	evt.Msg("resolved client playback")

	// 4) Build response
	resp := s.mapClientPlaybackInfo(itemId, res, dec)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) mapClientPlaybackInfo(id string, res recordings.PlaybackResolution, dec clientplayback.Decision) clientplayback.PlaybackInfoResponse {
	// URL construction stays in handler (Option 1 principle).
	directURL := fmt.Sprintf("/api/v3/recordings/%s/stream.mp4", id)
	hlsURL := fmt.Sprintf("/api/v3/recordings/%s/playlist.m3u8", id)

	// Runtime ticks (optional)
	var ticks *int64
	if res.DurationSec != nil && *res.DurationSec > 0 {
		v := (*res.DurationSec) * 10_000_000
		ticks = &v
	}

	// Default: Transcode via HLS (fail-closed).
	ms := clientplayback.MediaSourceInfo{
		Path:      hlsURL,
		Protocol:  "Http",
		Container: nil,

		RunTimeTicks: ticks,

		SupportsDirectPlay:   false,
		SupportsDirectStream: false,
		SupportsTranscoding:  true,
	}

	if dec == clientplayback.DecisionDirectPlay && res.Strategy == recordings.StrategyDirect {
		ms.Path = directURL
		ms.Container = res.Container
		ms.SupportsDirectPlay = true
		ms.SupportsDirectStream = true
		ms.SupportsTranscoding = true
		return clientplayback.PlaybackInfoResponse{MediaSources: []clientplayback.MediaSourceInfo{ms}}
	}

	// Transcode branch: expose transcoding URL semantics explicitly.
	trURL := hlsURL
	tc := "m3u8"
	sp := "hls"
	ms.TranscodingUrl = &trURL
	ms.TranscodingContainer = &tc
	ms.TranscodingSubProtocol = &sp

	return clientplayback.PlaybackInfoResponse{MediaSources: []clientplayback.MediaSourceInfo{ms}}
}

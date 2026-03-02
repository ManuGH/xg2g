package recordings

import (
	"context"
	"fmt"

	"github.com/ManuGH/xg2g/internal/control/clientplayback"
	"github.com/ManuGH/xg2g/internal/control/playback"
	domainrecordings "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/log"
)

// Service handles recordings-related playback compatibility flows independent of HTTP transport.
type Service struct {
	deps Deps
}

func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

func (s *Service) ResolveClientPlayback(ctx context.Context, itemID string, req ClientPlaybackRequest) (ClientPlaybackResponse, *ClientPlaybackError) {
	svc := s.deps.RecordingsService()
	if svc == nil {
		return ClientPlaybackResponse{}, &ClientPlaybackError{
			Kind:    ClientPlaybackErrorUnavailable,
			Message: "Recordings service is not initialized",
		}
	}

	res, err := svc.ResolvePlayback(ctx, itemID, "generic")
	if err != nil {
		class := domainrecordings.Classify(err)
		msg := err.Error()
		switch class {
		case domainrecordings.ClassInvalidArgument:
			return ClientPlaybackResponse{}, &ClientPlaybackError{Kind: ClientPlaybackErrorInvalidInput, Message: msg, Cause: err}
		case domainrecordings.ClassNotFound:
			return ClientPlaybackResponse{}, &ClientPlaybackError{Kind: ClientPlaybackErrorNotFound, Message: msg, Cause: err}
		case domainrecordings.ClassPreparing:
			return ClientPlaybackResponse{}, &ClientPlaybackError{
				Kind:              ClientPlaybackErrorPreparing,
				Message:           msg,
				RetryAfterSeconds: 5,
				ProbeState:        string(playback.ProbeStateInFlight),
				Cause:             err,
			}
		case domainrecordings.ClassUpstream:
			return ClientPlaybackResponse{}, &ClientPlaybackError{Kind: ClientPlaybackErrorUpstreamUnavailable, Message: msg, Cause: err}
		default:
			log.L().Error().Err(err).Str("id", itemID).Msg("client playbackinfo resolution failed")
			return ClientPlaybackResponse{}, &ClientPlaybackError{
				Kind:    ClientPlaybackErrorInternal,
				Message: "An unexpected error occurred",
				Cause:   err,
			}
		}
	}

	dec := clientplayback.Decide(&req, clientplayback.Truth{
		Container:  res.Container,
		VideoCodec: res.VideoCodec,
		AudioCodec: res.AudioCodec,
	})

	evt := log.L().Info().
		Str("event", "client.playback_decision").
		Str("id", itemID).
		Str("decision", string(dec)).
		Str("strategy", res.Strategy)
	if res.DurationSource != nil {
		evt.Str("duration_source", string(*res.DurationSource))
	}
	if res.Container != nil {
		evt.Str("container", *res.Container)
	}
	evt.Msg("resolved client playback")

	return mapClientPlaybackInfo(itemID, res, dec), nil
}

func mapClientPlaybackInfo(id string, res domainrecordings.PlaybackResolution, dec clientplayback.Decision) ClientPlaybackResponse {
	directURL := fmt.Sprintf("/api/v3/recordings/%s/stream.mp4", id)
	hlsURL := fmt.Sprintf("/api/v3/recordings/%s/playlist.m3u8", id)

	var ticks *int64
	if res.DurationSec != nil && *res.DurationSec > 0 {
		v := (*res.DurationSec) * 10_000_000
		ticks = &v
	}

	ms := clientplayback.MediaSourceInfo{
		Path:      hlsURL,
		Protocol:  "Http",
		Container: nil,

		RunTimeTicks: ticks,

		SupportsDirectPlay:   false,
		SupportsDirectStream: false,
		SupportsTranscoding:  true,
	}

	if dec == clientplayback.DecisionDirectPlay && res.Strategy == domainrecordings.StrategyDirect {
		ms.Path = directURL
		ms.Container = res.Container
		ms.SupportsDirectPlay = true
		ms.SupportsDirectStream = true
		ms.SupportsTranscoding = true
		return clientplayback.PlaybackInfoResponse{MediaSources: []clientplayback.MediaSourceInfo{ms}}
	}

	trURL := hlsURL
	tc := "m3u8"
	sp := "hls"
	ms.TranscodingUrl = &trURL
	ms.TranscodingContainer = &tc
	ms.TranscodingSubProtocol = &sp

	return clientplayback.PlaybackInfoResponse{MediaSources: []clientplayback.MediaSourceInfo{ms}}
}

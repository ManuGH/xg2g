package recordings

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/ManuGH/xg2g/internal/control/clientplayback"
	"github.com/ManuGH/xg2g/internal/control/playback"
	domainrecordings "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	"golang.org/x/sync/singleflight"
)

// Service handles recordings-related playback compatibility flows independent of HTTP transport.
type Service struct {
	deps Deps
	// liveProbeGroup collapses concurrent interactive capability probes for the same
	// serviceRef into a single relay probe (see probeLiveTruthBounded).
	liveProbeGroup singleflight.Group
}

func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

// defaultLiveInteractiveProbeBudget bounds how long an interactive live playback
// request waits for a cold capability probe before returning "unverified, retry".
// A cold relay probe (descrambler not yet locked) can take ~20-33s through its
// failing ffprobe fallbacks; blocking the request that long freezes the player on
// the first-ever play of a channel. The probe keeps running past this budget to
// populate the persistent truth cache, so the client's retry is served from cache.
const defaultLiveInteractiveProbeBudget = 5 * time.Second

// liveInteractiveProbeBudgetNs holds the interactive probe budget
// (see defaultLiveInteractiveProbeBudget). Overridable via setLiveInteractiveProbeBudget.
var liveInteractiveProbeBudgetNs atomic.Int64

func init() {
	liveInteractiveProbeBudgetNs.Store(int64(defaultLiveInteractiveProbeBudget))
}

func liveInteractiveProbeBudget() time.Duration {
	return time.Duration(liveInteractiveProbeBudgetNs.Load())
}

func setLiveInteractiveProbeBudget(d time.Duration) {
	if d > 0 {
		liveInteractiveProbeBudgetNs.Store(int64(d))
	}
}

type liveProbeOutcome struct {
	cap   scan.Capability
	found bool
	err   error
}

// probeLiveTruthBounded runs the capability probe for an interactive live request
// under a hard wall-clock budget. The probe runs on a DETACHED context via
// singleflight so that:
//   - concurrent requests for the same channel (e.g. a player start racing the EPG
//     grid) share ONE relay probe instead of each storming the tuner, and
//   - when the requester gives up at the budget, the probe keeps running to populate
//     the persistent truth cache for the client's retry, instead of being cancelled
//     and discarded.
//
// ProbeCapability enforces its own internal ffprobe timeouts, so the detached probe
// cannot run unbounded. Returns completed=false when the budget elapsed first.
func (s *Service) probeLiveTruthBounded(ctx context.Context, probeSource channelTruthProbeSource, serviceRef string) (cap scan.Capability, found bool, completed bool, err error) {
	ch := s.liveProbeGroup.DoChan(serviceRef, func() (interface{}, error) {
		// Detached: the probe must outlive this request's context so a budget-exceeded
		// requester still gets its truth cached for the retry.
		bg := context.WithoutCancel(ctx)
		c, f, e := probeSource.ProbeCapability(bg, serviceRef)
		return liveProbeOutcome{cap: c, found: f, err: e}, nil
	})

	timer := time.NewTimer(liveInteractiveProbeBudget())
	defer timer.Stop()

	select {
	case res := <-ch:
		outcome, _ := res.Val.(liveProbeOutcome)
		return outcome.cap, outcome.found, true, outcome.err
	case <-timer.C:
		return scan.Capability{}, false, false, nil
	case <-ctx.Done():
		return scan.Capability{}, false, false, ctx.Err()
	}
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

package playback

import (
	"context"

	"github.com/ManuGH/xg2g/internal/core/normalize"
)

// --- Interfaces ---

type MediaTruthProvider interface {
	GetMediaTruth(ctx context.Context, id string) (MediaTruth, error)
}

type ClientProfileResolver interface {
	Resolve(ctx context.Context, headers map[string]string) (PlaybackCapabilities, error)
}

// --- Engine ---

type DecisionEngine struct {
	truth   MediaTruthProvider
	profile ClientProfileResolver
}

func NewDecisionEngine(truth MediaTruthProvider, profile ClientProfileResolver) *DecisionEngine {
	return &DecisionEngine{
		truth:   truth,
		profile: profile,
	}
}

func (e *DecisionEngine) GetMediaTruth(ctx context.Context, id string) (MediaTruth, error) {
	return e.truth.GetMediaTruth(ctx, id)
}

func (e *DecisionEngine) Decide(truth MediaTruth, caps PlaybackCapabilities, protocolHint string) (PlaybackPlan, error) {
	// 3. State Gate
	if truth.State == StatePreparing {
		return PlaybackPlan{}, ErrPreparing
	}
	if truth.State == StateFailed {
		return PlaybackPlan{}, ErrUpstream
	}

	// 4. Unknown Truth Gate (G9) -> Fail Closed (422)
	// Mandatory fields must be present and not "unknown".
	if isUnknownToken(truth.Container) ||
		isUnknownToken(truth.VideoCodec) ||
		isUnknownToken(truth.AudioCodec) {
		return PlaybackPlan{
			DecisionReason: ReasonProbeFailed,
		}, ErrDecisionAmbiguous
	}

	// --- Phase 1: Select Protocol --- //

	// Default to HLS
	protocol := ProtocolHLS

	// Hint Overrides
	switch normalize.Token(protocolHint) {
	case "mp4":
		protocol = ProtocolMP4
	case "hls":
		protocol = ProtocolHLS
	}

	// --- Phase 2: Analyze Compatibility --- //

	// Check Codecs
	videoCompatible := contains(caps.VideoCodecs, truth.VideoCodec)
	audioCompatible := contains(caps.AudioCodecs, truth.AudioCodec)

	// Check Container for selected Protocol
	// If MP4 req: container must be MP4/MOV
	// If HLS req: container acts as the segment format.
	// We check if the client supports this container via its capabilities.
	containerCompatible := false
	if protocol == ProtocolMP4 {
		// Strict MP4
		containerCompatible = isMP4Container(truth.Container) && contains(caps.Containers, truth.Container)
	} else {
		// HLS
		if caps.SupportsHLS {
			// If HLS is supported, we check if the underlying container (segment format)
			// is in the client's supported containers list.
			// e.g. Safari supports "ts", "mp4".
			// e.g. MSE supports "mp4"; "ts" support (via JS transmuxing) should be explicitly listed in caps.Containers.
			containerCompatible = contains(caps.Containers, truth.Container)
		}
	}

	// --- Phase 3: Decision Matrix --- //

	// G7/G8: Codec Incompatible -> Transcode
	if !videoCompatible {
		return PlaybackPlan{
			Mode:           ModeTranscode,
			Protocol:       protocol,
			DecisionReason: ReasonTranscodeVideo,
			TruthReason:    "codec_video_mismatch",
			Container:      truth.Container,
			VideoCodec:     truth.VideoCodec,
			AudioCodec:     truth.AudioCodec,
			Duration:       truth.Duration,
		}, nil
	}

	if !audioCompatible {
		return PlaybackPlan{
			Mode:           ModeTranscode,
			Protocol:       protocol,
			DecisionReason: ReasonTranscodeAudio,
			TruthReason:    "codec_audio_mismatch",
			Container:      truth.Container,
			VideoCodec:     truth.VideoCodec,
			AudioCodec:     truth.AudioCodec,
			Duration:       truth.Duration,
		}, nil
	}

	// G6: Codecs OK, Container Incompatible -> DirectStream
	if !containerCompatible {
		return PlaybackPlan{
			Mode:           ModeDirectStream,
			Protocol:       protocol,
			DecisionReason: ReasonDirectStreamMatch,
			TruthReason:    "container_mismatch",
			Container:      truth.Container,
			VideoCodec:     truth.VideoCodec,
			AudioCodec:     truth.AudioCodec,
			Duration:       truth.Duration,
		}, nil
	}

	// G4/G5: Everything Compatible -> DirectPlay
	mode := ModeDirectPlay
	if protocol == ProtocolMP4 && !isMP4Container(truth.Container) {
		// If requesting MP4 but source is TS -> Must remux (DirectStream)
		mode = ModeDirectStream
	}

	plan := PlaybackPlan{
		Mode:           mode,
		Protocol:       protocol,
		DecisionReason: ReasonDirectPlayMatch,
		TruthReason:    "all_compatible",
		Container:      truth.Container,
		VideoCodec:     truth.VideoCodec,
		AudioCodec:     truth.AudioCodec,
		Duration:       truth.Duration,
	}
	return plan, nil
}

func (e *DecisionEngine) Resolve(ctx context.Context, req ResolveRequest) (PlaybackPlan, error) {
	// 1. Resolve Profile
	caps, err := e.profile.Resolve(ctx, req.Headers)
	if err != nil {
		return PlaybackPlan{}, err
	}

	// 2. Get Truth
	truth, err := e.truth.GetMediaTruth(ctx, req.RecordingID)
	if err != nil {
		return PlaybackPlan{}, err
	}

	return e.Decide(truth, caps, req.ProtocolHint)
}

func contains(slice []string, val string) bool {
	val = normalize.Token(val)
	if val == "" {
		return false
	}
	for _, s := range slice {
		if normalize.Token(s) == val {
			return true
		}
	}
	return false
}

func isMP4Container(c string) bool {
	norm := normalize.Token(c)
	return norm == "mp4" || norm == "mov" || norm == "m4v"
}

func isUnknownToken(s string) bool {
	norm := normalize.Token(s)
	return norm == "" || norm == "unknown"
}

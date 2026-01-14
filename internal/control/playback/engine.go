package playback

import (
	"context"
)

// --- Interfaces ---

type MediaTruthProvider interface {
	GetMediaTruth(ctx context.Context, id string) (MediaTruth, error)
}

type ClientProfileResolver interface {
	Resolve(ctx context.Context, headers map[string]string) (ClientProfile, error)
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

func (e *DecisionEngine) Resolve(ctx context.Context, req ResolveRequest) (PlaybackPlan, error) {
	// --- Phase 0: Inputs & Gating --- //

	// 1. Resolve Profile (includes Auth check if profile resolver enforces it)
	profile, err := e.profile.Resolve(ctx, req.Headers)
	if err != nil {
		// G1: Unauthorized is handled here if resolver returns ErrForbidden
		return PlaybackPlan{}, err
	}

	// 2. Get Truth
	truth, err := e.truth.GetMediaTruth(ctx, req.RecordingID)
	if err != nil {
		// G2: NotFound handled here
		return PlaybackPlan{}, err
	}

	// 3. State Gate
	if truth.State == StatePreparing {
		// G3: Preparing Gate
		return PlaybackPlan{}, ErrPreparing
	}
	if truth.State == StateFailed {
		return PlaybackPlan{}, ErrUpstream
	}

	// 4. Unknown Truth Gate (G9)
	if truth.VideoCodec == "" || truth.VideoCodec == "unknown" ||
		truth.AudioCodec == "" || truth.AudioCodec == "unknown" {
		return PlaybackPlan{}, ErrUpstream
	}

	// --- Phase 1: Select Protocol --- //

	// Default to HLS
	protocol := ProtocolHLS

	// Hint Overrides
	switch req.ProtocolHint {
	case "mp4":
		protocol = ProtocolMP4
	case "hls":
		protocol = ProtocolHLS
	default:
		// Auto logic if no hint:
		// If native HLS supported (Safari), prefer HLS
		// If generic client and MP4 container, maybe MP4?
		// For now, strict HLS default unless hinted, as per plan.
		protocol = ProtocolHLS
	}

	// --- Phase 2: Analyze Compatibility --- //

	// Check Codecs
	videoCompatible := e.isVideoCompatible(profile, truth.VideoCodec)
	audioCompatible := e.isAudioCompatible(profile, truth.AudioCodec)

	// Check Container for selected Protocol
	// If MP4 req: container must be MP4/MOV
	// If HLS req: container is less strict IF we support remux (DirectStream)
	// OR if client supports native TS (Safari)
	containerCompatible := false
	if protocol == ProtocolMP4 {
		// Strict MP4
		containerCompatible = isMP4Container(truth.Container)
	} else {
		// HLS
		if profile.SupportsNativeHLS {
			// Safari supports TS and fMP4 (via HLS)
			containerCompatible = isNativeHLSContainer(truth.Container)
		} else if profile.SupportsMSE {
			// MSE (hls.js) typically needs fMP4/MP4 repacking or TS transmuxing client-side.
			// Ideally engine treats "TS via HLS.js" as "Compatible" (DirectPlay) if hls.js handles TS.
			// HLS.js handles TS. So TS is "compatible" for HLS protocol.
			containerCompatible = true
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
	// (Example: MKV with H264/AAC requesting HLS)
	// (Example: MKV with H264/AAC requesting MP4 -> technically Transcode/Remux, but engine calls it DirectStream)
	if !containerCompatible {
		// If protocol is MP4 and container is MKV -> DirectStream (Remux to MP4)
		// If protocol is HLS and container is MKV -> DirectStream (Remux to TS/fMP4)
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
	return PlaybackPlan{
		Mode:           ModeDirectPlay,
		Protocol:       protocol,
		DecisionReason: ReasonDirectPlayMatch,
		TruthReason:    "all_compatible",
		Container:      truth.Container,
		VideoCodec:     truth.VideoCodec,
		AudioCodec:     truth.AudioCodec,
		Duration:       truth.Duration,
	}, nil
}

// --- Helpers ---

func (e *DecisionEngine) isVideoCompatible(freq ClientProfile, codec string) bool {
	// Simple mapping for now
	switch codec {
	case "h264":
		return freq.SupportsH264
	case "hevc":
		return freq.SupportsHEVC
	case "mpeg2video":
		return freq.SupportsMPEG2
	}
	return false // Fail closed on unknown/unsupported types
}

func (e *DecisionEngine) isAudioCompatible(freq ClientProfile, codec string) bool {
	switch codec {
	case "aac":
		return freq.SupportsAAC
	case "ac3":
		return freq.SupportsAC3
	case "mp2":
		// Assume generic support not present unless explicit?
		// Actually modern browsers don't do MP2.
		// Tests G7 implies Transcode needed for mpeg2/mp2.
		return false
	}
	return false
}

func isMP4Container(c string) bool {
	return c == "mp4" || c == "mov" || c == "m4v"
}

func isNativeHLSContainer(c string) bool {
	// Safari plays TS, MP4, MOV via HLS
	return c == "mpegts" || c == "ts" || isMP4Container(c)
}

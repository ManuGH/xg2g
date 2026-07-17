package playback

import (
	"slices"

	"github.com/ManuGH/xg2g/internal/media/codec"
	"github.com/ManuGH/xg2g/internal/media/container"
)

// AudioCapability is intentionally narrow in phase 1. We only need normalized
// codec identity to reason about packaging combinations.
type AudioCapability struct {
	Codec codec.ID
}

// PackagingCapability describes a client-supported transport combination.
// It is relational by design: the same codec may be supported in one container
// but not in another, or only for a specific delivery method.
type PackagingCapability struct {
	Container   container.Format
	Delivery    container.DeliveryMethod
	VideoCodecs []codec.ID
	AudioCodecs []codec.ID
}

// ClientPlaybackMatrix combines codec-level capabilities with packaging-level
// support and delivery-level constraints.
type ClientPlaybackMatrix struct {
	Video         []codec.VideoCapability
	Audio         []AudioCapability
	Packaging     []PackagingCapability
	SupportsRange bool
}

// StreamRequest is a pure media-semantic requirement, not a policy decision.
type StreamRequest struct {
	Video     codec.ID
	Audio     codec.ID
	Container container.Format
	Delivery  container.DeliveryMethod
}

// PackagingPolicy is the immutable operator-policy input for practical
// container compatibility. Keeping it separate from StreamRequest preserves
// the distinction between media facts and policy decisions.
type PackagingPolicy struct {
	ExperimentalAV1MPEGTS bool
}

type CompatibilityReason string

const (
	ReasonNoMatchingTransport   CompatibilityReason = "no_matching_transport"
	ReasonVideoCodecUnsupported CompatibilityReason = "video_codec_unsupported"
	ReasonAudioCodecUnsupported CompatibilityReason = "audio_codec_unsupported"
	ReasonContainerCannotCarry  CompatibilityReason = "container_cannot_carry_codec"
)

// CompatibilityResult stays slice-based for now, matching the current phase-1
// codec package and keeping inspection easy while the new path is still parallel
// to the existing predicates.
type CompatibilityResult struct {
	Reasons []CompatibilityReason
}

func (r CompatibilityResult) Compatible() bool {
	return len(r.Reasons) == 0
}

func (r CompatibilityResult) Has(reason CompatibilityReason) bool {
	return slices.Contains(r.Reasons, reason)
}

func (r *CompatibilityResult) Add(reason CompatibilityReason) {
	if r == nil || reason == "" || r.Has(reason) {
		return
	}
	r.Reasons = append(r.Reasons, reason)
}

func (m ClientPlaybackMatrix) FindVideo(id codec.ID) (codec.VideoCapability, bool) {
	for _, capability := range m.Video {
		if capability.Codec == id {
			return capability, true
		}
	}
	return codec.VideoCapability{}, false
}

func (m ClientPlaybackMatrix) HasAudio(id codec.ID) bool {
	for _, capability := range m.Audio {
		if capability.Codec == id {
			return true
		}
	}
	return false
}

// EvaluatePackagingCompatibility reports pure transport/package mismatches. It
// does not decide whether Uncertain media properties should fail closed; that
// policy remains in the decision layer.
func EvaluatePackagingCompatibility(matrix ClientPlaybackMatrix, request StreamRequest) CompatibilityResult {
	return EvaluatePackagingCompatibilityWithPolicy(matrix, request, PackagingPolicy{})
}

// EvaluatePackagingCompatibilityWithPolicy reports transport/package
// mismatches using the exact policy snapshot attached to the planning request.
func EvaluatePackagingCompatibilityWithPolicy(matrix ClientPlaybackMatrix, request StreamRequest, policy PackagingPolicy) CompatibilityResult {
	var result CompatibilityResult

	for _, capability := range matrix.Packaging {
		if capability.Container != request.Container || capability.Delivery != request.Delivery {
			continue
		}

		if !request.Container.CanCarryWithPolicy(request.Video, policy.ExperimentalAV1MPEGTS) ||
			!request.Container.CanCarryWithPolicy(request.Audio, policy.ExperimentalAV1MPEGTS) {
			result.Add(ReasonContainerCannotCarry)
		}
		if !containsCodec(capability.VideoCodecs, request.Video) {
			result.Add(ReasonVideoCodecUnsupported)
		}
		if !containsCodec(capability.AudioCodecs, request.Audio) {
			result.Add(ReasonAudioCodecUnsupported)
		}
		return result
	}

	result.Add(ReasonNoMatchingTransport)
	return result
}

func containsCodec(codecs []codec.ID, target codec.ID) bool {
	return slices.Contains(codecs, target)
}

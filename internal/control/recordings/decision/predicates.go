package decision

import (
	"strings"
)

// computePredicates evaluates all compatibility predicates (Section 6.2).
// All predicates are pure boolean functions with no side effects.
func computePredicates(source Source, caps Capabilities, policy Policy) Predicates {
	// Element-wise compatibility checks
	// ADR-009.1 §1 Scope Cut: codec compatibility is string-only (no profile/level).
	// Exit condition: TruthProvider provides profile/level or RFC-6381, and Capabilities can express them.
	canContainer := contains(caps.Containers, source.Container)
	canVideo := contains(caps.VideoCodecs, source.VideoCodec)
	canAudio := contains(caps.AudioCodecs, source.AudioCodec)

	// Direct play: client can play source container+codecs directly via static MP4
	// Strict: Container MUST be mp4/mov/m4v (Protocol Limitation)
	// AND Client MUST support Range requests (for seeking/progressive)
	// FIX R2-001: Normalize container to match contains() behavior
	containerNorm := strings.ToLower(strings.TrimSpace(source.Container))
	isMP4 := containerNorm == "mp4" || containerNorm == "mov" || containerNorm == "m4v"
	hasRange := caps.SupportsRange != nil && *caps.SupportsRange
	directPlayPossible := canContainer && canVideo && canAudio && isMP4 && hasRange

	// Direct stream: no re-encode, but may remux/package to HLS
	// Requires: HLS support + compatible codecs (container may differ)
	directStreamPossible := caps.SupportsHLS && canVideo && canAudio

	// Transcode needed: any incompatibility OR protocol gap (neither DP nor DS possible)
	transcodeNeeded := !canVideo || !canAudio || (!directPlayPossible && !directStreamPossible)

	// Transcode possible: policy-gated + client must accept HLS output
	transcodePossible := policy.AllowTranscode && caps.SupportsHLS

	return Predicates{
		CanContainer:         canContainer,
		CanVideo:             canVideo,
		CanAudio:             canAudio,
		DirectPlayPossible:   directPlayPossible,
		DirectStreamPossible: directStreamPossible,
		TranscodeNeeded:      transcodeNeeded,
		TranscodePossible:    transcodePossible,
	}
}

// contains checks if a slice contains a specific string (case-insensitive).
func contains(slice []string, item string) bool {
	item = strings.ToLower(strings.TrimSpace(item))
	for _, s := range slice {
		if strings.ToLower(strings.TrimSpace(s)) == item {
			return true
		}
	}
	return false
}

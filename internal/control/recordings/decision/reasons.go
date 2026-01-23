package decision

import (
	"fmt"
	"sort"
)

// ReasonCode represents a frozen vocabulary of machine-readable reason codes.
// No ad-hoc strings allowed. Any addition requires ADR + OpenAPI + golden update.
type ReasonCode string

// Frozen reason code vocabulary (P4-2 baseline).
const (
	// Policy
	// Decision Ambiguity (Priority 1)
	ReasonAmbiguous ReasonCode = "decision_ambiguous"

	// Capability Violations (Priority 2-4)
	ReasonContainerNotSupported  ReasonCode = "container_not_supported_by_client"
	ReasonVideoCodecNotSupported ReasonCode = "video_codec_not_supported_by_client"
	ReasonAudioCodecNotSupported ReasonCode = "audio_codec_not_supported_by_client"
	ReasonHLSNotSupported        ReasonCode = "hls_not_supported_by_client"

	// Policy Violations (Priority 5)
	ReasonPolicyDeniesTranscode ReasonCode = "policy_denies_transcode"

	// General Fallback
	ReasonNoCompatiblePlaybackPath ReasonCode = "no_compatible_playback_path"

	// Observability (Success Modes Only)
	ReasonDirectPlayMatch   ReasonCode = "directplay_match"
	ReasonDirectStreamMatch ReasonCode = "directstream_match"
)

// validReasons is the whitelist guard to prevent ad-hoc string injection.
var validReasons = map[ReasonCode]bool{
	ReasonAmbiguous:                true,
	ReasonContainerNotSupported:    true,
	ReasonVideoCodecNotSupported:   true,
	ReasonAudioCodecNotSupported:   true,
	ReasonHLSNotSupported:          true,
	ReasonPolicyDeniesTranscode:    true,
	ReasonDirectPlayMatch:          true,
	ReasonDirectStreamMatch:        true,
	ReasonNoCompatiblePlaybackPath: true,
}

// Valid returns true if this reason code is in the frozen vocabulary.
// This is a defensive guard to catch ad-hoc string injection.
func (r ReasonCode) Valid() bool {
	return validReasons[r]
}

// AllReasonCodes returns a slice of all valid reason codes for validation.
func AllReasonCodes() []ReasonCode {
	return []ReasonCode{
		ReasonAmbiguous,
		ReasonContainerNotSupported,
		ReasonVideoCodecNotSupported,
		ReasonAudioCodecNotSupported,
		ReasonHLSNotSupported,
		ReasonPolicyDeniesTranscode,
		ReasonDirectPlayMatch,
		ReasonDirectStreamMatch,
		ReasonNoCompatiblePlaybackPath,
	}
}

// ValidateReasons checks if all reasons in the slice are valid.
func ValidateReasons(reasons []ReasonCode) error {
	for _, r := range reasons {
		if !r.Valid() {
			return fmt.Errorf("invalid reason code: %s", r)
		}
	}
	return nil
}

var reasonPriority = map[ReasonCode]int{
	ReasonPolicyDeniesTranscode:    0,
	ReasonContainerNotSupported:    1,
	ReasonVideoCodecNotSupported:   1,
	ReasonAudioCodecNotSupported:   1,
	ReasonHLSNotSupported:          1,
	ReasonNoCompatiblePlaybackPath: 2,
	ReasonDirectPlayMatch:          3,
	ReasonDirectStreamMatch:        3,
	ReasonAmbiguous:                4,
}

func reasonRank(r ReasonCode) int {
	if rank, ok := reasonPriority[r]; ok {
		return rank
	}
	return 100
}

func sortReasonsByPriority(reasons []ReasonCode) {
	sort.SliceStable(reasons, func(i, j int) bool {
		ri := reasonRank(reasons[i])
		rj := reasonRank(reasons[j])
		if ri != rj {
			return ri < rj
		}
		return reasons[i] < reasons[j]
	})
}

func primaryReason(reasons []ReasonCode) ReasonCode {
	if len(reasons) == 0 {
		return ReasonAmbiguous
	}
	best := reasons[0]
	bestRank := reasonRank(best)
	for _, r := range reasons[1:] {
		rank := reasonRank(r)
		if rank < bestRank || (rank == bestRank && r < best) {
			best = r
			bestRank = rank
		}
	}
	return best
}

// ReasonCodeSlice attaches the methods of sort.Interface to []ReasonCode,
// sorting in increasing order.
type ReasonCodeSlice []ReasonCode

func (p ReasonCodeSlice) Len() int           { return len(p) }
func (p ReasonCodeSlice) Less(i, j int) bool { return p[i] < p[j] }
func (p ReasonCodeSlice) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

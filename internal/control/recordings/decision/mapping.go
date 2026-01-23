package decision

import (
	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
)

// ProtocolFrom derives the playback protocol from a decision.
// Returns "mp4", "hls", or "none".
// This is the SINGLE SOURCE OF TRUTH for protocol derivation.
func ProtocolFrom(dec *Decision) string {
	if dec == nil {
		return "none"
	}
	switch dec.SelectedOutputKind {
	case "file":
		return "mp4"
	case "hls":
		return "hls"
	default:
		return "none"
	}
}

// ReasonPrimaryFrom derives the primary reason code string.
// Handles both Decision (Success/Deny) and Problem (Error) paths.
// This is the SINGLE SOURCE OF TRUTH for reason reporting.
func ReasonPrimaryFrom(dec *Decision, prob *Problem) string {
	if prob != nil {
		return string(prob.Code)
	}
	if dec == nil {
		return string(ReasonAmbiguous) // Defensiveness
	}

	if len(dec.Reasons) > 0 {
		return string(primaryReason(dec.Reasons))
	}

	// Fallback logic for success/deny modes if reasons empty
	switch dec.Mode {
	case ModeDirectPlay:
		return string(ReasonDirectPlayMatch)
	case ModeDirectStream:
		return string(ReasonDirectStreamMatch)
	case ModeDeny:
		return string(ReasonNoCompatiblePlaybackPath)
	}

	return string(ReasonAmbiguous)
}

// ReasonsAsStrings returns all reasons as a string slice.
// If problem is present, returns a slice containing just the problem code.
func ReasonsAsStrings(dec *Decision, prob *Problem) []string {
	if prob != nil {
		return []string{string(prob.Code)}
	}
	if dec == nil {
		return []string{}
	}

	out := make([]string, len(dec.Reasons))
	for i, r := range dec.Reasons {
		out[i] = string(r)
	}
	return out
}

// FromCapabilities maps external capabilities to internal Decision Engine capabilities.
// This centralizes mapping logic to ensure consistency across tests and handlers.
func FromCapabilities(c capabilities.PlaybackCapabilities) Capabilities {
	dc := Capabilities{
		Version:       c.CapabilitiesVersion,
		Containers:    c.Containers,
		VideoCodecs:   c.VideoCodecs,
		AudioCodecs:   c.AudioCodecs,
		SupportsHLS:   c.SupportsHLS,
		SupportsRange: c.SupportsRange,
		DeviceType:    c.DeviceType,
	}
	if c.MaxVideo != nil {
		dc.MaxVideo = &MaxVideoDimensions{
			Width:  c.MaxVideo.Width,
			Height: c.MaxVideo.Height,
		}
	}
	return dc
}

// derefInt helper if needed, but MaxVideo uses int in capabilities.MaxVideo.
// Wait, capabilities.MaxVideo defines Width/Height as int (not pointer).
// decision.MaxVideoDimensions defines Width/Height as int.
// So direct assignment is fine.

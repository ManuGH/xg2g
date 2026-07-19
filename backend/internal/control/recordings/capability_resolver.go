package recordings

import (
	"context"

	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/domain/playbackcompat"
)

// CapabilityContract keeps client claims separate from server-verified and
// effective capabilities. Verified contains family fallback plus compatibility
// policy; Effective additionally contains operator/server constraints.
type CapabilityContract struct {
	Raw           capabilities.PlaybackCapabilities
	Verified      capabilities.PlaybackCapabilities
	Effective     capabilities.PlaybackCapabilities
	PolicyVersion string
	Adjustments   []playbackcompat.Adjustment
}

// ResolveCapabilities implements the SSOT (Single Source of Truth) for playback capabilities.
// It follows the normative rules of ADR-P7 as superseded by ADR-028:
// 1. Explicit-only profile selection (default: web_conservative).
// 2. Client lists are claims that compatibility policy may narrow, never widen.
// 3. Only Effective capabilities are returned to legacy callers.
func ResolveCapabilities(
	ctx context.Context,
	principal string, // or auth.Principal
	apiVersion string,
	requestedProfile string,
	headers map[string]string,
	clientCaps *capabilities.PlaybackCapabilities,
) capabilities.PlaybackCapabilities {
	return ResolveCapabilityContract(ctx, principal, apiVersion, requestedProfile, headers, clientCaps, "unknown", "").Effective
}

// ResolveCapabilityContract resolves the provenance-aware capability contract
// for one playback scope. Runtime probes remain visible as Raw claims; only the
// Effective set is allowed to enter a decision engine.
func ResolveCapabilityContract(
	ctx context.Context,
	principal string,
	apiVersion string,
	requestedProfile string,
	headers map[string]string,
	clientCaps *capabilities.PlaybackCapabilities,
	scope string,
	clientFamily string,
) CapabilityContract {
	_ = ctx
	_ = principal
	_ = apiVersion
	_ = headers

	var raw capabilities.PlaybackCapabilities
	var resolved capabilities.PlaybackCapabilities

	// 1. v3.1 Branch: preserve explicit client caps as claims. Family data may
	// fill omitted fields, but never extends a non-empty client list.
	if clientCaps != nil && clientCaps.CapabilitiesVersion > 0 {
		raw = capabilities.CanonicalizeCapabilities(*clientCaps)
		resolved = capabilities.ResolveRuntimeProbeCapabilities(raw)
	} else {
		// 2. Manual Profile Selection
		profile := requestedProfile
		if profile == "" {
			profile = string(ProfileGeneric) // Defaults to web_conservative via mapping
		}

		// 3. Identity-bound profile values are explicit server fallback claims.
		raw = capabilities.CanonicalizeCapabilities(getProfileCaps(profile))
		resolved = raw
	}

	verifiedFamily := resolved.ClientFamilyFallback
	if verifiedFamily == "" {
		verifiedFamily = clientFamily
	}
	verifiedResolution := playbackcompat.Resolve(playbackcompat.Claims{
		Scope:           scope,
		Family:          verifiedFamily,
		PreferredEngine: resolved.PreferredHLSEngine,
		Containers:      resolved.Containers,
		VideoCodecs:     resolved.VideoCodecs,
		AudioCodecs:     resolved.AudioCodecs,
	})
	verified := resolved
	verified.Containers = append([]string(nil), verifiedResolution.Effective.Containers...)
	verified.VideoCodecs = append([]string(nil), verifiedResolution.Effective.VideoCodecs...)
	verified.AudioCodecs = append([]string(nil), verifiedResolution.Effective.AudioCodecs...)
	verified = capabilities.CanonicalizeCapabilities(verified)

	// 4. Operator/server constraints are applied only after compatibility
	// verification. They may narrow, never widen, the verified result.
	effective := applyServerConstraints(verified)
	return CapabilityContract{
		Raw:           raw,
		Verified:      verified,
		Effective:     effective,
		PolicyVersion: verifiedResolution.PolicyVersion,
		Adjustments:   append([]playbackcompat.Adjustment(nil), verifiedResolution.Adjustments...),
	}
}

func applyServerConstraints(in capabilities.PlaybackCapabilities) capabilities.PlaybackCapabilities {
	// ADR P7: Server NEVER extends lists. It only applies constraints.
	out := in
	// Example constraint: hardware policy could override AllowTranscode to false
	// But we don't ADD codecs here.
	return out
}

func getProfileCaps(profile string) capabilities.PlaybackCapabilities {
	switch profile {
	case string(ProfileTVOS), "tvos":
		return capabilities.PlaybackCapabilities{
			CapabilitiesVersion: 1,
			Containers:          []string{"mp4", "ts"},
			VideoCodecs:         []string{"h264"},
			AudioCodecs:         []string{"aac", "ac3"},
			SupportsHLS:         true,
		}
	case "stb_enigma2":
		return capabilities.PlaybackCapabilities{
			CapabilitiesVersion: 1,
			Containers:          []string{"ts"},
			VideoCodecs:         []string{"h264", "mpeg2"},
			AudioCodecs:         []string{"ac3", "mp2"},
			SupportsHLS:         true,
		}
	case "vlc_desktop":
		return capabilities.PlaybackCapabilities{
			CapabilitiesVersion: 1,
			Containers:          []string{"mp4", "mkv", "ts"},
			VideoCodecs:         []string{"h264", "hevc", "mpeg2"},
			AudioCodecs:         []string{"aac", "ac3", "mp3", "mp2"},
			SupportsHLS:         true,
		}
	case "android_tv":
		return capabilities.PlaybackCapabilities{
			CapabilitiesVersion: 1,
			Containers:          []string{"mp4", "mkv", "ts"},
			VideoCodecs:         []string{"h264", "hevc"},
			AudioCodecs:         []string{"aac", "ac3"},
			SupportsHLS:         true,
		}
	case string(ProfileSafari): // Legacy alias
		return getProfileCaps("tvos") // Safari matches tvos native support
	default:
		// web_conservative (H264/AAC/MP3)
		return capabilities.PlaybackCapabilities{
			CapabilitiesVersion: 1,
			Containers:          []string{"mp4", "mkv", "ts"},
			VideoCodecs:         []string{"h264"},
			AudioCodecs:         []string{"aac", "mp3"},
			SupportsHLS:         true,
		}
	}
}

package recordings

import (
	"context"

	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
)

// ResolveCapabilities implements the SSOT (Single Source of Truth) for playback capabilities.
// It follows the normative rules of ADR-P7:
// 1. Explicit-only profile selection (default: web_conservative).
// 2. No server-side extension of client-provided caps (v3.1).
// 3. Bit-for-bit parity with fixtures.
func ResolveCapabilities(
	ctx context.Context,
	principal string, // or auth.Principal
	apiVersion string,
	requestedProfile string,
	headers map[string]string,
	clientCaps *capabilities.PlaybackCapabilities,
) capabilities.PlaybackCapabilities {
	// 1. v3.1 Branch: If client caps are provided, use them immutably (except constraints)
	if clientCaps != nil && clientCaps.CapabilitiesVersion > 0 {
		return applyServerConstraints(*clientCaps)
	}

	// 2. Manual Profile Selection
	profile := requestedProfile
	if profile == "" {
		profile = string(ProfileGeneric) // Defaults to web_conservative via mapping
	}

	// 3. Resolve identity-bound profile fixture values
	caps := getProfileCaps(profile)

	// 4. Return canonicalized result
	return capabilities.CanonicalizeCapabilities(caps)
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

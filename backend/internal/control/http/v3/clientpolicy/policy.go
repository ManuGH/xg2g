package clientpolicy

import (
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/normalize"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

// ResolveProfileUserAgent centralizes the client-specific profile-resolution UA
// contract so start resolution does not have to encode Safari/iPhone rules
// inline.
func ResolveProfileUserAgent(requestedPlaybackMode, clientFamily, requestUserAgent string) string {
	switch normalize.Token(requestedPlaybackMode) {
	case "", "native_hls":
		// iPhone native HLS follows the explicit runtime packaging contract rather
		// than Safari UA sniffing. Other start paths keep the historical UA-based
		// profile resolution behavior.
		if normalize.Token(requestedPlaybackMode) == "native_hls" &&
			normalize.Token(clientFamily) == playbackprofile.ClientIOSSafariNative {
			return ""
		}
		return requestUserAgent
	case "hlsjs":
		switch normalize.Token(clientFamily) {
		case playbackprofile.ClientSafariNative, playbackprofile.ClientIOSSafariNative:
			return requestUserAgent
		default:
			return ""
		}
	default:
		return ""
	}
}

// ApplyStartPackagingPolicy centralizes client-specific start-time packaging
// safeguards. The policy only mutates the resolved execution shape, not the
// selected profile identity.
func ApplyStartPackagingPolicy(clientFamily, effectiveProfileID string, profileSpec model.ProfileSpec) model.ProfileSpec {
	if normalize.Token(clientFamily) == playbackprofile.ClientIOSSafariNative &&
		profiles.NormalizeRequestedProfileID(effectiveProfileID) == profiles.ProfileAV1HW &&
		strings.EqualFold(strings.TrimSpace(profileSpec.Container), "mpegts") {
		// iPhone AV1 stays on fMP4 even when the global AV1 MPEG-TS experiment is
		// enabled for other playback paths.
		profileSpec.Container = "fmp4"
	}
	return profileSpec
}

// WantsFMP4Packaging captures the client-side preference for native HLS/fMP4
// packaging. This was previously encoded inline in playback-info rewrites.
func WantsFMP4Packaging(requestedProfile, clientFamily string) bool {
	if profiles.PrefersNativeFMP4Packaging(requestedProfile) {
		return true
	}

	switch normalize.Token(clientFamily) {
	case "android_native", "android_tv_native", playbackprofile.ClientSafariNative, playbackprofile.ClientIOSSafariNative:
		return true
	default:
		return false
	}
}

// AllowExperimentalNativeAV1TransportStream captures the one allowed exception
// to the native fMP4 packaging preference: desktop Safari may keep the AV1 TS
// experiment when runtime capabilities and the selected target both agree.
func AllowExperimentalNativeAV1TransportStream(
	resolvedCaps capabilities.PlaybackCapabilities,
	selectedVideoCodec string,
	target playbackprofile.TargetPlaybackProfile,
) bool {
	if !config.ParseBool("XG2G_EXPERIMENTAL_AV1_MPEGTS_ENABLED", false) {
		return false
	}
	if normalize.Token(resolvedCaps.ClientFamilyFallback) != playbackprofile.ClientSafariNative {
		return false
	}
	switch normalize.Token(resolvedCaps.ClientCapsSource) {
	case capabilities.ClientCapsSourceRuntime, capabilities.ClientCapsSourceRuntimePlusFam:
	default:
		return false
	}
	if !hasToken(resolvedCaps.VideoCodecs, "av1") {
		return false
	}
	if !hasToken(resolvedCaps.Containers, "ts") && !hasToken(resolvedCaps.Containers, "mpegts") {
		return false
	}
	if normalize.Token(selectedVideoCodec) != "av1" && normalize.Token(target.Video.Codec) != "av1" {
		return false
	}
	if normalize.Token(target.Container) != "mpegts" &&
		target.Packaging != playbackprofile.PackagingTS &&
		normalize.Token(target.HLS.SegmentContainer) != "mpegts" {
		return false
	}
	return true
}

func hasToken(values []string, want string) bool {
	want = normalize.Token(want)
	if want == "" {
		return false
	}
	for _, value := range values {
		if normalize.Token(value) == want {
			return true
		}
	}
	return false
}

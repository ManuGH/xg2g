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
func ApplyStartPackagingPolicy(clientFamily, effectiveProfileID string, profileSpec model.ProfileSpec, sourceVideoCodec, preferredEngine string) model.ProfileSpec {
	switch normalize.Token(clientFamily) {
	case playbackprofile.ClientSafariNative, playbackprofile.ClientIOSSafariNative:
	default:
		return profileSpec
	}
	if requiresNativeWebKitFMP4(effectiveProfileID) &&
		strings.EqualFold(strings.TrimSpace(profileSpec.Container), "mpegts") {
		// Native WebKit codec-specific starts stay on fMP4/CMAF. MPEG-TS can make
		// HEVC/AV1 sessions look healthy at HTTP/HLS level while native playback
		// still shows black video or stalls.
		profileSpec.Container = "fmp4"
	}
	if copyCodec, ok := nativeWebKitCopyCodec(profileSpec, sourceVideoCodec, preferredEngine); ok {
		// Native WebKit copy starts use one CMAF-style packaging contract for both
		// Apple-native codecs: H.264 becomes fMP4/avc1 and HEVC becomes fMP4/hvc1.
		// The native-engine gate keeps hls.js/MSE traffic on its existing path.
		profileSpec.Container = "fmp4"
		// Pin the copied source codec so FFmpeg emits the matching sample entry.
		// TranscodeVideo remains false, therefore this still resolves to -c:v copy.
		profileSpec.VideoCodec = copyCodec
	}
	return profileSpec
}

// nativeWebKitCopyCodec returns the copied Apple-native video codec that must be
// represented by the fMP4 sample entry (avc1 for H.264, hvc1 for HEVC).
func nativeWebKitCopyCodec(profileSpec model.ProfileSpec, sourceVideoCodec, preferredEngine string) (string, bool) {
	container := strings.TrimSpace(profileSpec.Container)
	containerOK := strings.EqualFold(container, "mpegts") || strings.EqualFold(container, "fmp4")
	if profileSpec.TranscodeVideo || !containerOK || !strings.EqualFold(strings.TrimSpace(preferredEngine), "native") {
		return "", false
	}
	switch normalize.Token(sourceVideoCodec) {
	case "h264", "hevc":
		return normalize.Token(sourceVideoCodec), true
	default:
		return "", false
	}
}

func requiresNativeWebKitFMP4(profileID string) bool {
	switch profiles.NormalizeRequestedProfileID(profileID) {
	case profiles.ProfileAV1HW,
		profiles.ProfileSafariHEVC,
		profiles.ProfileSafariHEVCHW,
		profiles.ProfileSafariHEVCHWLL:
		return true
	default:
		return false
	}
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

// AllowsExperimentalNativeAV1TransportStream intentionally returns false for
// native WebKit: the validated production path is AV1 in fMP4 HLS, not AV1 in
// MPEG-TS. Keep the function for central policy readability at call sites.
func AllowExperimentalNativeAV1TransportStream(
	resolvedCaps capabilities.PlaybackCapabilities,
	selectedVideoCodec string,
	target playbackprofile.TargetPlaybackProfile,
) bool {
	if normalize.Token(resolvedCaps.ClientFamilyFallback) == playbackprofile.ClientSafariNative ||
		normalize.Token(resolvedCaps.ClientFamilyFallback) == playbackprofile.ClientIOSSafariNative {
		return false
	}
	if !config.ParseBool("XG2G_EXPERIMENTAL_AV1_MPEGTS_ENABLED", false) {
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

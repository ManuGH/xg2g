package clientpolicy

import (
	"strings"

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
	if isNativeHevcCopyStart(profileSpec, sourceVideoCodec, preferredEngine) {
		// An HEVC live SOURCE copied (no transcode) for a Safari client that
		// explicitly asked for the native WebKit HLS engine must be fMP4/hvc1:
		// native Safari HLS does not play HEVC in MPEG-TS, and hls.js/MSE cannot
		// sustain 4K@50 HEVC Main10/HLG (it stalls). An Apple HW decoder (e.g. M4)
		// plays HEVC fMP4/hvc1 natively. Gated on preferredEngine=="native", which
		// the WebUI sends only for HEVC sources when the native-HEVC experiment
		// flag is on — so flag-off traffic is byte-for-byte unchanged, and only
		// HEVC sources flip (H.264 copy stays MPEG-TS for the hls.js/MSE path).
		profileSpec.Container = "fmp4"
		// The copy path defaults VideoCodec to "" -> planCodec resolves "h264", so
		// appendLiveVideoContainerTags would skip the hvc1 tag and FFmpeg writes an
		// hev1 fMP4 that native Safari HLS will not play ("HEVC is not hvc1").
		// Pin the output video codec to hevc so the existing fMP4 hvc1-tag path
		// engages. This stays a COPY: Name != "" keeps usesLegacyCPUDefaults false
		// and TranscodeVideo is untouched, so buildCopyVideoArgs (-c:v copy) is used.
		profileSpec.VideoCodec = "hevc"
	}
	return profileSpec
}

// isNativeHevcCopyStart reports whether this is an HEVC live source being copied
// (not transcoded) for a client that requested the native WebKit HLS engine.
// Both the MPEG-TS browser default (desktop Safari) and the already-fMP4 native
// profile (iOS Safari) must land here: the former needs the container flipped to
// fMP4, and BOTH need VideoCodec pinned to hevc so appendLiveVideoContainerTags
// emits the hvc1 sample entry. Without it iOS writes hev1 + in-band parameter
// sets, which the HLG/HDR decoder re-initialises on every keyframe (visible flash).
func isNativeHevcCopyStart(profileSpec model.ProfileSpec, sourceVideoCodec, preferredEngine string) bool {
	container := strings.TrimSpace(profileSpec.Container)
	containerOK := strings.EqualFold(container, "mpegts") || strings.EqualFold(container, "fmp4")
	return !profileSpec.TranscodeVideo &&
		containerOK &&
		strings.EqualFold(strings.TrimSpace(sourceVideoCodec), "hevc") &&
		strings.EqualFold(strings.TrimSpace(preferredEngine), "native")
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
	return AllowExperimentalNativeAV1TransportStreamWithPolicy(resolvedCaps, selectedVideoCodec, target, false)
}

func AllowExperimentalNativeAV1TransportStreamWithPolicy(
	resolvedCaps capabilities.PlaybackCapabilities,
	selectedVideoCodec string,
	target playbackprofile.TargetPlaybackProfile,
	enabled bool,
) bool {
	if normalize.Token(resolvedCaps.ClientFamilyFallback) == playbackprofile.ClientSafariNative ||
		normalize.Token(resolvedCaps.ClientFamilyFallback) == playbackprofile.ClientIOSSafariNative {
		return false
	}
	if !enabled {
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

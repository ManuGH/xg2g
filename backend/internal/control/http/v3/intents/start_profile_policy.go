package intents

import (
	"fmt"

	"github.com/ManuGH/xg2g/internal/control/http/v3/autocodec"
	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	recordingdecision "github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/normalize"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
)

const defaultStartProfileID = "universal"

type startProfilePolicyInput struct {
	RequestedPlaybackMode string
	ClientFamily          string
	RequestedCodecs       string
	ClientCaps            *capabilities.PlaybackCapabilities
	Capability            *scan.Capability
	HWAccelMode           profiles.HWAccelMode
	HostRuntime           playbackprofile.HostRuntimeSnapshot
}

func resolveRequestedStartProfilePolicy(input startProfilePolicyInput) (string, error) {
	if input.RequestedPlaybackMode != "" {
		baseProfileID, err := mapPlaybackModeToProfile(input.RequestedPlaybackMode, input.ClientFamily)
		if err != nil {
			return "", err
		}
		switch input.RequestedPlaybackMode {
		case "transcode", "hlsjs":
			if shouldPreferMappedStartProfile(input.RequestedPlaybackMode, input.ClientFamily, input.Capability, input.ClientCaps) {
				return copyPreferredStartProfile(input.RequestedPlaybackMode, input.ClientFamily), nil
			}
			if picked := pickProfileForCodecsWithHost(input.RequestedCodecs, input.ClientFamily, input.HWAccelMode, input.HostRuntime); picked != "" {
				return picked, nil
			}
		case "native_hls":
			// When native HLS has a real runtime probe, trust the runtime-signaled
			// heavy-codec path before falling back to the legacy Safari copy/remux
			// shortcut. This keeps stream.start aligned with /live/stream-info when
			// the latter already determined that native HLS must transcode.
			if shouldPreferRuntimeNativeHLSProfile(input.ClientCaps) {
				if picked := pickNativeHLSProfileWithHost(input.RequestedCodecs, input.ClientFamily, input.ClientCaps, input.HWAccelMode, input.HostRuntime); picked != "" {
					return picked, nil
				}
			}
			if shouldPreferMappedStartProfile(input.RequestedPlaybackMode, input.ClientFamily, input.Capability, input.ClientCaps) {
				return copyPreferredStartProfile(input.RequestedPlaybackMode, input.ClientFamily), nil
			}
			if picked := pickNativeHLSProfileWithHost(input.RequestedCodecs, input.ClientFamily, input.ClientCaps, input.HWAccelMode, input.HostRuntime); picked != "" &&
				!shouldPreferRuntimeNativeHLSProfile(input.ClientCaps) {
				return picked, nil
			}
		default:
			if shouldPreferMappedStartProfile(input.RequestedPlaybackMode, input.ClientFamily, input.Capability, input.ClientCaps) {
				return copyPreferredStartProfile(input.RequestedPlaybackMode, input.ClientFamily), nil
			}
		}
		return baseProfileID, nil
	}

	if shouldPreferMappedStartProfile("", input.ClientFamily, input.Capability, input.ClientCaps) {
		return defaultStartProfileID, nil
	}
	if picked := pickProfileForCodecsWithHost(input.RequestedCodecs, input.ClientFamily, input.HWAccelMode, input.HostRuntime); picked != "" {
		return picked, nil
	}
	return defaultStartProfileID, nil
}

func copyPreferredStartProfile(requestedPlaybackMode, clientFamily string) string {
	switch normalize.Token(requestedPlaybackMode) {
	case "native_hls":
		return profiles.ProfileSafari
	case "android_native":
		return profiles.ProfileAndroid
	case "hlsjs", "direct_mp4":
		return profiles.ProfileHigh
	case "":
		return defaultStartProfileID
	default:
		return mapCopyFallbackProfile(clientFamily)
	}
}

func mapCopyFallbackProfile(clientFamily string) string {
	switch normalize.Token(clientFamily) {
	case playbackprofile.ClientSafariNative, playbackprofile.ClientIOSSafariNative:
		return profiles.ProfileSafari
	default:
		return profiles.ProfileHigh
	}
}

func shouldPreferMappedStartProfile(requestedPlaybackMode, clientFamily string, capability *scan.Capability, clientCaps *capabilities.PlaybackCapabilities) bool {
	if !sourceVideoCanStayOnCopyPath(capability, clientCaps) {
		return false
	}

	switch normalize.Token(requestedPlaybackMode) {
	case "native_hls":
		switch normalize.Token(clientFamily) {
		case playbackprofile.ClientSafariNative, playbackprofile.ClientIOSSafariNative:
			return true
		default:
			return false
		}
	case "hlsjs":
		return true
	case "direct_mp4":
		return true
	case "":
		return true
	default:
		return false
	}
}

func sourceVideoCanStayOnCopyPath(capability *scan.Capability, clientCaps *capabilities.PlaybackCapabilities) bool {
	if capability == nil {
		return false
	}

	normalized := capability.Normalized()
	videoCodec := normalize.Token(normalized.VideoCodec)
	if videoCodec == "" {
		videoCodec = normalize.Token(normalized.Codec)
	}
	if videoCodec == "" {
		return false
	}

	if clientCaps == nil {
		// Without an explicit capability snapshot we only trust H.264 as the
		// universal browser-safe copy path.
		return videoCodec == "h264" && !normalized.Interlaced
	}
	if !clientCaps.SupportsHLS {
		return false
	}

	return recordingdecision.CanKeepVideoCopy(
		recordingdecision.Source{
			Container:  normalized.Container,
			VideoCodec: videoCodec,
			AudioCodec: normalize.Token(normalized.AudioCodec),
			Width:      normalized.Width,
			Height:     normalized.Height,
			FPS:        normalized.FPS,
			Interlaced: normalized.Interlaced,
		},
		recordingdecision.FromCapabilities(*clientCaps),
	)
}

func mapPlaybackModeToProfile(mode, clientFamily string) (string, error) {
	switch mode {
	case "native_hls":
		// native_hls is the Safari/iOS native HLS path:
		// progressive inputs stay remux/copy, interlaced or unknown inputs transcode.
		// More aggressive recovery (safari_dirty / repair) is handled after runtime errors.
		return profiles.ProfileSafari, nil
	case "android_native":
		// Android ExoPlayer: video copy + AAC in mpegts.
		// Separate from native_hls to avoid fMP4 codec-parameter issues.
		return profiles.ProfileAndroid, nil
	case "hlsjs":
		return profiles.ProfileH264FMP4, nil
	case "direct_mp4":
		return profiles.ProfileHigh, nil
	case "transcode":
		return profiles.ProfileH264FMP4, nil
	case "deny":
		return "", fmt.Errorf("playback_mode=deny cannot start a live session")
	default:
		return "", fmt.Errorf("unsupported playback_mode: %q", mode)
	}
}

func pickProfileForCodecsWithHost(raw, clientFamily string, hwaccelMode profiles.HWAccelMode, hostRuntime playbackprofile.HostRuntimeSnapshot) string {
	return autocodec.PickProfileForCodecsForClientAndHost(raw, clientFamily, hwaccelMode, hostRuntime)
}

func pickNativeHLSProfileWithHost(raw, clientFamily string, clientCaps *capabilities.PlaybackCapabilities, hwaccelMode profiles.HWAccelMode, hostRuntime playbackprofile.HostRuntimeSnapshot) string {
	return autocodec.PickNativeHLSProfileForClientAndHost(raw, clientFamily, clientCaps, hwaccelMode, hostRuntime)
}

func shouldPreferRuntimeNativeHLSProfile(clientCaps *capabilities.PlaybackCapabilities) bool {
	if clientCaps == nil {
		return false
	}
	if clientCaps.RuntimeProbeUsed {
		return true
	}
	switch normalize.Token(clientCaps.ClientCapsSource) {
	case capabilities.ClientCapsSourceRuntime, capabilities.ClientCapsSourceRuntimePlusFam:
		return true
	default:
		return false
	}
}

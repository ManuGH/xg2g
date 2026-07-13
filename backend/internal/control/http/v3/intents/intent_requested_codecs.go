package intents

import (
	"github.com/ManuGH/xg2g/internal/control/http/v3/autocodec"
	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/normalize"
	"strings"
)

func shouldTraceAutoCodecDecision(intent Intent, requestedCodecs string) bool {
	return strings.TrimSpace(intent.Params["profile"]) == "" && strings.TrimSpace(requestedCodecs) != ""
}

func requestedCodecsForIntent(intent Intent, requestedPlaybackMode string) string {
	return requestedCodecsForIntentWithPolicy(intent, requestedPlaybackMode, false)
}

func requestedCodecsForIntentWithPolicy(intent Intent, requestedPlaybackMode string, clientAV1Disabled bool) string {
	if explicit := joinRequestedCodecs(autocodec.ParseCodecList(intent.Params["codecs"])); explicit != "" {
		return clampRequestedCodecsForClientWithPolicy(intent, requestedPlaybackMode, explicit, clientAV1Disabled)
	}
	if derived := clampRequestedCodecsForClientWithPolicy(intent, requestedPlaybackMode, requestedCodecsFromClientCapsWithPolicy(intent, requestedPlaybackMode, clientAV1Disabled), clientAV1Disabled); derived != "" {
		return derived
	}
	return clampRequestedCodecsForClientWithPolicy(intent, requestedPlaybackMode, requestedCodecsFromClientMatrix(intent, requestedPlaybackMode), clientAV1Disabled)
}

func requestedCodecsFromClientCaps(intent Intent, requestedPlaybackMode string) string {
	return requestedCodecsFromClientCapsWithPolicy(intent, requestedPlaybackMode, false)
}

func requestedCodecsFromClientCapsWithPolicy(intent Intent, requestedPlaybackMode string, clientAV1Disabled bool) string {
	clientCaps := intent.ClientCaps
	if clientCaps == nil {
		return ""
	}

	codecs := append([]string(nil), autocodec.ResolveAutoTranscodeCodecsWithPolicy(*clientCaps, clientAV1Disabled)...)
	if requestedPlaybackMode == "native_hls" || requestedPlaybackMode == "" {
		source := normalize.Token(clientCaps.ClientCapsSource)
		if source == capabilities.ClientCapsSourceRuntime || source == capabilities.ClientCapsSourceRuntimePlusFam {
			if autocodec.ClientAV1PlaybackAllowedWithPolicy(*clientCaps, clientFamilyForIntent(intent), clientAV1Disabled) {
				codecs = append(codecs, "av1")
			}
		}
		if source == capabilities.ClientCapsSourceRuntime ||
			source == capabilities.ClientCapsSourceRuntimePlusFam ||
			source == capabilities.ClientCapsSourceFamilyFallback {
			if clientCapsHasCodec(clientCaps.VideoCodecs, "hevc") {
				codecs = append(codecs, "hevc")
			}
		}
	}
	if clientCapsHasCodec(clientCaps.VideoCodecs, "h264") || len(codecs) == 0 {
		codecs = append(codecs, "h264")
	}
	return joinRequestedCodecs(mergeRequestedCodecLists(codecs, matrixFallbackVideoCodecs(intent, requestedPlaybackMode)))
}

func clampRequestedCodecsForClient(intent Intent, requestedPlaybackMode, requestedCodecs string) string {
	return clampRequestedCodecsForClientWithPolicy(intent, requestedPlaybackMode, requestedCodecs, false)
}

func clampRequestedCodecsForClientWithPolicy(intent Intent, requestedPlaybackMode, requestedCodecs string, clientAV1Disabled bool) string {
	allowedCodecs := allowedRequestedCodecsForClientWithPolicy(intent, requestedPlaybackMode, clientAV1Disabled)
	clientFamily := clientFamilyForIntent(intent)
	if clientFamily == playbackprofile.ClientIOSSafariNative &&
		startPlaybackPath(intent, requestedPlaybackMode) == "hlsjs" &&
		!iosSafariManagedAV1Allowed(intent.ClientCaps) {
		allowedCodecs = []string{"h264"}
	}
	return mergeRequestedCodecsWithAllowed(requestedCodecs, allowedCodecs)
}

func requestedCodecsFromClientMatrix(intent Intent, requestedPlaybackMode string) string {
	return joinRequestedCodecs(matrixFallbackVideoCodecs(intent, requestedPlaybackMode))
}

func allowedRequestedCodecsForClient(intent Intent, requestedPlaybackMode string) []string {
	return allowedRequestedCodecsForClientWithPolicy(intent, requestedPlaybackMode, false)
}

func allowedRequestedCodecsForClientWithPolicy(intent Intent, requestedPlaybackMode string, clientAV1Disabled bool) []string {
	canonicalCaps := normalizedClientCaps(intent.ClientCaps)
	if canonicalCaps != nil && len(canonicalCaps.VideoCodecs) > 0 {
		codecs := preferredRequestedCodecOrder(canonicalCaps.VideoCodecs)
		if !autocodec.ClientAV1PlaybackAllowedWithPolicy(*canonicalCaps, clientFamilyForIntent(intent), clientAV1Disabled) {
			codecs = removeRequestedCodec(codecs, "av1")
		}
		return mergeRequestedCodecLists(codecs, matrixFallbackVideoCodecs(intent, requestedPlaybackMode))
	}
	return matrixFallbackVideoCodecs(intent, requestedPlaybackMode)
}

func removeRequestedCodec(codecs []string, blocked string) []string {
	out := make([]string, 0, len(codecs))
	for _, codec := range codecs {
		if normalize.Token(codec) == blocked {
			continue
		}
		out = append(out, codec)
	}
	return out
}

func matrixFallbackVideoCodecs(intent Intent, requestedPlaybackMode string) []string {
	clientFamily := clientFamilyForIntent(intent)
	switch startPlaybackPath(intent, requestedPlaybackMode) {
	case "hlsjs":
		switch clientFamily {
		case playbackprofile.ClientSafariNative,
			playbackprofile.ClientIOSSafariNative,
			playbackprofile.ClientFirefoxHLSJS,
			playbackprofile.ClientAndroidTVBrowser,
			playbackprofile.ClientChromiumHLSJS:
			return []string{"h264"}
		}
	case "android_native":
		return []string{"h264"}
	}

	if fixture, ok := playbackprofile.ClientFixture(clientFamily); ok {
		return preferredRequestedCodecOrder(fixture.VideoCodecs)
	}

	switch clientFamily {
	case playbackprofile.ClientSafariNative, playbackprofile.ClientIOSSafariNative:
		return []string{"hevc", "h264"}
	case playbackprofile.ClientFirefoxHLSJS, playbackprofile.ClientAndroidTVBrowser, playbackprofile.ClientChromiumHLSJS:
		return []string{"h264"}
	default:
		return nil
	}
}

func startPlaybackPath(intent Intent, requestedPlaybackMode string) string {
	switch normalize.Token(requestedPlaybackMode) {
	case "native_hls", "hlsjs", "transcode", "direct_mp4", "android_native":
		return normalize.Token(requestedPlaybackMode)
	}
	switch preferredEngineForIntent(intent) {
	case "hlsjs":
		return "hlsjs"
	case "native":
		return "native_hls"
	default:
		return ""
	}
}

func preferredRequestedCodecOrder(codecs []string) []string {
	out := make([]string, 0, 3)
	for _, codec := range []string{"av1", "hevc", "h264"} {
		if clientCapsHasCodec(codecs, codec) {
			out = append(out, codec)
		}
	}
	return out
}

func mergeRequestedCodecsWithAllowed(requestedCodecs string, allowedCodecs []string) string {
	requested := autocodec.ParseCodecList(requestedCodecs)
	allowed := preferredRequestedCodecOrder(allowedCodecs)

	if len(requested) == 0 {
		return joinRequestedCodecs(allowed)
	}
	if len(allowed) == 0 {
		return joinRequestedCodecs(requested)
	}

	allowedSet := make(map[string]struct{}, len(allowed))
	for _, codec := range allowed {
		allowedSet[codec] = struct{}{}
	}

	merged := make([]string, 0, len(allowed))
	for _, codec := range requested {
		if _, ok := allowedSet[codec]; !ok {
			continue
		}
		merged = append(merged, codec)
	}
	for _, codec := range allowed {
		if clientCapsHasCodec(merged, codec) {
			continue
		}
		merged = append(merged, codec)
	}
	return joinRequestedCodecs(merged)
}

func mergeRequestedCodecLists(primary, secondary []string) []string {
	combined := make([]string, 0, len(primary)+len(secondary))
	combined = append(combined, primary...)
	combined = append(combined, secondary...)
	return autocodec.ParseCodecList(joinRequestedCodecs(combined))
}

func iosSafariManagedAV1Allowed(clientCaps *capabilities.PlaybackCapabilities) bool {
	canonicalCaps := normalizedClientCaps(clientCaps)
	if canonicalCaps == nil {
		return false
	}

	source := normalize.Token(canonicalCaps.ClientCapsSource)
	if source != capabilities.ClientCapsSourceRuntime && source != capabilities.ClientCapsSourceRuntimePlusFam {
		return false
	}
	if normalize.Token(canonicalCaps.PreferredHLSEngine) != "hlsjs" {
		return false
	}
	if !clientCapsHasCodec(canonicalCaps.HLSEngines, "hlsjs") {
		return false
	}
	if !clientCapsHasCodec(canonicalCaps.Containers, "fmp4") {
		return false
	}
	if !clientCapsHasCodec(canonicalCaps.VideoCodecs, "av1") {
		return false
	}

	for _, signal := range canonicalCaps.VideoCodecSignals {
		if normalize.Token(signal.Codec) != "av1" {
			continue
		}
		if signal.Supported {
			return true
		}
		if signal.Smooth != nil && *signal.Smooth {
			return true
		}
	}
	return false
}

func joinRequestedCodecs(codecs []string) string {
	if len(codecs) == 0 {
		return ""
	}
	seen := make(map[string]struct{}, len(codecs))
	ordered := make([]string, 0, len(codecs))
	for _, raw := range codecs {
		canonical := canonicalRequestedCodec(raw)
		if canonical == "" {
			continue
		}
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}
		ordered = append(ordered, canonical)
	}
	return strings.Join(ordered, ",")
}

func canonicalRequestedCodec(raw string) string {
	parsed := autocodec.ParseCodecList(raw)
	if len(parsed) == 0 {
		return ""
	}
	return parsed[0]
}

func clientCapsHasCodec(values []string, want string) bool {
	want = normalize.Token(want)
	for _, value := range values {
		if normalize.Token(value) == want {
			return true
		}
	}
	return false
}

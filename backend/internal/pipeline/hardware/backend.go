package hardware

import (
	"strings"

	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

func PreferredGPUBackend() profiles.GPUBackend {
	for _, codec := range []string{"h264", "hevc", "av1"} {
		if backend := PreferredGPUBackendForCodec(codec); backend != profiles.GPUBackendNone {
			return backend
		}
	}
	if IsVAAPIReady() {
		return profiles.GPUBackendVAAPI
	}
	if IsNVENCReady() {
		return profiles.GPUBackendNVENC
	}
	return profiles.GPUBackendNone
}

func PreferredGPUBackendForCodec(codec string) profiles.GPUBackend {
	backend, _, ok := bestHardwareEncoderCapability(codec, false)
	if !ok {
		return profiles.GPUBackendNone
	}
	return backend
}

func SupportedHardwareCodecs() []string {
	return supportedHardwareCodecs(false)
}

func AutoHardwareCodecs() []string {
	return supportedHardwareCodecs(true)
}

func IsHardwareEncoderReady(codec string) bool {
	_, _, ok := bestHardwareEncoderCapability(codec, false)
	return ok
}

func IsHardwareEncoderAutoEligible(codec string) bool {
	_, _, ok := bestHardwareEncoderCapability(codec, true)
	return ok
}

func HardwareEncoderCapabilityFor(codec string) (HardwareEncoderCapability, profiles.GPUBackend, bool) {
	backend, cap, ok := bestHardwareEncoderCapability(codec, false)
	return cap, backend, ok
}

func BackendForHWAccel(hwaccel string) profiles.GPUBackend {
	switch strings.ToLower(strings.TrimSpace(hwaccel)) {
	case "vaapi", "vaapi_encode_only":
		return profiles.GPUBackendVAAPI
	case "nvenc":
		return profiles.GPUBackendNVENC
	default:
		return profiles.GPUBackendNone
	}
}

func supportedHardwareCodecs(autoOnly bool) []string {
	out := make([]string, 0, 3)
	for _, codec := range []string{"h264", "hevc", "av1"} {
		_, _, ok := bestHardwareEncoderCapability(codec, autoOnly)
		if ok {
			out = append(out, codec)
		}
	}
	return out
}

func bestHardwareEncoderCapability(codec string, autoOnly bool) (profiles.GPUBackend, HardwareEncoderCapability, bool) {
	var (
		bestBackend profiles.GPUBackend
		bestCap     HardwareEncoderCapability
		ok          bool
	)

	for _, backend := range []profiles.GPUBackend{profiles.GPUBackendVAAPI, profiles.GPUBackendNVENC} {
		cap, exists := hardwareEncoderCapability(codec, backend)
		if !exists || !cap.Verified {
			continue
		}
		if autoOnly && !cap.AutoEligible {
			continue
		}
		if !ok || betterHardwareEncoderCapability(backend, cap, bestBackend, bestCap) {
			bestBackend = backend
			bestCap = cap
			ok = true
		}
	}

	return bestBackend, bestCap, ok
}

func hardwareEncoderCapability(codec string, backend profiles.GPUBackend) (HardwareEncoderCapability, bool) {
	encoder, ok := encoderNameForBackend(codec, backend)
	if !ok {
		return HardwareEncoderCapability{}, false
	}

	switch backend {
	case profiles.GPUBackendVAAPI:
		return VAAPIEncoderCapabilityFor(encoder)
	case profiles.GPUBackendNVENC:
		return NVENCEncoderCapabilityFor(encoder)
	default:
		return HardwareEncoderCapability{}, false
	}
}

func encoderNameForBackend(codec string, backend profiles.GPUBackend) (string, bool) {
	switch normalizeCodec(codec) {
	case "h264":
		switch backend {
		case profiles.GPUBackendVAAPI:
			return "h264_vaapi", true
		case profiles.GPUBackendNVENC:
			return "h264_nvenc", true
		}
	case "hevc":
		switch backend {
		case profiles.GPUBackendVAAPI:
			return "hevc_vaapi", true
		case profiles.GPUBackendNVENC:
			return "hevc_nvenc", true
		}
	case "av1":
		switch backend {
		case profiles.GPUBackendVAAPI:
			return "av1_vaapi", true
		case profiles.GPUBackendNVENC:
			return "av1_nvenc", true
		}
	}
	return "", false
}

func betterHardwareEncoderCapability(candidateBackend profiles.GPUBackend, candidateCap HardwareEncoderCapability, bestBackend profiles.GPUBackend, bestCap HardwareEncoderCapability) bool {
	if candidateCap.AutoEligible != bestCap.AutoEligible {
		return candidateCap.AutoEligible
	}

	candidateMeasured := candidateCap.ProbeElapsed > 0
	bestMeasured := bestCap.ProbeElapsed > 0
	if candidateMeasured != bestMeasured {
		return candidateMeasured
	}
	if candidateMeasured && candidateCap.ProbeElapsed != bestCap.ProbeElapsed {
		return candidateCap.ProbeElapsed < bestCap.ProbeElapsed
	}

	return backendRank(candidateBackend) < backendRank(bestBackend)
}

func backendRank(backend profiles.GPUBackend) int {
	switch backend {
	case profiles.GPUBackendVAAPI:
		return 0
	case profiles.GPUBackendNVENC:
		return 1
	default:
		return 2
	}
}

func normalizeCodec(codec string) string {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "h264", "avc", "avc1", "libx264", "h264_vaapi", "h264_nvenc":
		return "h264"
	case "hevc", "h265", "h.265", "libx265", "hevc_vaapi", "hevc_nvenc":
		return "hevc"
	case "av1", "av01", "av1_vaapi", "av1_nvenc", "libsvtav1", "libaom-av1":
		return "av1"
	default:
		return strings.ToLower(strings.TrimSpace(codec))
	}
}

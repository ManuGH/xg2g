// Package capability derives FFmpeg hardware-encoder and profile capabilities
// from benchmark samples, and owns the auto-promotion ratio policy. Extracting
// it from the LocalAdapter god-file makes encoder capabilities first-class and
// independently testable.
package capability

import (
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

// Auto-promotion ratio policy: relative startup-cost thresholds for automatic
// codec promotion above H.264.
const (
	DefaultHEVCVAAPIAutoRatioMax = 1.75
	DefaultAV1VAAPIAutoRatioMax  = 2.50
	DefaultHEVCNVENCAutoRatioMax = 1.75
	DefaultAV1NVENCAutoRatioMax  = 2.50
)

func DeriveVAAPIEncoderCapabilities(samples map[string]time.Duration, hevcRatioMax, av1RatioMax float64) map[string]hardware.VAAPIEncoderCapability {
	return DeriveHardwareEncoderCapabilities(samples, hevcRatioMax, av1RatioMax)
}

func DeriveNVENCEncoderCapabilities(samples map[string]time.Duration, hevcRatioMax, av1RatioMax float64) map[string]hardware.NVENCEncoderCapability {
	return DeriveHardwareEncoderCapabilities(samples, hevcRatioMax, av1RatioMax)
}

func DeriveHardwareEncoderCapabilities(samples map[string]time.Duration, hevcRatioMax, av1RatioMax float64) map[string]hardware.HardwareEncoderCapability {
	if len(samples) == 0 {
		return nil
	}

	caps := make(map[string]hardware.HardwareEncoderCapability, len(samples))
	baseline, ok := SelectHardwareAutoBaseline(samples)
	if !ok {
		return caps
	}

	for encoder, elapsed := range samples {
		if elapsed <= 0 {
			continue
		}
		normEncoder := strings.ToLower(strings.TrimSpace(encoder))
		cap := hardware.HardwareEncoderCapability{
			Verified:     true,
			ProbeElapsed: elapsed,
			AutoEligible: strings.HasPrefix(normEncoder, "h264_"),
		}
		if !cap.AutoEligible {
			ratio := float64(elapsed) / float64(baseline)
			switch normEncoder {
			case "hevc_vaapi", "hevc_nvenc":
				cap.AutoEligible = ratio <= hevcRatioMax
			case "av1_vaapi", "av1_nvenc":
				cap.AutoEligible = ratio <= av1RatioMax
			default:
				cap.AutoEligible = true
			}
		}
		caps[normEncoder] = cap
	}

	return caps
}

func SelectHardwareAutoBaseline(samples map[string]time.Duration) (time.Duration, bool) {
	for _, key := range []string{"h264_vaapi", "h264_nvenc"} {
		for enc, elapsed := range samples {
			if strings.ToLower(strings.TrimSpace(enc)) == key && elapsed > 0 {
				return elapsed, true
			}
		}
	}
	var baseline time.Duration
	for _, elapsed := range samples {
		if elapsed <= 0 {
			continue
		}
		if baseline == 0 || elapsed < baseline {
			baseline = elapsed
		}
	}
	return baseline, baseline > 0
}

func DeriveProfileCapabilities(samples map[string]time.Duration) map[string]hardware.HardwareProfileCapability {
	if len(samples) == 0 {
		return nil
	}

	caps := make(map[string]hardware.HardwareProfileCapability, len(samples))
	for profileID, elapsed := range samples {
		if elapsed <= 0 {
			continue
		}
		caps[strings.ToLower(strings.TrimSpace(profileID))] = hardware.HardwareProfileCapability{
			Verified:     true,
			ProbeElapsed: elapsed,
		}
	}
	return caps
}

func BetterLocalHardwareCapability(candidateBackend profiles.GPUBackend, candidateCap hardware.HardwareEncoderCapability, bestBackend profiles.GPUBackend, bestCap hardware.HardwareEncoderCapability) bool {
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

	switch candidateBackend {
	case profiles.GPUBackendVAAPI:
		return bestBackend != profiles.GPUBackendVAAPI
	case profiles.GPUBackendNVENC:
		return bestBackend == profiles.GPUBackendNone
	default:
		return false
	}
}

func EncoderNameForBackend(codec string, backend profiles.GPUBackend) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(codec)) {
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

package playbackplanner

import (
	"sort"
	"strings"
)

const unmeasuredEncoderProbeMS = int64(24 * 60 * 60 * 1000)

type autoCodecCandidate struct {
	codec             string
	probeElapsedMS    int64
	tieOrder          int
	quality           int
	benchmarkClass    string
	nativeCPUFallback bool
}

func selectAutoTranscodeVideoCodec(ev PlaybackEvidence) (string, bool) {
	if requiresPlannedTranscode(ev) || !usesAutoTranscodeProfile(ev.RequestedIntent) || len(ev.ClientEvidence.AutoTranscodeVideoCodecs) == 0 {
		return "", false
	}

	encoders := make(map[string]HostEncoderCapability, len(ev.HostSnapshot.EncoderCapabilities))
	for _, encoder := range ev.HostSnapshot.EncoderCapabilities {
		encoders[strings.ToLower(strings.TrimSpace(encoder.Codec))] = encoder
	}

	candidates := make([]autoCodecCandidate, 0, len(ev.ClientEvidence.AutoTranscodeVideoCodecs))
	for _, raw := range ev.ClientEvidence.AutoTranscodeVideoCodecs {
		codec := strings.ToLower(strings.TrimSpace(raw))
		encoder, found := encoders[codec]
		switch codec {
		case "h264":
			probeMS := unmeasuredEncoderProbeMS
			benchmarkClass := ""
			if found {
				benchmarkClass = encoder.BenchmarkClass
				if encoder.Verified && encoder.AutoEligible && encoder.ProbeElapsedMS > 0 {
					probeMS = encoder.ProbeElapsedMS
				}
			}
			candidates = append(candidates, newAutoCodecCandidate(codec, probeMS, benchmarkClass))
		case "hevc":
			if found && encoder.Verified && encoder.AutoEligible && encoder.ProbeElapsedMS > 0 {
				candidates = append(candidates, newAutoCodecCandidate(codec, encoder.ProbeElapsedMS, encoder.BenchmarkClass))
				continue
			}
			if nativeWebKitClient(ev.ClientEvidence.Family) {
				benchmarkClass := ev.HostSnapshot.BenchmarkClass
				if found && encoder.BenchmarkClass != "" {
					benchmarkClass = encoder.BenchmarkClass
				}
				candidate := newAutoCodecCandidate(codec, unmeasuredEncoderProbeMS, benchmarkClass)
				candidate.nativeCPUFallback = true
				candidates = append(candidates, candidate)
			}
		case "av1":
			if !found || !encoder.Verified || !encoder.AutoEligible || encoder.ProbeElapsedMS <= 0 {
				continue
			}
			candidates = append(candidates, newAutoCodecCandidate(codec, encoder.ProbeElapsedMS, encoder.BenchmarkClass))
		}
	}
	if len(candidates) == 0 {
		return "", false
	}

	hostAware := hostPerformanceTier(ev.HostSnapshot.PerformanceClass) >= 0
	if !hostAware {
		for _, candidate := range candidates {
			if hostBenchmarkTier(candidate.benchmarkClass) >= 0 {
				hostAware = true
				break
			}
		}
	}

	if hostAware {
		eligible := candidates[:0]
		for _, candidate := range candidates {
			if autoCodecAllowedForHost(candidate, ev.HostSnapshot.PerformanceClass) {
				eligible = append(eligible, candidate)
			}
		}
		if len(eligible) == 0 {
			return "", false
		}
		candidates = eligible
		sort.SliceStable(candidates, func(i, j int) bool {
			leftStrength := autoCodecHostStrength(candidates[i], ev.HostSnapshot.PerformanceClass)
			rightStrength := autoCodecHostStrength(candidates[j], ev.HostSnapshot.PerformanceClass)
			if leftStrength != rightStrength {
				return leftStrength > rightStrength
			}
			if candidates[i].quality != candidates[j].quality {
				return candidates[i].quality > candidates[j].quality
			}
			if candidates[i].probeElapsedMS != candidates[j].probeElapsedMS {
				return candidates[i].probeElapsedMS < candidates[j].probeElapsedMS
			}
			return candidates[i].tieOrder < candidates[j].tieOrder
		})
	} else {
		sort.SliceStable(candidates, func(i, j int) bool {
			// Legacy native-HLS selection deliberately keeps a CPU HEVC path
			// ahead of the generic H.264 fallback when HEVC hardware is absent.
			// AV1 hardware candidates still compete by measured probe time.
			if candidates[i].nativeCPUFallback && candidates[j].codec == "h264" {
				return true
			}
			if candidates[j].nativeCPUFallback && candidates[i].codec == "h264" {
				return false
			}
			if candidates[i].probeElapsedMS != candidates[j].probeElapsedMS {
				return candidates[i].probeElapsedMS < candidates[j].probeElapsedMS
			}
			return candidates[i].tieOrder < candidates[j].tieOrder
		})
	}

	return candidates[0].codec, true
}

func nativeWebKitClient(family string) bool {
	switch strings.ToLower(strings.TrimSpace(family)) {
	case "safari_native", "ios_safari_native":
		return true
	default:
		return false
	}
}

func usesAutoTranscodeProfile(requestedIntent string) bool {
	switch strings.ToLower(strings.TrimSpace(requestedIntent)) {
	case "direct", "copy", "passthrough", "compatible", "high", "bandwidth", "low", "repair", "h264_fmp4", "safari_dirty":
		return false
	default:
		return true
	}
}

func newAutoCodecCandidate(codec string, probeElapsedMS int64, benchmarkClass string) autoCodecCandidate {
	candidate := autoCodecCandidate{codec: codec, probeElapsedMS: probeElapsedMS, benchmarkClass: benchmarkClass}
	switch codec {
	case "av1":
		candidate.tieOrder = 2
		candidate.quality = 2
	case "hevc":
		candidate.tieOrder = 1
		candidate.quality = 1
	}
	return candidate
}

func autoCodecAllowedForHost(candidate autoCodecCandidate, performanceClass string) bool {
	if tier := hostPerformanceTier(performanceClass); tier >= 0 && tier < minimumPerformanceTier(candidate.codec) {
		return false
	}
	if tier := hostBenchmarkTier(candidate.benchmarkClass); tier >= 0 && tier < minimumBenchmarkTier(candidate.codec) {
		return false
	}
	return true
}

func autoCodecHostStrength(candidate autoCodecCandidate, performanceClass string) int {
	performanceTier := hostPerformanceTier(performanceClass)
	benchmarkTier := hostBenchmarkTier(candidate.benchmarkClass)
	switch {
	case performanceTier >= 0 && benchmarkTier >= 0:
		if performanceTier < benchmarkTier {
			return performanceTier
		}
		return benchmarkTier
	case performanceTier >= 0:
		return performanceTier
	default:
		return benchmarkTier
	}
}

func hostPerformanceTier(raw string) int {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "low":
		return 0
	case "medium":
		return 1
	case "high", "ultra":
		return 2
	default:
		return -1
	}
}

func hostBenchmarkTier(raw string) int {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "weak":
		return 0
	case "moderate":
		return 1
	case "strong":
		return 2
	default:
		return -1
	}
}

func minimumPerformanceTier(codec string) int {
	switch codec {
	case "av1":
		return 2
	case "hevc":
		return 1
	default:
		return 0
	}
}

func minimumBenchmarkTier(codec string) int {
	switch codec {
	case "av1", "hevc":
		return 1
	default:
		return 0
	}
}

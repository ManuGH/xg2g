package hardware

import "github.com/ManuGH/xg2g/internal/domain/playbackprofile"

const (
	fastH264ProbeThresholdMS = 160
	fastHEVCProbeThresholdMS = 220
	fastAV1ProbeThresholdMS  = 180
)

func classifyHostPerformanceClass(cpu playbackprofile.HostCPUSnapshot, concurrency playbackprofile.HostConcurrencySnapshot, caps playbackprofile.ServerTranscodeCapabilities) string {
	if !caps.FFmpegAvailable {
		return ""
	}

	baseClass := classifyStaticHostPerformanceClass(cpu.CoreCount, caps)
	return applyPerformancePenalty(baseClass, runtimePerformancePenalty(cpu, concurrency))
}

func classifyStaticHostPerformanceClass(coreCount int, caps playbackprofile.ServerTranscodeCapabilities) string {
	h264Cap, _, hasH264HW := HardwareEncoderCapabilityFor("h264")
	hevcCap, _, hasHEVCHW := HardwareEncoderCapabilityFor("hevc")
	av1Cap, _, hasAV1HW := HardwareEncoderCapabilityFor("av1")
	h264Fast := hasH264HW && h264Cap.ProbeElapsed > 0 && h264Cap.ProbeElapsed.Milliseconds() <= fastH264ProbeThresholdMS
	hevcFast := hasHEVCHW && hevcCap.ProbeElapsed > 0 && hevcCap.ProbeElapsed.Milliseconds() <= fastHEVCProbeThresholdMS
	av1Fast := hasAV1HW && av1Cap.ProbeElapsed > 0 && av1Cap.ProbeElapsed.Milliseconds() <= fastAV1ProbeThresholdMS

	switch {
	case coreCount >= 12 && (av1Fast || hevcFast):
		return "ultra"
	case (coreCount >= 8 && hevcFast) || (coreCount >= 10 && h264Fast):
		return "high"
	case hasH264HW || hasHEVCHW || coreCount >= 4:
		return "medium"
	default:
		return "low"
	}
}

func runtimePerformancePenalty(cpu playbackprofile.HostCPUSnapshot, concurrency playbackprofile.HostConcurrencySnapshot) int {
	penalty := 0

	if cpu.CoreCount > 0 && cpu.SampleCount >= minHostCPUSamples {
		cpuRatio := cpu.Load1m / float64(cpu.CoreCount)
		switch {
		case cpuRatio >= criticalCPULoadPerCore:
			penalty = 2
		case cpuRatio >= constrainedCPULoadPerCore:
			penalty = 1
		}
	}

	if cpu.CoreCount > 0 {
		transcodeDensity := float64(concurrency.TranscodesActive) / float64(cpu.CoreCount)
		switch {
		case concurrency.TranscodesActive >= 4 && transcodeDensity >= 0.75:
			penalty = maxInt(penalty, 2)
		case concurrency.TranscodesActive >= 2 && transcodeDensity >= 0.50:
			penalty = maxInt(penalty, 1)
		}
	}

	return penalty
}

func applyPerformancePenalty(baseClass string, penalty int) string {
	classes := []string{"low", "medium", "high", "ultra"}
	index := 0
	for i, class := range classes {
		if baseClass == class {
			index = i
			break
		}
	}
	index -= penalty
	if index < 0 {
		index = 0
	}
	return classes[index]
}

func maxInt(a, b int) int {
	if b > a {
		return b
	}
	return a
}

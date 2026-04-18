package hardware

import (
	"time"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
)

const (
	hostBenchmarkClassWeak     = "weak"
	hostBenchmarkClassModerate = "moderate"
	hostBenchmarkClassStrong   = "strong"
)

func snapshotHostBenchmark() playbackprofile.HostBenchmarkSnapshot {
	codecs := []string{"h264", "hevc", "av1"}
	results := make([]playbackprofile.HostCodecBenchmark, 0, len(codecs))

	var (
		preferredCodec   string
		preferredBackend string
		fastestElapsed   time.Duration
		hasMeasured      bool
	)

	for _, codec := range codecs {
		capability, backend, ok := HardwareEncoderCapabilityFor(codec)
		if !ok || !capability.Verified || capability.ProbeElapsed <= 0 {
			continue
		}
		results = append(results, playbackprofile.HostCodecBenchmark{
			Codec:          codec,
			Class:          classifyHostBenchmarkClass(capability.ProbeElapsed),
			Backend:        string(backend),
			ProbeElapsedMs: capability.ProbeElapsed.Milliseconds(),
			AutoEligible:   capability.AutoEligible,
		})
		if !hasMeasured || capability.ProbeElapsed < fastestElapsed {
			hasMeasured = true
			fastestElapsed = capability.ProbeElapsed
			preferredCodec = codec
			preferredBackend = string(backend)
		}
	}

	if !hasMeasured {
		return playbackprofile.HostBenchmarkSnapshot{}
	}

	return playbackprofile.HostBenchmarkSnapshot{
		Class:                 classifyHostBenchmarkClass(fastestElapsed),
		PreferredCodec:        preferredCodec,
		PreferredBackend:      preferredBackend,
		FastestProbeElapsedMs: fastestElapsed.Milliseconds(),
		Codecs:                results,
		Profiles:              snapshotProfileBenchmarks(results),
	}
}

func classifyHostBenchmarkClass(elapsed time.Duration) string {
	switch {
	case elapsed > 0 && elapsed <= 70*time.Millisecond:
		return hostBenchmarkClassStrong
	case elapsed > 0 && elapsed <= 160*time.Millisecond:
		return hostBenchmarkClassModerate
	case elapsed > 0:
		return hostBenchmarkClassWeak
	default:
		return ""
	}
}

func deriveProfileBenchmarks(codecs []playbackprofile.HostCodecBenchmark) []playbackprofile.HostProfileBenchmark {
	h264, ok := codecBenchmark(codecs, "h264")
	if !ok {
		return nil
	}

	baseElapsed := time.Duration(h264.ProbeElapsedMs) * time.Millisecond
	baseClass := h264.Class
	return []playbackprofile.HostProfileBenchmark{
		{
			ProfileID:      playbackprofile.BenchmarkProfileVideoH2641080P,
			Codec:          "h264",
			Class:          baseClass,
			Backend:        h264.Backend,
			ProbeElapsedMs: h264.ProbeElapsedMs,
		},
		{
			ProfileID:      playbackprofile.BenchmarkProfileVideoH2641080I,
			Codec:          "h264",
			Class:          classifyHostBenchmarkClass(baseElapsed * 7 / 4),
			Backend:        h264.Backend,
			ProbeElapsedMs: (baseElapsed * 7 / 4).Milliseconds(),
		},
		{
			ProfileID:      playbackprofile.BenchmarkProfileVideoH2641080I50,
			Codec:          "h264",
			Class:          classifyHostBenchmarkClass(baseElapsed * 7 / 2),
			Backend:        h264.Backend,
			ProbeElapsedMs: (baseElapsed * 7 / 2).Milliseconds(),
		},
		{
			ProfileID:      playbackprofile.BenchmarkProfileVideoH2642160P,
			Codec:          "h264",
			Class:          classifyHostBenchmarkClass(baseElapsed * 5 / 2),
			Backend:        h264.Backend,
			ProbeElapsedMs: (baseElapsed * 5 / 2).Milliseconds(),
		},
		{
			ProfileID:      playbackprofile.BenchmarkProfileVideoH2642160P50,
			Codec:          "h264",
			Class:          classifyHostBenchmarkClass(baseElapsed * 5),
			Backend:        h264.Backend,
			ProbeElapsedMs: (baseElapsed * 5).Milliseconds(),
		},
	}
}

func snapshotProfileBenchmarks(codecs []playbackprofile.HostCodecBenchmark) []playbackprofile.HostProfileBenchmark {
	actual := make([]playbackprofile.HostProfileBenchmark, 0, 6)
	seen := make(map[string]struct{}, 6)
	for _, profileID := range []string{
		playbackprofile.BenchmarkProfileAudioAACStereo,
		playbackprofile.BenchmarkProfileVideoH2641080P,
		playbackprofile.BenchmarkProfileVideoH2641080I,
		playbackprofile.BenchmarkProfileVideoH2641080I50,
		playbackprofile.BenchmarkProfileVideoH2642160P,
		playbackprofile.BenchmarkProfileVideoH2642160P50,
	} {
		capability, backend, ok := HardwareProfileCapabilityFor(profileID)
		if !ok || !capability.Verified || capability.ProbeElapsed <= 0 {
			continue
		}
		seen[profileID] = struct{}{}
		codec := "h264"
		if profileID == playbackprofile.BenchmarkProfileAudioAACStereo {
			codec = "aac"
		}
		actual = append(actual, playbackprofile.HostProfileBenchmark{
			ProfileID:      profileID,
			Codec:          codec,
			Class:          classifyHostBenchmarkClass(capability.ProbeElapsed),
			Backend:        backend,
			ProbeElapsedMs: capability.ProbeElapsed.Milliseconds(),
		})
	}

	derived := deriveProfileBenchmarks(codecs)
	for _, benchmark := range derived {
		if _, ok := seen[benchmark.ProfileID]; ok {
			continue
		}
		actual = append(actual, benchmark)
	}

	return actual
}

func codecBenchmark(codecs []playbackprofile.HostCodecBenchmark, codec string) (playbackprofile.HostCodecBenchmark, bool) {
	for _, benchmark := range codecs {
		if benchmark.Codec == codec {
			return benchmark, true
		}
	}
	return playbackprofile.HostCodecBenchmark{}, false
}

package playbackprofile

import "testing"

func TestCanonicalizeHostRuntime_NormalizesNegativeValues(t *testing.T) {
	got := CanonicalizeHostRuntime(HostRuntimeSnapshot{
		Capabilities: ServerTranscodeCapabilities{
			FFmpegAvailable:    true,
			HLSAvailable:       true,
			HardwareVideoCodec: []string{" HEVC ", "h264", "hevc"},
		},
		CPU: HostCPUSnapshot{
			Load1m:        -1,
			CoreCount:     -4,
			SampleCount:   -3,
			WindowSeconds: -30,
		},
		Concurrency: HostConcurrencySnapshot{
			TunersAvailable:   -1,
			SessionsActive:    -2,
			TranscodesActive:  -3,
			ActiveVAAPITokens: -4,
		},
	})

	if got.CPU.Load1m != 0 {
		t.Fatalf("expected Load1m to clamp to zero, got %v", got.CPU.Load1m)
	}
	if got.CPU.CoreCount != 0 || got.CPU.SampleCount != 0 || got.CPU.WindowSeconds != 0 {
		t.Fatalf("expected CPU counts to clamp to zero, got %#v", got.CPU)
	}
	if got.Concurrency.TunersAvailable != 0 || got.Concurrency.SessionsActive != 0 || got.Concurrency.TranscodesActive != 0 || got.Concurrency.ActiveVAAPITokens != 0 {
		t.Fatalf("expected concurrency counts to clamp to zero, got %#v", got.Concurrency)
	}
	if got.Concurrency.MaxSessions != 0 || got.Concurrency.MaxVAAPITokens != 0 {
		t.Fatalf("expected concurrency limits to clamp to zero, got %#v", got.Concurrency)
	}
	if len(got.Capabilities.HardwareVideoCodec) != 2 || got.Capabilities.HardwareVideoCodec[0] != "h264" || got.Capabilities.HardwareVideoCodec[1] != "hevc" {
		t.Fatalf("expected canonicalized hardware codec set, got %#v", got.Capabilities.HardwareVideoCodec)
	}
}

func TestBenchmarkClassForCodec_PrefersCodecSpecificClassBeforeAggregate(t *testing.T) {
	got := BenchmarkClassForCodec(HostBenchmarkSnapshot{
		Class: "strong",
		Codecs: []HostCodecBenchmark{
			{Codec: "h264", Class: "weak"},
			{Codec: "hevc", Class: "strong"},
		},
	}, " H264 ")

	if got != "weak" {
		t.Fatalf("expected codec-specific h264 class, got %q", got)
	}
}

func TestBenchmarkClassForCodec_FallsBackToAggregateClass(t *testing.T) {
	got := BenchmarkClassForCodec(HostBenchmarkSnapshot{
		Class: "moderate",
	}, "av1")

	if got != "moderate" {
		t.Fatalf("expected aggregate benchmark fallback, got %q", got)
	}
}

func TestBenchmarkClassForProfile_PrefersProfileSpecificClassBeforeAggregate(t *testing.T) {
	got := BenchmarkClassForProfile(HostBenchmarkSnapshot{
		Class: "strong",
		Profiles: []HostProfileBenchmark{
			{ProfileID: BenchmarkProfileVideoH2641080I, Class: "weak"},
		},
	}, " VIDEO_H264_1080I ")

	if got != "weak" {
		t.Fatalf("expected profile-specific benchmark class, got %q", got)
	}
}

func TestBenchmarkClassForProfile_FallsBackToAggregateClass(t *testing.T) {
	got := BenchmarkClassForProfile(HostBenchmarkSnapshot{
		Class: "moderate",
	}, BenchmarkProfileVideoH2642160P)

	if got != "moderate" {
		t.Fatalf("expected aggregate profile fallback, got %q", got)
	}
}

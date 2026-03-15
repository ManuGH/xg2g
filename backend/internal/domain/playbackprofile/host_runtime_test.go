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

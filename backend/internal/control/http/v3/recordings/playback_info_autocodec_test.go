package recordings

import (
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

func TestPickPlaybackInfoAutoProfile_UsesAV1OnlyOnHealthyHost(t *testing.T) {
	hardware.SetVAAPIPreflightResult(true)
	hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{
		"av1_vaapi":  {Verified: true, AutoEligible: true, ProbeElapsed: 30 * time.Millisecond},
		"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 40 * time.Millisecond},
		"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 10 * time.Millisecond},
	})
	t.Cleanup(func() {
		hardware.SetVAAPIPreflightResult(false)
		hardware.SetVAAPIEncoderCapabilities(nil)
	})

	resolvedCaps := capabilities.PlaybackCapabilities{
		ClientFamilyFallback: playbackprofile.ClientIOSSafariNative,
		ClientCapsSource:     capabilities.ClientCapsSourceRuntimePlusFam,
		VideoCodecs:          []string{"av1", "hevc", "h264"},
	}
	healthyHost := playbackprofile.HostRuntimeSnapshot{
		PerformanceClass: "high",
		Benchmark: playbackprofile.HostBenchmarkSnapshot{
			Codecs: []playbackprofile.HostCodecBenchmark{
				{Codec: "av1", Class: "strong"},
				{Codec: "hevc", Class: "strong"},
				{Codec: "h264", Class: "strong"},
			},
		},
	}

	if got := pickPlaybackInfoAutoProfile(resolvedCaps, healthyHost); got != profiles.ProfileAV1HW {
		t.Fatalf("pickPlaybackInfoAutoProfile() = %q, want %q", got, profiles.ProfileAV1HW)
	}

	mediumHost := healthyHost
	mediumHost.PerformanceClass = "medium"
	if got := pickPlaybackInfoAutoProfile(resolvedCaps, mediumHost); got != profiles.ProfileSafariHEVCHW {
		t.Fatalf("pickPlaybackInfoAutoProfile() on medium host = %q, want %q", got, profiles.ProfileSafariHEVCHW)
	}
}

func TestAlignAutoCodecDecision_PersistsNeutralSelectionTrace(t *testing.T) {
	hardware.SetVAAPIPreflightResult(true)
	hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{
		"av1_vaapi":  {Verified: true, AutoEligible: true, ProbeElapsed: 30 * time.Millisecond},
		"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 40 * time.Millisecond},
		"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 10 * time.Millisecond},
	})
	t.Cleanup(func() {
		hardware.SetVAAPIPreflightResult(false)
		hardware.SetVAAPIEncoderCapabilities(nil)
	})

	allowTranscode := true
	dec := &decision.Decision{
		Mode: decision.ModeTranscode,
		Trace: decision.Trace{
			RequestedIntent: "quality",
			ResolvedIntent:  "quality",
		},
	}
	resolvedCaps := capabilities.PlaybackCapabilities{
		ClientFamilyFallback: playbackprofile.ClientIOSSafariNative,
		ClientCapsSource:     capabilities.ClientCapsSourceRuntimePlusFam,
		AllowTranscode:       &allowTranscode,
		VideoCodecs:          []string{"av1", "hevc", "h264"},
		VideoCodecSignals: []capabilities.VideoCodecSignal{
			{Codec: "av1", Supported: true, PowerEfficient: boolPtr(true)},
			{Codec: "hevc", Supported: true, PowerEfficient: boolPtr(true)},
			{Codec: "h264", Supported: true, PowerEfficient: boolPtr(true)},
		},
	}
	hostRuntime := playbackprofile.HostRuntimeSnapshot{
		PerformanceClass: "medium",
		Benchmark: playbackprofile.HostBenchmarkSnapshot{
			Codecs: []playbackprofile.HostCodecBenchmark{
				{Codec: "av1", Class: "strong"},
				{Codec: "hevc", Class: "strong"},
				{Codec: "h264", Class: "strong"},
			},
		},
	}

	alignAutoCodecDecision(PlaybackInfoRequest{
		RequestedProfile: "quality",
		Capabilities:     &resolvedCaps,
	}, resolvedCaps, hostRuntime, dec)

	if dec.Trace.AutoCodecPolicy != "host_aware_bottleneck" {
		t.Fatalf("expected host-aware policy, got %#v", dec.Trace)
	}
	if dec.Trace.AutoCodecRequested != "av1,hevc,h264" {
		t.Fatalf("expected requested codecs trace, got %#v", dec.Trace)
	}
	if dec.Trace.AutoCodecSelected != "hevc" {
		t.Fatalf("expected selected codec hevc, got %#v", dec.Trace)
	}
	if dec.Trace.AutoCodecHostClass != "medium" {
		t.Fatalf("expected host class medium, got %#v", dec.Trace)
	}
	if dec.Trace.AutoCodecBenchClass != "strong" {
		t.Fatalf("expected benchmark class strong, got %#v", dec.Trace)
	}
}

func boolPtr(v bool) *bool {
	return &v
}

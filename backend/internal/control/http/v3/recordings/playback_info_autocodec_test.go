package recordings

import (
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/control/http/v3/autocodec"
	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"github.com/stretchr/testify/require"
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
		Containers:           []string{"mp4", "ts", "fmp4"},
		VideoCodecs:          []string{"av1", "hevc", "h264"},
		RuntimeProbeUsed:     true,
		DeviceType:           "iphone",
		DeviceContext:        &capabilities.DeviceContext{OSName: "ios", OSVersion: "17.5", Model: "iPhone 15 Pro A17 Pro"},
		VideoCodecSignals: []capabilities.VideoCodecSignal{
			{Codec: "av1", Supported: true, PowerEfficient: boolPtr(true)},
			{Codec: "hevc", Supported: true, PowerEfficient: boolPtr(true)},
			{Codec: "h264", Supported: true, PowerEfficient: boolPtr(true)},
		},
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

	if got := pickPlaybackInfoAutoProfileWithPolicy(resolvedCaps, healthyHost, false, autocodec.ResolveIOSNativeHEVCHWMode()); got != profiles.ProfileAV1HW {
		t.Fatalf("pickPlaybackInfoAutoProfile() = %q, want %q", got, profiles.ProfileAV1HW)
	}

	mediumHost := healthyHost
	mediumHost.PerformanceClass = "medium"
	if got := pickPlaybackInfoAutoProfileWithPolicy(resolvedCaps, mediumHost, false, autocodec.ResolveIOSNativeHEVCHWMode()); got != profiles.ProfileSafariHEVCHW {
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
		Containers:           []string{"mp4", "ts", "fmp4"},
		VideoCodecs:          []string{"av1", "hevc", "h264"},
		RuntimeProbeUsed:     true,
		DeviceType:           "iphone",
		DeviceContext:        &capabilities.DeviceContext{OSName: "ios", OSVersion: "17.5", Model: "iPhone 15 Pro A17 Pro"},
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

	alignAutoCodecDecisionWithPolicy(PlaybackInfoRequest{
		RequestedProfile: "quality",
		Capabilities:     &resolvedCaps,
	}, resolvedCaps, hostRuntime, profiles.Resolver{}, false, autocodec.ResolveIOSNativeHEVCHWMode(), dec)

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

func TestPlannerAutoTranscodeVideoCodecsRequiresExplicitRequestCapabilities(t *testing.T) {
	resolved := capabilities.PlaybackCapabilities{
		ClientFamilyFallback: playbackprofile.ClientSafariNative,
		VideoCodecs:          []string{"hevc", "h264"},
	}
	require.Empty(t, plannerAutoTranscodeVideoCodecs(PlaybackInfoRequest{}, resolved, false))
}

func TestPlannerAutoTranscodeVideoCodecsKeepsNativeHEVCWithoutSmoothSignal(t *testing.T) {
	requestCaps := capabilities.PlaybackCapabilities{VideoCodecs: []string{"hevc", "h264"}}
	resolved := capabilities.PlaybackCapabilities{
		ClientFamilyFallback: playbackprofile.ClientSafariNative,
		VideoCodecs:          []string{"hevc", "h264"},
	}

	require.Equal(t,
		[]string{"hevc", "h264"},
		plannerAutoTranscodeVideoCodecs(PlaybackInfoRequest{Capabilities: &requestCaps}, resolved, false),
	)
}

func boolPtr(v bool) *bool {
	return &v
}

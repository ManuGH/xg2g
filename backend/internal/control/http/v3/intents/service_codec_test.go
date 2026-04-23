package intents

import (
	"context"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/control/http/v3/autocodec"
	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

func TestPickProfileForCodecs_AutoUsesMeasuredHostRanking(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		raw          string
		hwaccel      profiles.HWAccelMode
		capabilities map[string]hardware.VAAPIEncoderCapability
		want         string
	}{
		{
			name:    "picks the fastest measured heavy codec instead of input order",
			raw:     "hevc,av1,h264",
			hwaccel: profiles.HWAccelAuto,
			capabilities: map[string]hardware.VAAPIEncoderCapability{
				"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 90 * time.Millisecond},
				"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 70 * time.Millisecond},
				"av1_vaapi":  {Verified: true, AutoEligible: true, ProbeElapsed: 50 * time.Millisecond},
			},
			want: profiles.ProfileAV1HW,
		},
		{
			name:    "falls through to h264 when heavy codecs are not auto eligible",
			raw:     "hevc,h264",
			hwaccel: profiles.HWAccelAuto,
			capabilities: map[string]hardware.VAAPIEncoderCapability{
				"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 90 * time.Millisecond},
				"hevc_vaapi": {Verified: true, AutoEligible: false, ProbeElapsed: 40 * time.Millisecond},
			},
			want: profiles.ProfileH264FMP4,
		},
		{
			name:    "does not auto-promote hevc cpu when only hevc is hinted",
			raw:     "hevc",
			hwaccel: profiles.HWAccelAuto,
			capabilities: map[string]hardware.VAAPIEncoderCapability{
				"hevc_vaapi": {Verified: true, AutoEligible: false, ProbeElapsed: 40 * time.Millisecond},
			},
			want: "",
		},
		{
			name:    "hwaccel off disables heavy hw auto-promotion",
			raw:     "av1,hevc,h264",
			hwaccel: profiles.HWAccelOff,
			capabilities: map[string]hardware.VAAPIEncoderCapability{
				"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 90 * time.Millisecond},
				"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 40 * time.Millisecond},
				"av1_vaapi":  {Verified: true, AutoEligible: true, ProbeElapsed: 30 * time.Millisecond},
			},
			want: profiles.ProfileH264FMP4,
		},
		{
			name:    "uses h264 cpu fallback when it is the only acceptable codec",
			raw:     "h264",
			hwaccel: profiles.HWAccelAuto,
			want:    profiles.ProfileH264FMP4,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := autocodec.PickProfileForCodecsWithCapabilities(tt.raw, tt.hwaccel, tt.capabilities)
			if got != tt.want {
				t.Fatalf("pickProfileForCodecs(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestPickNativeHLSProfileForCodecs_PrefersAV1HWOnSafariNative(t *testing.T) {
	hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{
		"av1_vaapi": {
			Verified:     true,
			AutoEligible: true,
			ProbeElapsed: 30 * time.Millisecond,
		},
	})
	t.Cleanup(func() {
		hardware.SetVAAPIEncoderCapabilities(nil)
	})

	got := autocodec.PickNativeHLSProfileForCodecs("av1,hevc,h264", "ios_safari_native", profiles.HWAccelAuto)
	if got != profiles.ProfileAV1HW {
		t.Fatalf("pickNativeHLSProfileForCodecs() = %q, want %q", got, profiles.ProfileAV1HW)
	}
}

func TestPickProfileForCodecsForClient_IOSNativeHEVCSelectionStaysOnHWProfile(t *testing.T) {
	hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{
		"hevc_vaapi": {
			Verified:     true,
			AutoEligible: true,
			ProbeElapsed: 40 * time.Millisecond,
		},
		"h264_vaapi": {
			Verified:     true,
			AutoEligible: true,
			ProbeElapsed: 90 * time.Millisecond,
		},
	})
	t.Cleanup(func() {
		hardware.SetVAAPIEncoderCapabilities(nil)
	})

	got := autocodec.PickProfileForCodecsForClient("hevc,h264", playbackprofile.ClientIOSSafariNative, profiles.HWAccelAuto)
	if got != profiles.ProfileSafariHEVCHW {
		t.Fatalf("PickProfileForCodecsForClient() = %q, want %q", got, profiles.ProfileSafariHEVCHW)
	}
}

func TestPickProfileForCodecsWithCapabilitiesAndHost_PrefersAV1OnlyOnHealthyHost(t *testing.T) {
	caps := map[string]hardware.VAAPIEncoderCapability{
		"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 10 * time.Millisecond},
		"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 40 * time.Millisecond},
		"av1_vaapi":  {Verified: true, AutoEligible: true, ProbeElapsed: 30 * time.Millisecond},
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
	got := autocodec.PickProfileForCodecsWithCapabilitiesAndHost("av1,hevc,h264", profiles.HWAccelAuto, caps, healthyHost)
	if got != profiles.ProfileAV1HW {
		t.Fatalf("PickProfileForCodecsWithCapabilitiesAndHost() = %q, want %q", got, profiles.ProfileAV1HW)
	}

	mediumHost := healthyHost
	mediumHost.PerformanceClass = "medium"
	got = autocodec.PickProfileForCodecsWithCapabilitiesAndHost("av1,hevc,h264", profiles.HWAccelAuto, caps, mediumHost)
	if got != profiles.ProfileSafariHEVCHW {
		t.Fatalf("PickProfileForCodecsWithCapabilitiesAndHost() on medium host = %q, want %q", got, profiles.ProfileSafariHEVCHW)
	}
}

func TestPickProfileForCodecsWithCapabilitiesAndHost_DemotesWeakAV1BenchmarkToHEVC(t *testing.T) {
	got := autocodec.PickProfileForCodecsWithCapabilitiesAndHost(
		"av1,hevc,h264",
		profiles.HWAccelAuto,
		map[string]hardware.VAAPIEncoderCapability{
			"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 10 * time.Millisecond},
			"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 40 * time.Millisecond},
			"av1_vaapi":  {Verified: true, AutoEligible: true, ProbeElapsed: 30 * time.Millisecond},
		},
		playbackprofile.HostRuntimeSnapshot{
			PerformanceClass: "high",
			Benchmark: playbackprofile.HostBenchmarkSnapshot{
				Codecs: []playbackprofile.HostCodecBenchmark{
					{Codec: "av1", Class: "weak"},
					{Codec: "hevc", Class: "strong"},
					{Codec: "h264", Class: "strong"},
				},
			},
		},
	)
	if got != profiles.ProfileSafariHEVCHW {
		t.Fatalf("PickProfileForCodecsWithCapabilitiesAndHost() = %q, want %q", got, profiles.ProfileSafariHEVCHW)
	}
}

func TestPickProfileForCodecsForClient_IOSNativeHEVCSelectionStillPrefersHEVCOverH264WhenRequested(t *testing.T) {
	hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{
		"h264_vaapi": {
			Verified:     true,
			AutoEligible: true,
			ProbeElapsed: 10 * time.Millisecond,
		},
	})
	t.Cleanup(func() {
		hardware.SetVAAPIEncoderCapabilities(nil)
	})

	got := autocodec.PickProfileForCodecsForClient("hevc,h264", playbackprofile.ClientIOSSafariNative, profiles.HWAccelAuto)
	if got != profiles.ProfileH264FMP4 {
		t.Fatalf("PickProfileForCodecsForClient() = %q, want %q", got, profiles.ProfileH264FMP4)
	}
}

func TestPickProfileForCapabilitiesForClient_IOSNativeHEVCSelectionStaysOnHWProfile(t *testing.T) {
	hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{
		"hevc_vaapi": {
			Verified:     true,
			AutoEligible: true,
			ProbeElapsed: 40 * time.Millisecond,
		},
		"h264_vaapi": {
			Verified:     true,
			AutoEligible: true,
			ProbeElapsed: 90 * time.Millisecond,
		},
	})
	t.Cleanup(func() {
		hardware.SetVAAPIEncoderCapabilities(nil)
	})

	hevcSmooth := true
	hevcEfficient := true
	h264Smooth := true
	h264Efficient := true
	got := autocodec.PickProfileForCapabilitiesForClient(capabilities.PlaybackCapabilities{
		ClientFamilyFallback: playbackprofile.ClientIOSSafariNative,
		VideoCodecs:          []string{"hevc", "h264"},
		VideoCodecSignals: []capabilities.VideoCodecSignal{
			{Codec: "hevc", Supported: true, Smooth: &hevcSmooth, PowerEfficient: &hevcEfficient},
			{Codec: "h264", Supported: true, Smooth: &h264Smooth, PowerEfficient: &h264Efficient},
		},
	}, playbackprofile.ClientIOSSafariNative, profiles.HWAccelAuto)
	if got != profiles.ProfileSafariHEVCHW {
		t.Fatalf("PickProfileForCapabilitiesForClient() = %q, want %q", got, profiles.ProfileSafariHEVCHW)
	}
}

func TestPickNativeHLSProfileForCapabilities_RuntimeAV1DoesNotRequireTSContainer(t *testing.T) {
	t.Setenv("XG2G_AV1_VAAPI_AUTO_RATIO_MAX", "10")
	hardware.SetVAAPIPreflightResult(true)
	hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{
		"av1_vaapi": {
			Verified:     true,
			AutoEligible: true,
			ProbeElapsed: 30 * time.Millisecond,
		},
	})
	t.Cleanup(func() {
		hardware.SetVAAPIEncoderCapabilities(nil)
		hardware.SetVAAPIPreflightResult(false)
	})

	caps := &capabilities.PlaybackCapabilities{
		ClientCapsSource: capabilities.ClientCapsSourceRuntime,
		Containers:       []string{"mp4"},
		VideoCodecs:      []string{"av1", "hevc", "h264"},
	}

	got := autocodec.PickNativeHLSProfileForCapabilities("ios_safari_native", caps, profiles.HWAccelAuto)
	if got != profiles.ProfileAV1HW {
		t.Fatalf("pickNativeHLSProfileForCapabilities() = %q, want %q", got, profiles.ProfileAV1HW)
	}
}

func TestPickNativeHLSProfileForCapabilities_RuntimeHEVCPrefersHEVCOverH264(t *testing.T) {
	hardware.SetVAAPIPreflightResult(true)
	hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{
		"hevc_vaapi": {
			Verified:     true,
			AutoEligible: true,
			ProbeElapsed: 40 * time.Millisecond,
		},
		"h264_vaapi": {
			Verified:     true,
			AutoEligible: true,
			ProbeElapsed: 10 * time.Millisecond,
		},
	})
	t.Cleanup(func() {
		hardware.SetVAAPIEncoderCapabilities(nil)
		hardware.SetVAAPIPreflightResult(false)
	})

	caps := &capabilities.PlaybackCapabilities{
		ClientCapsSource: capabilities.ClientCapsSourceRuntimePlusFam,
		VideoCodecs:      []string{"hevc", "h264"},
	}

	got := autocodec.PickNativeHLSProfileForCapabilities("ios_safari_native", caps, profiles.HWAccelAuto)
	if got != profiles.ProfileSafariHEVCHW {
		t.Fatalf("pickNativeHLSProfileForCapabilities() = %q, want %q", got, profiles.ProfileSafariHEVCHW)
	}
}

func TestPickNativeHLSProfileForCapabilitiesAndHost_DemotesAV1ToHEVCOnMediumHost(t *testing.T) {
	hardware.SetVAAPIPreflightResult(true)
	hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{
		"av1_vaapi":  {Verified: true, AutoEligible: true, ProbeElapsed: 30 * time.Millisecond},
		"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 40 * time.Millisecond},
	})
	t.Cleanup(func() {
		hardware.SetVAAPIEncoderCapabilities(nil)
		hardware.SetVAAPIPreflightResult(false)
	})

	got := autocodec.PickNativeHLSProfileForCapabilitiesAndHost(
		playbackprofile.ClientIOSSafariNative,
		&capabilities.PlaybackCapabilities{
			ClientFamilyFallback: playbackprofile.ClientIOSSafariNative,
			ClientCapsSource:     capabilities.ClientCapsSourceRuntimePlusFam,
			VideoCodecs:          []string{"av1", "hevc", "h264"},
		},
		profiles.HWAccelAuto,
		playbackprofile.HostRuntimeSnapshot{
			PerformanceClass: "medium",
			Benchmark: playbackprofile.HostBenchmarkSnapshot{
				Codecs: []playbackprofile.HostCodecBenchmark{
					{Codec: "av1", Class: "strong"},
					{Codec: "hevc", Class: "strong"},
				},
			},
		},
	)
	if got != profiles.ProfileSafariHEVCHW {
		t.Fatalf("PickNativeHLSProfileForCapabilitiesAndHost() = %q, want %q", got, profiles.ProfileSafariHEVCHW)
	}
}

func TestPickNativeHLSProfileForCodecs_IOSNativeHEVCSelectionStaysOnHWProfile(t *testing.T) {
	hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{
		"hevc_vaapi": {
			Verified:     true,
			AutoEligible: true,
			ProbeElapsed: 40 * time.Millisecond,
		},
	})
	t.Cleanup(func() {
		hardware.SetVAAPIEncoderCapabilities(nil)
	})

	got := autocodec.PickNativeHLSProfileForCodecs("hevc,h264", "ios_safari_native", profiles.HWAccelAuto)
	if got != profiles.ProfileSafariHEVCHW {
		t.Fatalf("pickNativeHLSProfileForCodecs() = %q, want %q", got, profiles.ProfileSafariHEVCHW)
	}
}

func TestApplyClientCompatibilityPolicy_IOSNativeHEVCDemotesToEncodeOnly(t *testing.T) {
	profileID, spec := autocodec.ApplyClientCompatibilityPolicy(
		playbackprofile.ClientIOSSafariNative,
		profiles.ProfileSafariHEVCHW,
		model.ProfileSpec{
			Name:       profiles.ProfileSafariHEVCHW,
			HWAccel:    "vaapi",
			Container:  "fmp4",
			VideoCodec: "hevc",
		},
		func(profileID string) model.ProfileSpec {
			t.Fatalf("resolveProfileSpec must not be called, got %q", profileID)
			return model.ProfileSpec{}
		},
	)
	if profileID != profiles.ProfileSafariHEVCHW {
		t.Fatalf("ApplyClientCompatibilityPolicy() profileID = %q, want %q", profileID, profiles.ProfileSafariHEVCHW)
	}
	if spec.HWAccel != "vaapi_encode_only" {
		t.Fatalf("ApplyClientCompatibilityPolicy() hwaccel = %q, want %q", spec.HWAccel, "vaapi_encode_only")
	}
}

func TestApplyClientCompatibilityPolicy_IOSNativeHEVCKeepsFullVAAPIWhenConfigured(t *testing.T) {
	t.Setenv("XG2G_IOS_NATIVE_HEVC_HW_MODE", "full")

	profileID, spec := autocodec.ApplyClientCompatibilityPolicy(
		playbackprofile.ClientIOSSafariNative,
		profiles.ProfileSafariHEVCHW,
		model.ProfileSpec{
			Name:       profiles.ProfileSafariHEVCHW,
			HWAccel:    "vaapi",
			Container:  "fmp4",
			VideoCodec: "hevc",
		},
		func(profileID string) model.ProfileSpec {
			t.Fatalf("resolveProfileSpec must not be called, got %q", profileID)
			return model.ProfileSpec{}
		},
	)
	if profileID != profiles.ProfileSafariHEVCHW {
		t.Fatalf("ApplyClientCompatibilityPolicy() profileID = %q, want %q", profileID, profiles.ProfileSafariHEVCHW)
	}
	if spec.HWAccel != "vaapi" {
		t.Fatalf("ApplyClientCompatibilityPolicy() hwaccel = %q, want %q", spec.HWAccel, "vaapi")
	}
}

func TestApplyClientCompatibilityPolicy_IOSNativeHEVCFallsBackToCPUWhenConfigured(t *testing.T) {
	t.Setenv("XG2G_IOS_NATIVE_HEVC_HW_MODE", "cpu")

	profileID, spec := autocodec.ApplyClientCompatibilityPolicy(
		playbackprofile.ClientIOSSafariNative,
		profiles.ProfileSafariHEVCHW,
		model.ProfileSpec{
			Name:       profiles.ProfileSafariHEVCHW,
			HWAccel:    "vaapi",
			Container:  "fmp4",
			VideoCodec: "hevc",
		},
		func(profileID string) model.ProfileSpec {
			if profileID != profiles.ProfileSafariHEVC {
				t.Fatalf("resolveProfileSpec got %q, want %q", profileID, profiles.ProfileSafariHEVC)
			}
			return model.ProfileSpec{
				Name:       profiles.ProfileSafariHEVC,
				HWAccel:    "",
				Container:  "fmp4",
				VideoCodec: "hevc",
			}
		},
	)
	if profileID != profiles.ProfileSafariHEVC {
		t.Fatalf("ApplyClientCompatibilityPolicy() profileID = %q, want %q", profileID, profiles.ProfileSafariHEVC)
	}
	if spec.HWAccel != "" {
		t.Fatalf("ApplyClientCompatibilityPolicy() hwaccel = %q, want empty string", spec.HWAccel)
	}
}

func TestPickNativeHLSProfile_RuntimeH264OnlyDoesNotAssumeHEVC(t *testing.T) {
	hardware.SetVAAPIPreflightResult(true)
	hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{
		"hevc_vaapi": {
			Verified:     true,
			AutoEligible: true,
			ProbeElapsed: 40 * time.Millisecond,
		},
	})
	t.Cleanup(func() {
		hardware.SetVAAPIEncoderCapabilities(nil)
		hardware.SetVAAPIPreflightResult(false)
	})

	caps := &capabilities.PlaybackCapabilities{
		ClientCapsSource: capabilities.ClientCapsSourceRuntime,
		VideoCodecs:      []string{"h264"},
	}

	got := autocodec.PickNativeHLSProfile("h264", "ios_safari_native", caps, profiles.HWAccelAuto)
	if got != "" {
		t.Fatalf("pickNativeHLSProfile() = %q, want empty result", got)
	}
}

func TestResolveRequestedStartProfile_IOSHlsJsClampsToH264(t *testing.T) {
	hardware.SetVAAPIPreflightResult(true)
	hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{
		"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 90 * time.Millisecond},
		"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 40 * time.Millisecond},
		"av1_vaapi":  {Verified: true, AutoEligible: true, ProbeElapsed: 30 * time.Millisecond},
	})
	t.Cleanup(func() {
		hardware.SetVAAPIEncoderCapabilities(nil)
		hardware.SetVAAPIPreflightResult(false)
	})

	deps := newMockDeps()
	deps.hostRuntime = playbackprofile.HostRuntimeSnapshot{
		PerformanceClass: "high",
		Benchmark: playbackprofile.HostBenchmarkSnapshot{
			Codecs: []playbackprofile.HostCodecBenchmark{
				{Codec: "av1", Class: "strong"},
				{Codec: "hevc", Class: "strong"},
				{Codec: "h264", Class: "strong"},
			},
		},
	}
	svc := NewService(deps)

	profileID, playbackMode, err := svc.resolveRequestedStartProfile(context.Background(), Intent{
		PlaybackDecisionToken: "decision-token",
		Params: map[string]string{
			"playback_mode":             "hlsjs",
			"codecs":                    "av1,hevc,h264",
			model.CtxKeyClientFamily:    playbackprofile.ClientIOSSafariNative,
			model.CtxKeyPreferredEngine: "hlsjs",
		},
	}, profiles.HWAccelAuto)
	if err != nil {
		t.Fatalf("resolveRequestedStartProfile() error = %#v", err)
	}
	if playbackMode != "hlsjs" {
		t.Fatalf("resolveRequestedStartProfile() playbackMode = %q, want %q", playbackMode, "hlsjs")
	}
	if profileID != profiles.ProfileH264FMP4 {
		t.Fatalf("resolveRequestedStartProfile() profileID = %q, want %q", profileID, profiles.ProfileH264FMP4)
	}
}

func TestResolveRequestedStartProfile_IOSHlsJsAllowsAV1WithRuntimeManagedAV1Caps(t *testing.T) {
	hardware.SetVAAPIPreflightResult(true)
	hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{
		"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 90 * time.Millisecond},
		"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 40 * time.Millisecond},
		"av1_vaapi":  {Verified: true, AutoEligible: true, ProbeElapsed: 30 * time.Millisecond},
	})
	t.Cleanup(func() {
		hardware.SetVAAPIEncoderCapabilities(nil)
		hardware.SetVAAPIPreflightResult(false)
	})

	av1Smooth := true

	deps := newMockDeps()
	deps.hostRuntime = playbackprofile.HostRuntimeSnapshot{
		PerformanceClass: "high",
		Benchmark: playbackprofile.HostBenchmarkSnapshot{
			Codecs: []playbackprofile.HostCodecBenchmark{
				{Codec: "av1", Class: "strong"},
				{Codec: "hevc", Class: "strong"},
				{Codec: "h264", Class: "strong"},
			},
		},
	}
	svc := NewService(deps)

	profileID, playbackMode, err := svc.resolveRequestedStartProfile(context.Background(), Intent{
		PlaybackDecisionToken: "decision-token",
		Params: map[string]string{
			"playback_mode":             "hlsjs",
			"codecs":                    "av1,hevc,h264",
			model.CtxKeyClientFamily:    playbackprofile.ClientIOSSafariNative,
			model.CtxKeyPreferredEngine: "hlsjs",
		},
		ClientCaps: &capabilities.PlaybackCapabilities{
			ClientFamilyFallback: playbackprofile.ClientIOSSafariNative,
			ClientCapsSource:     capabilities.ClientCapsSourceRuntime,
			Containers:           []string{"mp4", "ts", "fmp4"},
			VideoCodecs:          []string{"av1", "hevc", "h264"},
			VideoCodecSignals: []capabilities.VideoCodecSignal{
				{Codec: "av1", Supported: true, Smooth: &av1Smooth},
			},
			HLSEngines:          []string{"native", "hlsjs"},
			PreferredHLSEngine:  "hlsjs",
			RuntimeProbeUsed:    true,
			RuntimeProbeVersion: 2,
		},
	}, profiles.HWAccelAuto)
	if err != nil {
		t.Fatalf("resolveRequestedStartProfile() error = %#v", err)
	}
	if playbackMode != "hlsjs" {
		t.Fatalf("resolveRequestedStartProfile() playbackMode = %q, want %q", playbackMode, "hlsjs")
	}
	if profileID != profiles.ProfileAV1HW {
		t.Fatalf("resolveRequestedStartProfile() profileID = %q, want %q", profileID, profiles.ProfileAV1HW)
	}
}

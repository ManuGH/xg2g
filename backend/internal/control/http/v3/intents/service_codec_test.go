package intents

import (
	"context"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/control/http/v3/autocodec"
	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/control/recordings/capreg"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
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

func TestRequestedCodecsForIntent_AndroidTVBrowserFamilyFallbackIsH264Only(t *testing.T) {
	got := requestedCodecsForIntent(Intent{
		Params: map[string]string{
			"playback_mode":          "hlsjs",
			"codecs":                 "av1,hevc,h264",
			model.CtxKeyClientFamily: playbackprofile.ClientAndroidTVBrowser,
		},
	}, "hlsjs")
	if got != "h264" {
		t.Fatalf("requestedCodecsForIntent() = %q, want h264", got)
	}
}

func TestRequestedCodecsForIntent_AndroidTVBrowserRuntimeHEVCStaysOptIn(t *testing.T) {
	smooth := true
	got := requestedCodecsForIntent(Intent{
		Params: map[string]string{
			"playback_mode":          "hlsjs",
			model.CtxKeyClientFamily: playbackprofile.ClientAndroidTVBrowser,
		},
		ClientCaps: &capabilities.PlaybackCapabilities{
			ClientFamilyFallback: playbackprofile.ClientAndroidTVBrowser,
			ClientCapsSource:     capabilities.ClientCapsSourceRuntime,
			Containers:           []string{"mp4", "ts", "fmp4"},
			VideoCodecs:          []string{"hevc", "h264"},
			AudioCodecs:          []string{"aac", "mp3"},
			SupportsHLS:          true,
			PreferredHLSEngine:   "hlsjs",
			RuntimeProbeUsed:     true,
			RuntimeProbeVersion:  2,
			VideoCodecSignals: []capabilities.VideoCodecSignal{
				{Codec: "hevc", Supported: true, Smooth: &smooth},
			},
		},
	}, "hlsjs")
	if got != "hevc,h264" {
		t.Fatalf("requestedCodecsForIntent() = %q, want hevc,h264", got)
	}
}

func TestRequestedCodecsForIntent_AndroidTVBrowserKnownAV1DeviceCanOptIntoAV1(t *testing.T) {
	smooth := true
	got := requestedCodecsForIntent(Intent{
		Params: map[string]string{
			"playback_mode":          "hlsjs",
			model.CtxKeyClientFamily: playbackprofile.ClientAndroidTVBrowser,
		},
		ClientCaps: &capabilities.PlaybackCapabilities{
			ClientFamilyFallback: playbackprofile.ClientAndroidTVBrowser,
			ClientCapsSource:     capabilities.ClientCapsSourceRuntime,
			Containers:           []string{"mp4", "ts", "fmp4"},
			VideoCodecs:          []string{"av1", "hevc", "h264"},
			AudioCodecs:          []string{"aac", "mp3"},
			SupportsHLS:          true,
			PreferredHLSEngine:   "hlsjs",
			RuntimeProbeUsed:     true,
			RuntimeProbeVersion:  2,
			DeviceType:           "android_tv",
			DeviceContext:        &capabilities.DeviceContext{OSName: "android", OSVersion: "11", SDKInt: 30, Manufacturer: "Amazon", Model: "AFTKRT"},
			VideoCodecSignals: []capabilities.VideoCodecSignal{
				{Codec: "av1", Supported: true, Smooth: &smooth},
				{Codec: "hevc", Supported: true, Smooth: &smooth},
			},
		},
	}, "hlsjs")
	if got != "av1,hevc,h264" {
		t.Fatalf("requestedCodecsForIntent() = %q, want av1,hevc,h264", got)
	}
}

func TestRequestedCodecsForIntent_AndroidTVBrowserShieldCannotOptIntoAV1(t *testing.T) {
	smooth := true
	got := requestedCodecsForIntent(Intent{
		Params: map[string]string{
			"playback_mode":          "hlsjs",
			model.CtxKeyClientFamily: playbackprofile.ClientAndroidTVBrowser,
		},
		ClientCaps: &capabilities.PlaybackCapabilities{
			ClientFamilyFallback: playbackprofile.ClientAndroidTVBrowser,
			ClientCapsSource:     capabilities.ClientCapsSourceRuntime,
			Containers:           []string{"mp4", "ts", "fmp4"},
			VideoCodecs:          []string{"av1", "h264"},
			AudioCodecs:          []string{"aac", "mp3"},
			SupportsHLS:          true,
			PreferredHLSEngine:   "hlsjs",
			RuntimeProbeUsed:     true,
			RuntimeProbeVersion:  2,
			DeviceType:           "android_tv",
			DeviceContext:        &capabilities.DeviceContext{OSName: "android", OSVersion: "11", SDKInt: 30, Manufacturer: "NVIDIA", Model: "SHIELD Android TV"},
			VideoCodecSignals: []capabilities.VideoCodecSignal{
				{Codec: "av1", Supported: true, Smooth: &smooth},
			},
		},
	}, "hlsjs")
	if got != "h264" {
		t.Fatalf("requestedCodecsForIntent() = %q, want h264", got)
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

func TestPickNativeHLSProfileForCapabilities_RuntimeAV1UsesFMP4WithoutTSContainer(t *testing.T) {
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
		Containers:       []string{"mp4", "fmp4"},
		VideoCodecs:      []string{"av1", "hevc", "h264"},
		RuntimeProbeUsed: true,
		DeviceType:       "iphone",
		DeviceContext:    &capabilities.DeviceContext{OSName: "ios", OSVersion: "17.5", Model: "iPhone 15 Pro A17 Pro"},
		VideoCodecSignals: []capabilities.VideoCodecSignal{
			{Codec: "av1", Supported: true},
		},
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
	}, profiles.HWAccelAuto, nil)
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
	}, profiles.HWAccelAuto, nil)
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

func TestResolveRequestedStartProfile_DesktopSafariNativeH264SourceKeepsSafariProfile(t *testing.T) {
	hardware.SetVAAPIPreflightResult(true)
	hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{
		"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 90 * time.Millisecond},
		"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 40 * time.Millisecond},
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
				{Codec: "hevc", Class: "strong"},
				{Codec: "h264", Class: "strong"},
			},
		},
	}
	svc := NewService(deps)

	profileID, playbackMode, err := svc.resolveRequestedStartProfile(context.Background(), Intent{
		PlaybackDecisionToken: "decision-token",
		Params: map[string]string{
			"playback_mode":             "native_hls",
			"codecs":                    "hevc,h264",
			model.CtxKeyClientFamily:    playbackprofile.ClientSafariNative,
			model.CtxKeyPreferredEngine: "native",
		},
	}, profiles.HWAccelAuto, &scan.Capability{VideoCodec: "h264", Interlaced: false})
	if err != nil {
		t.Fatalf("resolveRequestedStartProfile() error = %#v", err)
	}
	if playbackMode != "native_hls" {
		t.Fatalf("resolveRequestedStartProfile() playbackMode = %q, want %q", playbackMode, "native_hls")
	}
	if profileID != profiles.ProfileSafari {
		t.Fatalf("resolveRequestedStartProfile() profileID = %q, want %q", profileID, profiles.ProfileSafari)
	}
}

// Copy-first: even on an AV1-capable iOS client, a progressive H.264 source the
// client can play natively must stay on the copy/remux path. Re-encoding a
// directly-playable source to AV1 only loses quality; AV1/HEVC are reserved for
// sources that genuinely cannot be copied (interlaced or unsupported codecs).
func TestResolveRequestedStartProfile_IOSNativeRuntimeAV1HEVCProgressiveH264SourcePrefersCopy(t *testing.T) {
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
	av1Efficient := true
	hevcSmooth := true
	hevcEfficient := true

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
			"playback_mode":             "native_hls",
			model.CtxKeyClientFamily:    playbackprofile.ClientIOSSafariNative,
			model.CtxKeyPreferredEngine: "native",
		},
		ClientCaps: &capabilities.PlaybackCapabilities{
			ClientFamilyFallback: playbackprofile.ClientIOSSafariNative,
			ClientCapsSource:     capabilities.ClientCapsSourceRuntimePlusFam,
			Containers:           []string{"mp4", "ts", "fmp4"},
			VideoCodecs:          []string{"av1", "hevc", "h264"},
			AudioCodecs:          []string{"aac", "ac3"},
			SupportsHLS:          true,
			RuntimeProbeUsed:     true,
			DeviceType:           "iphone",
			DeviceContext:        &capabilities.DeviceContext{OSName: "ios", OSVersion: "17.5", Model: "iPhone 15 Pro A17 Pro"},
			VideoCodecSignals: []capabilities.VideoCodecSignal{
				{Codec: "av1", Supported: true, Smooth: &av1Smooth, PowerEfficient: &av1Efficient},
				{Codec: "hevc", Supported: true, Smooth: &hevcSmooth, PowerEfficient: &hevcEfficient},
			},
		},
	}, profiles.HWAccelAuto, &scan.Capability{
		Container:  "ts",
		VideoCodec: "h264",
		AudioCodec: "ac3",
		Width:      1920,
		Height:     1080,
		FPS:        25,
		Interlaced: false,
	})
	if err != nil {
		t.Fatalf("resolveRequestedStartProfile() error = %#v", err)
	}
	if playbackMode != "native_hls" {
		t.Fatalf("resolveRequestedStartProfile() playbackMode = %q, want %q", playbackMode, "native_hls")
	}
	// Copy-first: progressive H.264 the client plays natively → copy/remux
	// (ProfileSafari), not an AV1 re-encode. AV1 only where copy is impossible.
	if profileID != profiles.ProfileSafari {
		t.Fatalf("resolveRequestedStartProfile() profileID = %q, want %q", profileID, profiles.ProfileSafari)
	}
}

func TestResolveRequestedStartProfile_HLSJSChromiumCompatibleH264SourceKeepsHighProfile(t *testing.T) {
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
			model.CtxKeyClientFamily:    playbackprofile.ClientChromiumHLSJS,
			model.CtxKeyPreferredEngine: "hlsjs",
		},
		ClientCaps: &capabilities.PlaybackCapabilities{
			ClientFamilyFallback: playbackprofile.ClientChromiumHLSJS,
			ClientCapsSource:     capabilities.ClientCapsSourceRuntime,
			Containers:           []string{"mp4", "ts", "fmp4"},
			VideoCodecs:          []string{"av1", "hevc", "h264"},
			AudioCodecs:          []string{"aac", "ac3"},
			SupportsHLS:          true,
			PreferredHLSEngine:   "hlsjs",
			RuntimeProbeUsed:     true,
			RuntimeProbeVersion:  2,
		},
	}, profiles.HWAccelAuto, &scan.Capability{
		Container:  "ts",
		VideoCodec: "h264",
		AudioCodec: "ac3",
		Width:      1920,
		Height:     1080,
		FPS:        25,
		Interlaced: false,
	})
	if err != nil {
		t.Fatalf("resolveRequestedStartProfile() error = %#v", err)
	}
	if playbackMode != "hlsjs" {
		t.Fatalf("resolveRequestedStartProfile() playbackMode = %q, want %q", playbackMode, "hlsjs")
	}
	if profileID != profiles.ProfileHigh {
		t.Fatalf("resolveRequestedStartProfile() profileID = %q, want %q", profileID, profiles.ProfileHigh)
	}
}

func TestResolveRequestedStartProfile_AutoModeCompatibleH264SourceKeepsUniversalProfile(t *testing.T) {
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
		Params: map[string]string{
			model.CtxKeyClientFamily:    playbackprofile.ClientChromiumHLSJS,
			model.CtxKeyPreferredEngine: "hlsjs",
		},
		ClientCaps: &capabilities.PlaybackCapabilities{
			ClientFamilyFallback: playbackprofile.ClientChromiumHLSJS,
			ClientCapsSource:     capabilities.ClientCapsSourceRuntime,
			Containers:           []string{"mp4", "ts", "fmp4"},
			VideoCodecs:          []string{"av1", "hevc", "h264"},
			AudioCodecs:          []string{"aac", "ac3"},
			SupportsHLS:          true,
			PreferredHLSEngine:   "hlsjs",
			RuntimeProbeUsed:     true,
			RuntimeProbeVersion:  2,
		},
	}, profiles.HWAccelAuto, &scan.Capability{
		Container:  "ts",
		VideoCodec: "h264",
		AudioCodec: "ac3",
		Width:      1920,
		Height:     1080,
		FPS:        25,
		Interlaced: false,
	})
	if err != nil {
		t.Fatalf("resolveRequestedStartProfile() error = %#v", err)
	}
	if playbackMode != "" {
		t.Fatalf("resolveRequestedStartProfile() playbackMode = %q, want empty playback mode", playbackMode)
	}
	if profileID != "universal" {
		t.Fatalf("resolveRequestedStartProfile() profileID = %q, want %q", profileID, "universal")
	}
}

func TestResolveRequestedStartProfile_AutoModeIOSRuntimeAV1H264SourceKeepsUniversalProfile(t *testing.T) {
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
	av1Efficient := true
	hevcSmooth := true
	hevcEfficient := true

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
		Params: map[string]string{
			model.CtxKeyClientFamily:    playbackprofile.ClientIOSSafariNative,
			model.CtxKeyPreferredEngine: "native",
		},
		ClientCaps: &capabilities.PlaybackCapabilities{
			ClientFamilyFallback: playbackprofile.ClientIOSSafariNative,
			ClientCapsSource:     capabilities.ClientCapsSourceRuntimePlusFam,
			Containers:           []string{"mp4", "ts", "fmp4"},
			VideoCodecs:          []string{"av1", "hevc", "h264"},
			AudioCodecs:          []string{"aac", "ac3"},
			SupportsHLS:          true,
			RuntimeProbeUsed:     true,
			DeviceType:           "iphone",
			DeviceContext:        &capabilities.DeviceContext{OSName: "ios", OSVersion: "17.5", Model: "iPhone 15 Pro A17 Pro"},
			VideoCodecSignals: []capabilities.VideoCodecSignal{
				{Codec: "av1", Supported: true, Smooth: &av1Smooth, PowerEfficient: &av1Efficient},
				{Codec: "hevc", Supported: true, Smooth: &hevcSmooth, PowerEfficient: &hevcEfficient},
			},
		},
	}, profiles.HWAccelAuto, &scan.Capability{
		Container:  "ts",
		VideoCodec: "h264",
		AudioCodec: "ac3",
		Width:      1920,
		Height:     1080,
		FPS:        25,
		Interlaced: false,
	})
	if err != nil {
		t.Fatalf("resolveRequestedStartProfile() error = %#v", err)
	}
	if playbackMode != "" {
		t.Fatalf("resolveRequestedStartProfile() playbackMode = %q, want empty playback mode", playbackMode)
	}
	if profileID != defaultStartProfileID {
		t.Fatalf("resolveRequestedStartProfile() profileID = %q, want %q", profileID, defaultStartProfileID)
	}
}

func TestResolveRequestedStartProfile_AutoModeIOSRuntimeAV1WithoutCopyPathPicksAV1Profile(t *testing.T) {
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
	av1Efficient := true
	hevcSmooth := true
	hevcEfficient := true

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
		Params: map[string]string{
			model.CtxKeyClientFamily:    playbackprofile.ClientIOSSafariNative,
			model.CtxKeyPreferredEngine: "native",
		},
		ClientCaps: &capabilities.PlaybackCapabilities{
			ClientFamilyFallback: playbackprofile.ClientIOSSafariNative,
			ClientCapsSource:     capabilities.ClientCapsSourceRuntimePlusFam,
			Containers:           []string{"mp4", "ts", "fmp4"},
			VideoCodecs:          []string{"av1", "hevc", "h264"},
			AudioCodecs:          []string{"aac", "ac3"},
			SupportsHLS:          true,
			RuntimeProbeUsed:     true,
			DeviceType:           "iphone",
			DeviceContext:        &capabilities.DeviceContext{OSName: "ios", OSVersion: "17.5", Model: "iPhone 15 Pro A17 Pro"},
			VideoCodecSignals: []capabilities.VideoCodecSignal{
				{Codec: "av1", Supported: true, Smooth: &av1Smooth, PowerEfficient: &av1Efficient},
				{Codec: "hevc", Supported: true, Smooth: &hevcSmooth, PowerEfficient: &hevcEfficient},
			},
		},
	}, profiles.HWAccelAuto, nil)
	if err != nil {
		t.Fatalf("resolveRequestedStartProfile() error = %#v", err)
	}
	if playbackMode != "" {
		t.Fatalf("resolveRequestedStartProfile() playbackMode = %q, want empty playback mode", playbackMode)
	}
	if profileID != profiles.ProfileAV1HW {
		t.Fatalf("resolveRequestedStartProfile() profileID = %q, want %q", profileID, profiles.ProfileAV1HW)
	}
}

func TestResolveRequestedStartProfile_AutoModeChromiumWithoutRuntimeCapsFallsBackToH264Profile(t *testing.T) {
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
		Params: map[string]string{
			model.CtxKeyClientFamily:    playbackprofile.ClientChromiumHLSJS,
			model.CtxKeyPreferredEngine: "hlsjs",
		},
	}, profiles.HWAccelAuto, nil)
	if err != nil {
		t.Fatalf("resolveRequestedStartProfile() error = %#v", err)
	}
	if playbackMode != "" {
		t.Fatalf("resolveRequestedStartProfile() playbackMode = %q, want empty playback mode", playbackMode)
	}
	if profileID != profiles.ProfileH264FMP4 {
		t.Fatalf("resolveRequestedStartProfile() profileID = %q, want %q", profileID, profiles.ProfileH264FMP4)
	}
}

func TestResolveRequestedStartProfile_AutoModeSafariWithoutRuntimeCapsPrefersHEVCProfile(t *testing.T) {
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
		Params: map[string]string{
			model.CtxKeyClientFamily:    playbackprofile.ClientSafariNative,
			model.CtxKeyPreferredEngine: "native",
		},
	}, profiles.HWAccelAuto, nil)
	if err != nil {
		t.Fatalf("resolveRequestedStartProfile() error = %#v", err)
	}
	if playbackMode != "" {
		t.Fatalf("resolveRequestedStartProfile() playbackMode = %q, want empty playback mode", playbackMode)
	}
	if profileID != profiles.ProfileSafariHEVCHW {
		t.Fatalf("resolveRequestedStartProfile() profileID = %q, want %q", profileID, profiles.ProfileSafariHEVCHW)
	}
}

func TestResolveRequestedStartProfile_NativeHLSSafariRuntimeH264OnlyUsesHEVCBaseline(t *testing.T) {
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
				{Codec: "hevc", Class: "strong"},
				{Codec: "h264", Class: "strong"},
			},
		},
	}
	svc := NewService(deps)

	profileID, playbackMode, err := svc.resolveRequestedStartProfile(context.Background(), Intent{
		PlaybackDecisionToken: "decision-token",
		Params: map[string]string{
			"playback_mode":             "native_hls",
			model.CtxKeyClientFamily:    playbackprofile.ClientSafariNative,
			model.CtxKeyPreferredEngine: "native",
		},
		ClientCaps: &capabilities.PlaybackCapabilities{
			ClientFamilyFallback: playbackprofile.ClientSafariNative,
			ClientCapsSource:     capabilities.ClientCapsSourceRuntime,
			Containers:           []string{"mp4", "ts"},
			VideoCodecs:          []string{"h264"},
			AudioCodecs:          []string{"aac", "ac3"},
			SupportsHLS:          true,
			PreferredHLSEngine:   "native",
			RuntimeProbeUsed:     true,
			RuntimeProbeVersion:  2,
		},
	}, profiles.HWAccelAuto, nil)
	if err != nil {
		t.Fatalf("resolveRequestedStartProfile() error = %#v", err)
	}
	if playbackMode != "native_hls" {
		t.Fatalf("resolveRequestedStartProfile() playbackMode = %q, want %q", playbackMode, "native_hls")
	}
	if profileID != profiles.ProfileSafariHEVCHW {
		t.Fatalf("resolveRequestedStartProfile() profileID = %q, want %q", profileID, profiles.ProfileSafariHEVCHW)
	}
}

func TestResolveRequestedStartProfile_DesktopSafariHlsjsWithoutRuntimeCapsFallsBackToH264Profile(t *testing.T) {
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
			model.CtxKeyClientFamily:    playbackprofile.ClientSafariNative,
			model.CtxKeyPreferredEngine: "hlsjs",
		},
	}, profiles.HWAccelAuto, nil)
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

func TestResolveRequestedStartProfile_AutoModeIOSRuntimeAV1WithoutCopyPathClampsToCompatibleProfileFromPlaybackFeedback(t *testing.T) {
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
	av1Efficient := true
	hevcSmooth := true
	hevcEfficient := true

	registry := capreg.NewMemoryStore()
	const (
		decisionTrace = "decision-ios-compatible"
		serviceRef    = "1:0:1:5000:1:70:1680000:0:0:0:"
	)
	seedStartPlaybackPolicyState(t, registry, decisionTrace, serviceRef, playbackprofile.RungCompatibleVideoH264CRF23)

	deps := newMockDeps()
	deps.registry = registry
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
		ServiceRef:    serviceRef,
		DecisionTrace: decisionTrace,
		Params: map[string]string{
			model.CtxKeyClientFamily:    playbackprofile.ClientIOSSafariNative,
			model.CtxKeyPreferredEngine: "native",
		},
		ClientCaps: &capabilities.PlaybackCapabilities{
			ClientFamilyFallback: playbackprofile.ClientIOSSafariNative,
			ClientCapsSource:     capabilities.ClientCapsSourceRuntimePlusFam,
			Containers:           []string{"mp4", "ts", "fmp4"},
			VideoCodecs:          []string{"av1", "hevc", "h264"},
			AudioCodecs:          []string{"aac", "ac3"},
			SupportsHLS:          true,
			VideoCodecSignals: []capabilities.VideoCodecSignal{
				{Codec: "av1", Supported: true, Smooth: &av1Smooth, PowerEfficient: &av1Efficient},
				{Codec: "hevc", Supported: true, Smooth: &hevcSmooth, PowerEfficient: &hevcEfficient},
			},
		},
	}, profiles.HWAccelAuto, nil)
	if err != nil {
		t.Fatalf("resolveRequestedStartProfile() error = %#v", err)
	}
	if playbackMode != "" {
		t.Fatalf("resolveRequestedStartProfile() playbackMode = %q, want empty playback mode", playbackMode)
	}
	if profileID != profiles.ProfileSafari {
		t.Fatalf("resolveRequestedStartProfile() profileID = %q, want %q", profileID, profiles.ProfileSafari)
	}
}

func TestResolveRequestedStartProfile_AutoModeIOSRuntimeAV1WithoutCopyPathClampsToRepairProfileFromPlaybackFeedback(t *testing.T) {
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
	av1Efficient := true
	hevcSmooth := true
	hevcEfficient := true

	registry := capreg.NewMemoryStore()
	const (
		decisionTrace = "decision-ios-repair"
		serviceRef    = "1:0:1:5000:1:70:1680001:0:0:0:"
	)
	seedStartPlaybackPolicyState(t, registry, decisionTrace, serviceRef, playbackprofile.RungRepairH264AAC)

	deps := newMockDeps()
	deps.registry = registry
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
		ServiceRef:    serviceRef,
		DecisionTrace: decisionTrace,
		Params: map[string]string{
			model.CtxKeyClientFamily:    playbackprofile.ClientIOSSafariNative,
			model.CtxKeyPreferredEngine: "native",
		},
		ClientCaps: &capabilities.PlaybackCapabilities{
			ClientFamilyFallback: playbackprofile.ClientIOSSafariNative,
			ClientCapsSource:     capabilities.ClientCapsSourceRuntimePlusFam,
			Containers:           []string{"mp4", "ts", "fmp4"},
			VideoCodecs:          []string{"av1", "hevc", "h264"},
			AudioCodecs:          []string{"aac", "ac3"},
			SupportsHLS:          true,
			VideoCodecSignals: []capabilities.VideoCodecSignal{
				{Codec: "av1", Supported: true, Smooth: &av1Smooth, PowerEfficient: &av1Efficient},
				{Codec: "hevc", Supported: true, Smooth: &hevcSmooth, PowerEfficient: &hevcEfficient},
			},
		},
	}, profiles.HWAccelAuto, nil)
	if err != nil {
		t.Fatalf("resolveRequestedStartProfile() error = %#v", err)
	}
	if playbackMode != "" {
		t.Fatalf("resolveRequestedStartProfile() playbackMode = %q, want empty playback mode", playbackMode)
	}
	if profileID != profiles.ProfileSafariDirty {
		t.Fatalf("resolveRequestedStartProfile() profileID = %q, want %q", profileID, profiles.ProfileSafariDirty)
	}
}

func seedStartPlaybackPolicyState(t *testing.T, registry *capreg.MemoryStore, decisionTrace, serviceRef string, maxQualityRung playbackprofile.QualityRung) {
	t.Helper()

	observation := capreg.PlaybackObservation{
		ObservedAt:        time.Now().UTC(),
		RequestID:         decisionTrace,
		ObservationKind:   "decision",
		Outcome:           "predicted",
		SourceRef:         serviceRef,
		SubjectKind:       "live",
		SourceFingerprint: "source-fp",
		DeviceFingerprint: "device-fp",
		HostFingerprint:   "host-fp",
	}
	if err := registry.RecordObservation(context.Background(), observation); err != nil {
		t.Fatalf("RecordObservation() error = %v", err)
	}
	if err := registry.RememberPlaybackPolicyState(context.Background(), capreg.PlaybackPolicyState{
		SubjectKind:       "live",
		SourceFingerprint: observation.SourceFingerprint,
		DeviceFingerprint: observation.DeviceFingerprint,
		HostFingerprint:   observation.HostFingerprint,
		MaxQualityRung:    maxQualityRung,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("RememberPlaybackPolicyState() error = %v", err)
	}
}

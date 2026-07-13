package playbackplanner

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlaybackEvidence_HashIsDeterministic(t *testing.T) {
	ev1 := PlaybackEvidence{
		EvaluatedAt: 1672531200000,
		SourceTruth: SourceTruth{
			Container:  "mpegts",
			VideoCodec: "h264",
			AudioCodec: "aac",
			Width:      1920,
			Height:     1080,
			FPS:        50,
			Interlaced: false,
		},
		ClientEvidence: ClientEvidence{
			Family:               "safari_native",
			AllowTranscode:       false,
			SupportedContainers:  []string{"mp4", "hls"},
			SupportedVideoCodecs: []string{"h264", "hevc"},
			SupportedAudioCodecs: []string{"aac"},
			MaxVideoWidth:        1920,
			MaxVideoHeight:       1080,
			MaxVideoFPS:          60,
		},
		NetworkEvidence: NetworkEvidence{
			DownlinkKbps:      5000,
			RTTMillis:         50,
			InternetValidated: true,
		},
		HostSnapshot: HostSnapshot{
			PressureBand:     "relaxed",
			AvailableEngines: []string{"hls"},
		},
		OperatorPolicy: OperatorPolicy{
			DisableTranscoding: false,
			MaxGlobalBitrate:   8000,
		},
	}

	ev2 := ev1 // Copy

	hash1, err := ev1.Hash()
	require.NoError(t, err)

	hash2, err := ev2.Hash()
	require.NoError(t, err)

	assert.Equal(t, hash1, hash2, "Identical evidence should produce identical hashes")

	// Change something
	ev3 := ev1
	ev3.EvaluatedAt = 1672531200001

	hash3, err := ev3.Hash()
	require.NoError(t, err)
	assert.NotEqual(t, hash1, hash3, "Different EvaluatedAt should produce different hashes")

	t.Run("Pure Hashing with Deduplication and Sorting", func(t *testing.T) {
		e1 := PlaybackEvidence{
			ClientEvidence: ClientEvidence{
				SupportedContainers: []string{"mp4", "hls", "mp4"},
			},
		}

		// The original slice should NOT be changed after hashing
		origContainerAddr := &e1.ClientEvidence.SupportedContainers[0]

		h1, err := e1.Hash()
		require.NoError(t, err)

		// Original shouldn't be sorted/deduplicated (len should still be 3)
		assert.Len(t, e1.ClientEvidence.SupportedContainers, 3)
		assert.Equal(t, origContainerAddr, &e1.ClientEvidence.SupportedContainers[0])

		e2 := PlaybackEvidence{
			ClientEvidence: ClientEvidence{
				SupportedContainers: []string{"hls", "mp4"},
			},
		}

		h2, err := e2.Hash()
		require.NoError(t, err)

		assert.Equal(t, h1, h2, "Duplicates and order should not change the hash")
	})

	t.Run("Auto codec preference order is semantic while host capacity order is not", func(t *testing.T) {
		base := PlaybackEvidence{
			ClientEvidence: ClientEvidence{
				AutoTranscodeVideoCodecs: []string{"av1", "hevc", "h264", "hevc"},
			},
			HostSnapshot: HostSnapshot{EncoderCapabilities: []HostEncoderCapability{
				{Codec: "hevc", Verified: true, AutoEligible: true, ProbeElapsedMS: 40},
				{Codec: "av1", Verified: true, AutoEligible: true, ProbeElapsedMS: 30},
			}},
		}
		baseHash, err := base.Hash()
		require.NoError(t, err)

		reorderedHost := base
		reorderedHost.HostSnapshot.EncoderCapabilities = []HostEncoderCapability{
			{Codec: "av1", Verified: true, AutoEligible: true, ProbeElapsedMS: 30},
			{Codec: "hevc", Verified: true, AutoEligible: true, ProbeElapsedMS: 40},
		}
		reorderedHostHash, err := reorderedHost.Hash()
		require.NoError(t, err)
		assert.Equal(t, baseHash, reorderedHostHash)

		reorderedPreference := base
		reorderedPreference.ClientEvidence.AutoTranscodeVideoCodecs = []string{"hevc", "av1", "h264"}
		reorderedPreferenceHash, err := reorderedPreference.Hash()
		require.NoError(t, err)
		assert.NotEqual(t, baseHash, reorderedPreferenceHash)
	})
}

func TestPlaybackPlan_HashIsDeterministic(t *testing.T) {
	plan1 := PlaybackPlan{
		Outcome:        "allow",
		Mode:           "transcode",
		DeliveryEngine: "hls",
		Video: TrackPlan{
			Mode:  "copy",
			Codec: "h264",
		},
		Audio: TrackPlan{
			Mode:  "copy",
			Codec: "aac",
		},
		Packaging: Packaging{
			Container: "fmp4",
		},
		RateControl: RateControl{
			TargetVideoBitrateKbps: 3000,
			MaxVideoBitrateKbps:    4000,
		},
		Filters: Filters{
			Deinterlace: true,
			ScaleWidth:  1280,
			ScaleHeight: 720,
		},
		ProbeReqs: ProbeReqs{
			RequireFullProbe: false,
		},
		Guardrails: Guardrails{
			PermittedAlternativePlans: []string{"audio_only"},
			MinQualityRung:            "low",
			MaxQualityRung:            "high",
			AllowProbeUp:              false,
			DecodeRisk:                "soft",
		},
	}

	plan2 := plan1 // Copy

	hash1, err := plan1.Hash()
	require.NoError(t, err)

	hash2, err := plan2.Hash()
	require.NoError(t, err)

	assert.Equal(t, hash1, hash2, "Identical plans should produce identical hashes")

	plan3 := plan1
	plan3.Mode = "direct_stream"

	hash3, err := plan3.Hash()
	require.NoError(t, err)

	assert.NotEqual(t, hash1, hash3, "Different plans should produce different hashes")

	plan4 := plan1
	plan4.Startup.DVRWindowSeconds = 16200
	hash4, err := plan4.Hash()
	require.NoError(t, err)
	assert.NotEqual(t, hash1, hash4, "Startup semantics must be bound by the plan hash")
}

func TestPlanningReceipt_Lifecycle(t *testing.T) {
	now := time.Now().UnixMilli()
	receipt := PlanningReceipt{
		EvidenceHash: "abc",
		PlanHash:     "def",
		IssuedAt:     now,
		ExpiresAt:    now + 60000,
	}

	assert.True(t, receipt.ExpiresAt > receipt.IssuedAt, "Receipt must expire after issuance")
}

func TestPlan_DeniesTranscodeWithoutHLSSupport(t *testing.T) {
	ev := PlaybackEvidence{
		EvaluatedAt:    time.Now().UnixMilli(),
		Scope:          "recording",
		SourceIdentity: "test-rec-1",
		SourceTruth: SourceTruth{
			Container:  "mpegts",
			VideoCodec: "h264",
			AudioCodec: "mp2", // audio incompatible with client -> requires transcode or deny
		},
		ClientEvidence: ClientEvidence{
			AllowTranscode:       true,
			SupportedContainers:  []string{"ts", "hls"},
			SupportedVideoCodecs: []string{"h264"},
			SupportedAudioCodecs: []string{"aac"},
			SupportsHls:          false, // Client lacks HLS engine/support
		},
		HostSnapshot: HostSnapshot{
			AvailableEngines: []string{"hls"},
		},
	}

	res, err := Plan(ev)
	require.NoError(t, err)
	assert.Equal(t, DecisionDeny, res.Plan.Decision)
	assert.Equal(t, "none", res.Plan.Mode)
	assert.Equal(t, ReasonHLSNotSupported, res.Plan.ReasonCode)
}

func TestPlan_RejectsUnknownMediaTruth(t *testing.T) {
	ev := PlaybackEvidence{
		EvaluatedAt:    time.Now().UnixMilli(),
		Scope:          "recording",
		SourceIdentity: "test-rec-unknown",
		SourceTruth: SourceTruth{
			Container:  "unknown",
			VideoCodec: "unknown",
		},
	}

	_, err := Plan(ev)
	require.ErrorIs(t, err, ErrInvalidEvidence)
}

func TestPlanCarriesImmutableDVRStartupPolicy(t *testing.T) {
	ev := PlaybackEvidence{
		EvaluatedAt:    time.Now().UnixMilli(),
		Scope:          "live",
		SourceIdentity: "service:1",
		SourceTruth: SourceTruth{
			Container:  "mpegts",
			VideoCodec: "h264",
			AudioCodec: "aac",
		},
		ClientEvidence: ClientEvidence{
			SupportedVideoCodecs: []string{"h264"},
			SupportedAudioCodecs: []string{"aac"},
			SupportsHls:          true,
		},
		HostSnapshot:   HostSnapshot{AvailableEngines: []string{"hls"}},
		OperatorPolicy: OperatorPolicy{DVRWindowSeconds: 16200},
	}

	result, err := Plan(ev)
	require.NoError(t, err)
	require.Equal(t, DecisionAllow, result.Plan.Decision)
	require.Equal(t, 16200, result.Plan.Startup.DVRWindowSeconds)

	ev.OperatorPolicy.DVRWindowSeconds = -1
	_, err = Plan(ev)
	require.ErrorIs(t, err, ErrInvalidEvidence)
}

func TestPlanTranscodesVideoWhenClientDimensionsAreExceeded(t *testing.T) {
	ev := PlaybackEvidence{
		EvaluatedAt:    time.Now().UnixMilli(),
		Scope:          "live",
		SourceIdentity: "service:4k",
		SourceTruth: SourceTruth{
			Container:  "mpegts",
			VideoCodec: "h264",
			AudioCodec: "aac",
			Width:      3840,
			Height:     2160,
			FPS:        50,
		},
		ClientEvidence: ClientEvidence{
			AllowTranscode:       true,
			SupportedVideoCodecs: []string{"h264"},
			SupportedAudioCodecs: []string{"aac"},
			MaxVideoWidth:        1280,
			MaxVideoHeight:       720,
			MaxVideoFPS:          60,
			SupportsHls:          true,
		},
		HostSnapshot: HostSnapshot{AvailableEngines: []string{"hls"}},
	}

	result, err := Plan(ev)
	require.NoError(t, err)
	require.Equal(t, PlannerVersion, result.Trace.PlannerVersion)
	require.Equal(t, "transcode", result.Plan.Mode)
	require.Equal(t, "transcode", result.Plan.Video.Mode)
	require.Equal(t, "h264", result.Plan.Video.Codec)
	require.Equal(t, 1280, result.Plan.Filters.ScaleWidth)
}

func TestPlanRejectsDuplicateHostEncoderCapabilitiesBeforeHashResolution(t *testing.T) {
	base := autoTranscodeEvidence("h264")
	base.HostSnapshot.EncoderCapabilities = []HostEncoderCapability{
		{Codec: "H264", Verified: true, AutoEligible: true, ProbeElapsedMS: 10},
		{Codec: " h264 ", Verified: false, AutoEligible: false},
	}
	reversed := base
	reversed.HostSnapshot.EncoderCapabilities = []HostEncoderCapability{
		base.HostSnapshot.EncoderCapabilities[1],
		base.HostSnapshot.EncoderCapabilities[0],
	}

	baseHash, err := base.Hash()
	require.NoError(t, err)
	reversedHash, err := reversed.Hash()
	require.NoError(t, err)
	require.Equal(t, baseHash, reversedHash, "capability order is deliberately non-semantic")

	_, err = Plan(base)
	require.ErrorIs(t, err, ErrInvalidEvidence)
	_, err = Plan(reversed)
	require.ErrorIs(t, err, ErrInvalidEvidence)
}

func TestPlanAutoCodecRateControlMatchesExecutionProfiles(t *testing.T) {
	tests := []struct {
		name       string
		codec      string
		hostCodec  *HostEncoderCapability
		downlink   int
		wantTarget int
		wantMax    int
	}{
		{name: "av1", codec: "av1", hostCodec: &HostEncoderCapability{Codec: "av1", Verified: true, AutoEligible: true, ProbeElapsedMS: 30}, wantMax: 6000},
		{name: "hevc", codec: "hevc", hostCodec: &HostEncoderCapability{Codec: "hevc", Verified: true, AutoEligible: true, ProbeElapsedMS: 40}, wantMax: 5000},
		{name: "h264 cpu", codec: "h264", wantMax: 8000},
		{name: "h264 hardware", codec: "h264", hostCodec: &HostEncoderCapability{Codec: "h264", Verified: true, AutoEligible: true, ProbeElapsedMS: 10}, wantMax: 20000},
		{name: "constrained h264 hardware", codec: "h264", hostCodec: &HostEncoderCapability{Codec: "h264", Verified: true, AutoEligible: true, ProbeElapsedMS: 10}, downlink: 4000, wantTarget: 3000, wantMax: 6000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := autoTranscodeEvidence(tt.codec)
			ev.NetworkEvidence.DownlinkKbps = tt.downlink
			if tt.hostCodec != nil {
				ev.HostSnapshot.EncoderCapabilities = []HostEncoderCapability{*tt.hostCodec}
			}

			result, err := Plan(ev)
			require.NoError(t, err)
			require.Equal(t, "transcode", result.Plan.Video.Mode)
			require.Equal(t, tt.codec, result.Plan.Video.Codec)
			require.Equal(t, tt.wantTarget, result.Plan.RateControl.TargetVideoBitrateKbps)
			require.Equal(t, tt.wantMax, result.Plan.RateControl.MaxVideoBitrateKbps)
		})
	}
}

func TestPlanNativeSafariKeepsLegacyHEVCCPUFallback(t *testing.T) {
	ev := autoTranscodeEvidence("hevc")
	ev.ClientEvidence.AutoTranscodeVideoCodecs = []string{"hevc", "h264"}
	ev.HostSnapshot = HostSnapshot{
		AvailableEngines: []string{"hls"},
		PerformanceClass: "high",
		BenchmarkClass:   "strong",
		EncoderCapabilities: []HostEncoderCapability{
			{Codec: "h264", Verified: true, AutoEligible: true, ProbeElapsedMS: 10, BenchmarkClass: "strong"},
		},
	}

	result, err := Plan(ev)
	require.NoError(t, err)
	require.Equal(t, "hevc", result.Plan.Video.Codec)
	require.Equal(t, 5000, result.Plan.RateControl.MaxVideoBitrateKbps)

	ev.ClientEvidence.Family = "chromium_hlsjs"
	nonNative, err := Plan(ev)
	require.NoError(t, err)
	require.Equal(t, "h264", nonNative.Plan.Video.Codec, "CPU HEVC fallback is native-WebKit-only")
}

func TestPlanExperimentalAV1PackagingIsHashBound(t *testing.T) {
	ev := autoTranscodeEvidence("av1")
	ev.HostSnapshot.EncoderCapabilities = []HostEncoderCapability{
		{Codec: "av1", Verified: true, AutoEligible: true, ProbeElapsedMS: 30},
	}

	stableHash, err := ev.Hash()
	require.NoError(t, err)
	stable, err := Plan(ev)
	require.NoError(t, err)
	require.Equal(t, "fmp4", stable.Plan.Packaging.Container)

	ev.OperatorPolicy.ExperimentalAV1MPEGTS = true
	experimentalHash, err := ev.Hash()
	require.NoError(t, err)
	require.NotEqual(t, stableHash, experimentalHash)
	experimental, err := Plan(ev)
	require.NoError(t, err)
	require.Equal(t, "mpegts", experimental.Plan.Packaging.Container)
}

func autoTranscodeEvidence(codec string) PlaybackEvidence {
	return PlaybackEvidence{
		EvaluatedAt:     1,
		Scope:           "live",
		RequestedIntent: "quality",
		SourceIdentity:  "service:auto-" + codec,
		SourceTruth: SourceTruth{
			Container:  "mpegts",
			VideoCodec: "h264",
			AudioCodec: "ac3",
			Width:      1920,
			Height:     1080,
			FPS:        25,
			Interlaced: true,
		},
		ClientEvidence: ClientEvidence{
			Family:                   "safari_native",
			AllowTranscode:           true,
			SupportedContainers:      []string{"mpegts", "fmp4"},
			SupportedVideoCodecs:     []string{codec, "h264"},
			SupportedAudioCodecs:     []string{"aac", "ac3"},
			AutoTranscodeVideoCodecs: []string{codec},
			SupportsHls:              true,
		},
		HostSnapshot: HostSnapshot{AvailableEngines: []string{"hls"}},
	}
}

func TestPlanInterlacedAutoProfileUsesImmutableHostCodecCapacity(t *testing.T) {
	ev := PlaybackEvidence{
		EvaluatedAt:    time.Now().UnixMilli(),
		Scope:          "live",
		SourceIdentity: "service:interlaced-auto",
		SourceTruth: SourceTruth{
			Container:  "mpegts",
			VideoCodec: "h264",
			AudioCodec: "ac3",
			Width:      1920,
			Height:     1080,
			FPS:        25,
			Interlaced: true,
		},
		ClientEvidence: ClientEvidence{
			Family:                   "safari_native",
			AllowTranscode:           true,
			SupportedContainers:      []string{"mp4", "mpegts", "fmp4"},
			SupportedVideoCodecs:     []string{"av1", "hevc", "h264"},
			SupportedAudioCodecs:     []string{"aac", "ac3"},
			AutoTranscodeVideoCodecs: []string{"av1", "hevc", "h264"},
			SupportsHls:              true,
		},
		HostSnapshot: HostSnapshot{
			AvailableEngines: []string{"hls"},
			PerformanceClass: "high",
			EncoderCapabilities: []HostEncoderCapability{
				{Codec: "h264", Verified: true, AutoEligible: true, ProbeElapsedMS: 10, BenchmarkClass: "strong"},
				{Codec: "hevc", Verified: true, AutoEligible: true, ProbeElapsedMS: 40, BenchmarkClass: "strong"},
				{Codec: "av1", Verified: true, AutoEligible: true, ProbeElapsedMS: 30, BenchmarkClass: "strong"},
			},
		},
	}

	result, err := Plan(ev)
	require.NoError(t, err)
	require.Equal(t, "transcode", result.Plan.Mode)
	require.Equal(t, TrackPlan{Mode: "transcode", Codec: "av1"}, result.Plan.Video)
	require.Equal(t, "transcode", result.Plan.Audio.Mode)
	require.Equal(t, "aac", result.Plan.Audio.Codec)
	require.Equal(t, "fmp4", result.Plan.Packaging.Container)
	require.True(t, result.Plan.Filters.Deinterlace)
}

func TestPlanHonorsSignedRepairIntent(t *testing.T) {
	ev := PlaybackEvidence{
		EvaluatedAt:     time.Now().UnixMilli(),
		Scope:           "live",
		RequestedIntent: "repair",
		SourceIdentity:  "service:repair",
		SourceTruth: SourceTruth{
			Container:  "mpegts",
			VideoCodec: "h264",
			AudioCodec: "aac",
		},
		ClientEvidence: ClientEvidence{
			AllowTranscode:       true,
			SupportedVideoCodecs: []string{"h264"},
			SupportedAudioCodecs: []string{"aac"},
			SupportsHls:          true,
		},
		HostSnapshot: HostSnapshot{AvailableEngines: []string{"hls"}},
	}

	result, err := Plan(ev)
	require.NoError(t, err)
	require.Equal(t, "transcode", result.Plan.Mode)
	require.Equal(t, "copy", result.Plan.Video.Mode)
	require.Equal(t, "h264", result.Plan.Video.Codec)
	require.Equal(t, "transcode", result.Plan.Audio.Mode)
	require.Equal(t, "aac", result.Plan.Audio.Codec)
	require.Contains(t, result.Trace.Log, RuleHit{Rule: "direct_play_gate", Result: "fail", Reason: "transcode_intent_requested"})
}

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
	require.Equal(t, "transcode", result.Plan.Mode)
	require.Equal(t, "transcode", result.Plan.Video.Mode)
	require.Equal(t, "h264", result.Plan.Video.Codec)
	require.Equal(t, 1280, result.Plan.Filters.ScaleWidth)
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
	require.Equal(t, "transcode", result.Plan.Video.Mode)
	require.Equal(t, "copy", result.Plan.Audio.Mode)
	require.Contains(t, result.Trace.Log, RuleHit{Rule: "direct_play_gate", Result: "fail", Reason: "transcode_intent_requested"})
}

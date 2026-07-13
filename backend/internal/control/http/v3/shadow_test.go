package v3

import (
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/control/http/v3/intents/testfixtures"
	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/domain/playbackplanner"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	"github.com/stretchr/testify/assert"
)

func TestComparableFromLegacy(t *testing.T) {
	dec := &decision.Decision{
		Mode: decision.ModeTranscode,
		SelectedOutputKind: "hls",
		Selected: decision.SelectedFormats{
			Container:  "ts",
			VideoCodec: "h264",
			AudioCodec: "aac",
		},
		TargetProfile: &playbackprofile.TargetPlaybackProfile{
			Container: "ts",
			Video: playbackprofile.VideoTarget{
				Mode: "transcode",
				Codec: "h264",
				BitrateKbps: 4000,
				Width: 1280,
				Height: 720,
			},
			Audio: playbackprofile.AudioTarget{
				Mode: "copy",
				Codec: "aac",
			},
		},
	}
	
	comp := ComparableFromLegacy(dec)
	assert.True(t, comp.IsValid)
	assert.Equal(t, "allow", comp.Outcome)
	assert.Equal(t, "transcode", comp.Mode)
	assert.Equal(t, "hls", comp.Engine)
	assert.Equal(t, "ts", comp.Container)
	assert.Equal(t, "h264", comp.VideoCodec)
	assert.Equal(t, 4000, comp.TargetBitrate)
	assert.Equal(t, 1280, comp.ScaleWidth)
	assert.Equal(t, "transcode", comp.VideoMode)
	assert.Equal(t, "copy", comp.AudioMode)
}

func TestDiffComparablePlans(t *testing.T) {
	legacy := ComparablePlaybackPlan{
		IsValid: true,
		Outcome: "allow",
		Mode: "remux",
		Engine: "hls",
		Container: "ts",
		VideoMode: "copy",
		TargetBitrate: 5000,
	}
	
	newPlan := ComparablePlaybackPlan{
		IsValid: true,
		Outcome: "allow",
		Mode: "transcode", // mismatch
		Engine: "hls",
		Container: "ts",
		VideoMode: "transcode", // mismatch
		TargetBitrate: 4000, // mismatch
	}
	
	diffs := DiffComparablePlans(legacy, newPlan)
	assert.Contains(t, diffs, "mode_mismatch")
	assert.Contains(t, diffs, "video_mode_mismatch")
	assert.Contains(t, diffs, "target_bitrate_drift")
	assert.NotContains(t, diffs, "outcome_mismatch")
	assert.NotContains(t, diffs, "engine_mismatch")
}

func TestComparableFromPlanner(t *testing.T) {
	plan := playbackplanner.PlaybackPlan{
		Outcome: "allow",
		Mode: "transcode",
		DeliveryEngine: "hls",
		Packaging: playbackplanner.Packaging{
			Container: "ts",
		},
		Video: playbackplanner.TrackPlan{
			Mode: "transcode",
			Codec: "h264",
		},
		Audio: playbackplanner.TrackPlan{
			Mode: "copy",
			Codec: "copy",
		},
		RateControl: playbackplanner.RateControl{
			TargetVideoBitrateKbps: 3000,
		},
	}
	
	comp := ComparableFromPlanner(plan)
	assert.True(t, comp.IsValid)
	assert.Equal(t, "allow", comp.Outcome)
	assert.Equal(t, "transcode", comp.Mode)
	assert.Equal(t, "ts", comp.Container)
	assert.Equal(t, "h264", comp.VideoCodec)
	assert.Equal(t, 3000, comp.TargetBitrate)
}

func buildLegacyPlanningInput(tc testfixtures.CharacterizationTest) LegacyPlanningInput {
	confidence := "high"
	if tc.TruthConfidence > 0 && tc.TruthConfidence < 0.3 {
		confidence = "stale"
	} else if tc.TruthConfidence > 0 && tc.TruthConfidence < 0.8 {
		confidence = "partial"
	} else if tc.SourceCap.State == scan.CapabilityStatePartial || (tc.SourceCap.Width == 0 && tc.SourceCap.VideoCodec != "") {
		confidence = "partial"
	}

	supportedVideoCodecs := []string{"h264"}
	if tc.ClientFam == playbackprofile.ClientSafariNative || tc.ClientFam == playbackprofile.ClientIOSSafariNative {
		supportedVideoCodecs = []string{"hevc", "h264"}
	}

	return LegacyPlanningInput{
		EvaluatedAt:        time.Now().UnixMilli(),
		Scope:              "live",
		RequestedIntent:    "stream",
		SourceIdentity:     "ch-1",
		Provenance:         "scan",
		Confidence:         confidence,
		ObservedAt:         time.Now().UnixMilli(),
		NetworkCaptureTime: time.Now().UnixMilli(),
		PolicyVersion:      "v1",

		Container:         tc.SourceCap.Container,
		VideoCodec:        tc.SourceCap.VideoCodec,
		AudioCodec:        tc.SourceCap.AudioCodec,
		Width:             tc.SourceCap.Width,
		Height:            tc.SourceCap.Height,
		FPS:               int(tc.SourceCap.FPS),
		Interlaced:        tc.SourceCap.Interlaced,
		BitrateKbps:       tc.SourceCap.BitrateKbps,
		BitrateConfidence: "high",

		ClientFamily:         tc.ClientFam,
		DeviceType:           "unknown",
		CapabilityVersion:    "1.0",
		AllowTranscode:       tc.Params["allow_transcode"] != "0",
		SupportsHls:          true,
		SupportedContainers:  []string{"mpegts", "fmp4"},
		SupportedVideoCodecs: supportedVideoCodecs,
		SupportedAudioCodecs: []string{"aac", "ac3"},
		DownlinkKbps:         tc.NetworkKbps,
		RTTMillis:            tc.NetworkRTT,

		HostPressureBand: string(tc.HostPressure),
		AvailableEngines: []string{"hls"},
		PerformanceClass: "standard",
		BenchmarkClass:   "standard",

		ForceIntent:        "",
		MaxQualityRung:     "",
		DisableTranscoding: false,
		MaxGlobalBitrate:   0,
		StrictFreshness:    true,
	}
}

func buildExpectedLegacyPlan(tc testfixtures.CharacterizationTest) ComparablePlaybackPlan {
	mode := "remux"
	if tc.WantProfile != "high" && tc.WantProfile != "" {
		mode = "transcode"
	} else if tc.WantOutcome == "deny" {
		mode = "none"
	}

	container := tc.WantContainer
	if container == "" {
		container = "mpegts"
	}

	comp := ComparablePlaybackPlan{
		IsValid:        true,
		Outcome:        "allow",
		Mode:           mode,
		Engine:         "hls",
		Container:      container,
		VideoCodec:     tc.WantVideoCodec,
		MinQualityRung: tc.WantVideoRung,
		MaxQualityRung: "",
	}
	
	if mode == "remux" {
		comp.VideoMode = "copy"
		comp.AudioMode = "copy"
		if comp.VideoCodec == "" {
			comp.VideoCodec = tc.SourceCap.VideoCodec
		}
		comp.AudioCodec = tc.SourceCap.AudioCodec
	} else if mode != "none" {
		comp.VideoMode = "transcode"
		comp.AudioMode = "transcode"
		comp.AudioCodec = "aac"
	}
	
	if tc.WantOutcome != "" {
		comp.Outcome = tc.WantOutcome
	}
	if comp.Outcome == "deny" {
		comp.Mode = "none"
		comp.Engine = ""
		comp.Container = ""
		comp.VideoCodec = ""
		comp.AudioCodec = ""
		comp.VideoMode = ""
		comp.AudioMode = ""
	}
	
	return comp
}

func TestEquivalenceGate(t *testing.T) {
	for _, tc := range testfixtures.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			input := buildLegacyPlanningInput(tc)
			ev, err := BuildPlaybackEvidence(input)
			assert.NoError(t, err)

			res, err := playbackplanner.Plan(ev)
			assert.NoError(t, err)

			newComp := ComparableFromPlanner(res.Plan)
			legacyComp := buildExpectedLegacyPlan(tc)

			diffs := DiffComparablePlans(legacyComp, newComp)
			if len(diffs) > 0 {
				t.Logf("Legacy: %+v", legacyComp)
				t.Logf("Domain: %+v", newComp)
			}
			assert.Empty(t, diffs, "Expected 0 diffs, got: %v", diffs)
		})
	}
}

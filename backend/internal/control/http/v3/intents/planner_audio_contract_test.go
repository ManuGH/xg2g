package intents

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/domain/playbackplanner"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
)

// TestPlannerProducedTranscodePlanCarriesSharedAudioBitrate closes the seam that
// let two deploys ship without effect on 2026-07-26: the profile axes were raised
// to 320k while planner-bound sessions kept getting 192k, because
// applyPlannerPlanToProfile overwrites ProfileSpec.AudioBitrateK from the plan and
// the planner carried its own literal.
//
// The existing planner-profile tests feed a hand-written plan, so they verify
// pass-through but cannot catch a stale default inside the planner. This one plans
// from evidence — like /live/stream-info does — and follows the value all the way
// into the executed ProfileSpec.
func TestPlannerProducedTranscodePlanCarriesSharedAudioBitrate(t *testing.T) {
	// Mirrors the staging reality: interlaced DVB H.264 + AC-3 from the relay, a
	// Safari client that allows transcoding and prefers fMP4. Interlaced source
	// forces the transcode branch instead of copy/remux.
	evidence := playbackplanner.PlaybackEvidence{
		EvaluatedAt:    1,
		Scope:          "live",
		SourceIdentity: "1:0:19:83:6:85:C00000:0:0:0",
		PolicyVersion:  "policy-v1",
		SourceTruth: playbackplanner.SourceTruth{
			Container:  "mpegts",
			VideoCodec: "h264",
			AudioCodec: "ac3",
			Width:      1920,
			Height:     1080,
			FPS:        25,
			Interlaced: true,
		},
		ClientEvidence: playbackplanner.ClientEvidence{
			Family:               "safari_native",
			DeviceType:           "web",
			AllowTranscode:       true,
			SupportsHls:          true,
			PrefersFMP4:          true,
			PreferredEngine:      "hlsjs",
			SupportedContainers:  []string{"fmp4", "mp4", "ts"},
			SupportedVideoCodecs: []string{"av1", "hevc", "h264"},
			SupportedAudioCodecs: []string{"aac", "mp3", "ac3"},
		},
	}

	result, err := playbackplanner.Plan(evidence)
	require.NoError(t, err)
	require.Equal(t, "transcode", result.Plan.Mode, "evidence must exercise the transcode branch")
	require.Equal(t, "transcode", result.Plan.Audio.Mode)
	require.Equal(t, playbackprofile.LiveTranscodeAudioBitrateKbps, result.Plan.Audio.BitrateKbps,
		"planner audio target must use the shared constant, not its own literal")

	evidenceHash, err := evidence.Hash()
	require.NoError(t, err)
	planHash, err := result.Plan.Hash()
	require.NoError(t, err)
	receipt := playbackplanner.PlanningReceipt{
		ReceiptID:      "receipt-audio-contract",
		EvidenceHash:   evidenceHash,
		PlanHash:       planHash,
		PlannerVersion: playbackplanner.PlannerVersion,
		PolicyVersion:  evidence.PolicyVersion,
	}

	service := NewService(newMockDeps())
	resolution, intentErr := service.resolvePlannerStartProfile(Intent{
		ServiceRef:      evidence.SourceIdentity,
		PlannerPlan:     &result.Plan,
		PlanningReceipt: &receipt,
		PlannerEvidence: &evidence,
	}, startHardwareState{})
	require.Nil(t, intentErr)
	require.True(t, resolution.profileSpec.PlannerBound)
	require.Equal(t, "transcode", resolution.profileSpec.AudioMode)
	require.Equal(t, "aac", resolution.profileSpec.AudioCodec)
	require.Equal(t, playbackprofile.LiveTranscodeAudioBitrateKbps, resolution.profileSpec.AudioBitrateK,
		"the executed profile is what reaches ffmpeg -b:a")
}

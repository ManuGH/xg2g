package intents

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/domain/playbackplanner"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

func TestResolvePlannerStartProfileConsumesExactTrackAndPackagingPlan(t *testing.T) {
	service := NewService(newMockDeps())
	evidence, plan, receipt := plannerStartFixture(t)

	resolution, intentErr := service.resolvePlannerStartProfile(Intent{
		ServiceRef:      evidence.SourceIdentity,
		Params:          map[string]string{"profile": "repair", "hwaccel": "force", "codecs": "av1"},
		PlannerPlan:     &plan,
		PlanningReceipt: &receipt,
		PlannerEvidence: &evidence,
	}, startHardwareState{})
	require.Nil(t, intentErr)
	require.False(t, resolution.profileSpec.TranscodeVideo)
	require.True(t, resolution.profileSpec.PlannerBound)
	require.Equal(t, "transcode", resolution.profileSpec.AudioMode)
	require.Equal(t, "aac", resolution.profileSpec.AudioCodec)
	require.Equal(t, 160, resolution.profileSpec.AudioBitrateK)
	require.Equal(t, "fmp4", resolution.profileSpec.Container)
	require.Equal(t, 16200, resolution.profileSpec.DVRWindowSec)
	require.Empty(t, resolution.profileSpec.HWAccel)
	require.NotEmpty(t, resolution.idempotencyKey)
}

func TestResolvePlannerStartProfileRejectsTamperedPlan(t *testing.T) {
	service := NewService(newMockDeps())
	evidence, plan, receipt := plannerStartFixture(t)
	plan.Audio.BitrateKbps++

	_, intentErr := service.resolvePlannerStartProfile(Intent{
		ServiceRef:      evidence.SourceIdentity,
		PlannerPlan:     &plan,
		PlanningReceipt: &receipt,
		PlannerEvidence: &evidence,
	}, startHardwareState{})
	require.NotNil(t, intentErr)
	require.Contains(t, intentErr.Message, "plan hash")
}

func TestResolvePlannerStartProfileRejectsUnexecutableSignedPlan(t *testing.T) {
	service := NewService(newMockDeps())
	evidence, plan, receipt := plannerStartFixture(t)
	plan.Audio.Channels = 6
	planHash, err := plan.Hash()
	require.NoError(t, err)
	receipt.PlanHash = planHash

	_, intentErr := service.resolvePlannerStartProfile(Intent{
		ServiceRef:      evidence.SourceIdentity,
		PlannerPlan:     &plan,
		PlanningReceipt: &receipt,
		PlannerEvidence: &evidence,
	}, startHardwareState{})
	require.NotNil(t, intentErr)
	require.Contains(t, intentErr.Message, "audio channels")
}

func TestResolvePlannerStartProfileKeepsTargetAndMaximumBitrateDistinct(t *testing.T) {
	service := NewService(newMockDeps())
	evidence, plan, receipt := plannerStartFixture(t)
	plan.Mode = "transcode"
	plan.Video = playbackplanner.TrackPlan{Mode: "transcode", Codec: "h264"}
	plan.RateControl = playbackplanner.RateControl{
		TargetVideoBitrateKbps: 3000,
		MaxVideoBitrateKbps:    6000,
	}
	planHash, err := plan.Hash()
	require.NoError(t, err)
	receipt.PlanHash = planHash

	resolution, intentErr := service.resolvePlannerStartProfile(Intent{
		ServiceRef:      evidence.SourceIdentity,
		PlannerPlan:     &plan,
		PlanningReceipt: &receipt,
		PlannerEvidence: &evidence,
	}, startHardwareState{})
	require.Nil(t, intentErr)
	require.True(t, resolution.profileSpec.PlannerBound)
	require.True(t, resolution.profileSpec.TranscodeVideo)
	require.Equal(t, 3000, resolution.profileSpec.VideoTargetRateK)
	require.Equal(t, 6000, resolution.profileSpec.VideoMaxRateK)
	require.Equal(t, 12000, resolution.profileSpec.VideoBufSizeK)
	require.Zero(t, resolution.profileSpec.VideoCRF)
	require.Zero(t, resolution.profileSpec.VideoQP)
}

func TestProcessIntentPersistsReceiptPlanWithoutLegacyReplanning(t *testing.T) {
	deps := newMockDeps()
	service := NewService(deps)
	evidence, plan, receipt := plannerStartFixture(t)

	result, intentErr := service.ProcessIntent(context.Background(), Intent{
		Type:            model.IntentTypeStreamStart,
		SessionID:       "session-1",
		ServiceRef:      evidence.SourceIdentity,
		Mode:            model.ModeLive,
		Params:          map[string]string{"profile": "repair", "hwaccel": "force", "codecs": "av1"},
		PlannerPlan:     &plan,
		PlanningReceipt: &receipt,
		PlannerEvidence: &evidence,
		Logger:          zerolog.Nop(),
	})
	require.Nil(t, intentErr)
	require.Equal(t, "accepted", result.Status)
	require.NotNil(t, deps.store.putSession)
	require.False(t, deps.store.putSession.Profile.TranscodeVideo)
	require.True(t, deps.store.putSession.Profile.PlannerBound)
	require.Equal(t, "transcode", deps.store.putSession.Profile.AudioMode)
	require.Equal(t, "aac", deps.store.putSession.Profile.AudioCodec)
	require.Equal(t, "fmp4", deps.store.putSession.Profile.Container)
	require.Equal(t, receipt.ReceiptID, deps.store.putSession.ContextData["plannerReceiptId"])
	require.Equal(t, receipt.PlanHash, deps.store.putSession.ContextData["plannerPlanHash"])
}

func TestBuildStartSessionDoesNotReplanReceiptProfileThroughStartupCap(t *testing.T) {
	service := NewService(newMockDeps())
	receipt := playbackplanner.PlanningReceipt{ReceiptID: "receipt-1"}
	profile := model.ProfileSpec{
		Name:           "planner-h264",
		TranscodeVideo: true,
		VideoCodec:     "libx264",
		AudioMode:      "transcode",
		AudioCodec:     "aac",
		Deinterlace:    true,
		VideoCRF:       20,
		VideoMaxWidth:  1920,
		VideoMaxRateK:  8000,
		VideoBufSizeK:  16000,
		AudioBitrateK:  192,
		Preset:         "slow",
		Container:      "fmp4",
	}

	session := service.buildStartSession(Intent{
		SessionID:       "session-1",
		ServiceRef:      "service:1",
		Mode:            model.ModeLive,
		PlanningReceipt: &receipt,
		Params:          map[string]string{},
	}, startProfileResolution{profileSpec: profile})

	require.Equal(t, profile, session.Profile)
}

func plannerStartFixture(t *testing.T) (playbackplanner.PlaybackEvidence, playbackplanner.PlaybackPlan, playbackplanner.PlanningReceipt) {
	t.Helper()
	evidence := playbackplanner.PlaybackEvidence{
		EvaluatedAt:    1,
		Scope:          "live",
		SourceIdentity: "1:0:1:100:200:300:0:0:0:0:",
		PolicyVersion:  "policy-v1",
		SourceTruth: playbackplanner.SourceTruth{
			Container:  "mpegts",
			VideoCodec: "h264",
			AudioCodec: "ac3",
			Width:      1920,
			Height:     1080,
			FPS:        25,
		},
		ClientEvidence: playbackplanner.ClientEvidence{PreferredEngine: "native"},
	}
	plan := playbackplanner.PlaybackPlan{
		Decision:       playbackplanner.DecisionAllow,
		Outcome:        playbackplanner.DecisionAllow,
		Mode:           "remux",
		DeliveryEngine: "hls",
		Video:          playbackplanner.TrackPlan{Mode: "copy", Codec: "h264"},
		Audio:          playbackplanner.TrackPlan{Mode: "transcode", Codec: "aac", BitrateKbps: 160, Channels: 2, SampleRate: 48000},
		Packaging:      playbackplanner.Packaging{Container: "fmp4"},
		Startup:        playbackplanner.StartupPlan{DVRWindowSeconds: 16200},
	}
	evidenceHash, err := evidence.Hash()
	require.NoError(t, err)
	planHash, err := plan.Hash()
	require.NoError(t, err)
	receipt := playbackplanner.PlanningReceipt{
		ReceiptID:      "receipt-1",
		EvidenceHash:   evidenceHash,
		PlanHash:       planHash,
		PlannerVersion: "v4",
		PolicyVersion:  "policy-v1",
	}
	return evidence, plan, receipt
}

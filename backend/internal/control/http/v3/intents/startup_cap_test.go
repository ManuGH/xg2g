package intents

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/control/recordings/runtimepolicy"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestBuildStartSession_CapsHeavyLiveStartupProfileButPreservesTargetStep(t *testing.T) {
	deps := newMockDeps()
	svc := NewService(deps)

	resolution := startProfileResolution{
		effectiveProfileID: profiles.ProfileSafari,
		profileSpec: model.ProfileSpec{
			Name:                 profiles.ProfileSafari,
			PolicyModeHint:       ports.RuntimeModeHQ25,
			EffectiveRuntimeMode: ports.RuntimeModeHQ25,
			EffectiveModeSource:  ports.RuntimeModeSourceResolve,
			TranscodeVideo:       true,
			Deinterlace:          true,
			VideoCodec:           "libx264",
			VideoCRF:             20,
			VideoMaxWidth:        1920,
			VideoMaxRateK:        8000,
			VideoBufSizeK:        16000,
			AudioBitrateK:        192,
			Preset:               "slow",
			Container:            "mpegts",
		},
	}

	session := svc.buildStartSession(Intent{
		Type:          model.IntentTypeStreamStart,
		SessionID:     "sid-startup-cap",
		ServiceRef:    "1:0:19:EF75:3F9:1:C00000:0:0:0",
		CorrelationID: "corr-startup-cap",
		Mode:          model.ModeLive,
		Logger:        zerolog.Nop(),
	}, resolution)

	require.Equal(t, profiles.ProfileSafari, session.Profile.Name)
	require.Equal(t, ports.RuntimeModeSourceRuntimeHardening, session.Profile.EffectiveModeSource)
	require.Equal(t, "libx264", session.Profile.VideoCodec)
	require.Equal(t, 26, session.Profile.VideoCRF)
	require.Equal(t, 1280, session.Profile.VideoMaxWidth)
	require.Equal(t, 6000, session.Profile.VideoMaxRateK)
	require.Equal(t, 12000, session.Profile.VideoBufSizeK)
	require.Equal(t, 160, session.Profile.AudioBitrateK)
	require.Equal(t, "veryfast", session.Profile.Preset)
	require.True(t, session.Profile.Deinterlace)
	require.Equal(t, "mpegts", session.Profile.Container)
	require.Equal(t, string(runtimepolicy.PlaybackStepH2641080p), session.ContextData[model.CtxKeyRuntimeTargetStep])
}

func TestBuildStartSession_CapsAV1StartupButPreservesAV1TargetStep(t *testing.T) {
	deps := newMockDeps()
	svc := NewService(deps)

	resolution := startProfileResolution{
		effectiveProfileID: profiles.ProfileAV1HW,
		profileSpec: model.ProfileSpec{
			Name:                 profiles.ProfileAV1HW,
			PolicyModeHint:       ports.RuntimeModeHQ25,
			EffectiveRuntimeMode: ports.RuntimeModeHQ25,
			EffectiveModeSource:  ports.RuntimeModeSourceResolve,
			TranscodeVideo:       true,
			Deinterlace:          true,
			VideoCodec:           "av1",
			HWAccel:              "vaapi",
			VideoMaxWidth:        1920,
			AudioBitrateK:        192,
			Container:            "fmp4",
		},
	}

	session := svc.buildStartSession(Intent{
		Type:          model.IntentTypeStreamStart,
		SessionID:     "sid-startup-cap-av1",
		ServiceRef:    "1:0:19:EF75:3F9:1:C00000:0:0:0",
		CorrelationID: "corr-startup-cap-av1",
		Mode:          model.ModeLive,
		Logger:        zerolog.Nop(),
	}, resolution)

	require.Equal(t, profiles.ProfileAV1HW, session.Profile.Name)
	require.Equal(t, "libx264", session.Profile.VideoCodec)
	require.Equal(t, "", session.Profile.HWAccel)
	require.Equal(t, 1280, session.Profile.VideoMaxWidth)
	require.Equal(t, "fmp4", session.Profile.Container)
	require.Equal(t, string(runtimepolicy.PlaybackStepAV11080p), session.ContextData[model.CtxKeyRuntimeTargetStep])
}

func TestCapLiveStartupProfile_SkipsGPUBackedOrAlreadyCheapProfiles(t *testing.T) {
	intent := Intent{Mode: model.ModeLive}

	capped, changed := capLiveStartupProfile(intent, model.ProfileSpec{
		Name:                 profiles.ProfileSafari,
		PolicyModeHint:       ports.RuntimeModeHQ25,
		EffectiveRuntimeMode: ports.RuntimeModeHQ25,
		EffectiveModeSource:  ports.RuntimeModeSourceResolve,
		TranscodeVideo:       true,
		Deinterlace:          true,
		VideoCodec:           "libx264",
		HWAccel:              "vaapi",
		VideoCRF:             20,
		VideoMaxWidth:        1920,
	}, runtimepolicy.PlaybackStepH2641080p)
	require.False(t, changed)
	require.Equal(t, "vaapi", capped.HWAccel)

	capped, changed = capLiveStartupProfile(intent, model.ProfileSpec{
		Name:                 profiles.ProfileSafari,
		PolicyModeHint:       ports.RuntimeModeHQ25,
		EffectiveRuntimeMode: ports.RuntimeModeHQ25,
		EffectiveModeSource:  ports.RuntimeModeSourceResolve,
		TranscodeVideo:       true,
		Deinterlace:          true,
		VideoCodec:           "libx264",
		VideoCRF:             26,
		VideoMaxWidth:        1280,
	}, runtimepolicy.PlaybackStepH264720p)
	require.False(t, changed)
	require.Equal(t, 1280, capped.VideoMaxWidth)
}

func TestCapLiveStartupProfile_CapsAV1LiveTargetToCheapH264Startup(t *testing.T) {
	intent := Intent{Mode: model.ModeLive}

	capped, changed := capLiveStartupProfile(intent, model.ProfileSpec{
		Name:                 profiles.ProfileAV1HW,
		PolicyModeHint:       ports.RuntimeModeHQ25,
		EffectiveRuntimeMode: ports.RuntimeModeHQ25,
		EffectiveModeSource:  ports.RuntimeModeSourceResolve,
		TranscodeVideo:       true,
		Deinterlace:          true,
		VideoCodec:           "av1",
		HWAccel:              "vaapi",
		VideoMaxWidth:        1920,
		Container:            "fmp4",
		AudioBitrateK:        192,
	}, runtimepolicy.PlaybackStepAV11080p)

	require.True(t, changed)
	require.Equal(t, "libx264", capped.VideoCodec)
	require.Equal(t, "", capped.HWAccel)
	require.Equal(t, 1280, capped.VideoMaxWidth)
	require.Equal(t, "fmp4", capped.Container)
	require.Equal(t, 160, capped.AudioBitrateK)
	require.Equal(t, ports.RuntimeModeSourceRuntimeHardening, capped.EffectiveModeSource)
}

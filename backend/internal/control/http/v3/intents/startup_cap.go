package intents

import (
	"strings"

	"github.com/ManuGH/xg2g/internal/control/recordings/runtimepolicy"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
)

const (
	startupCapVideoCRF      = 26
	startupCapVideoMaxWidth = 1280
	startupCapVideoMaxRateK = 6000
	startupCapVideoBufSizeK = 12000
	startupCapAudioBitrateK = 160
	startupCapPreset        = "veryfast"
)

// capLiveStartupProfile keeps the first live transcode start on a cheaper rung
// when we know we're about to launch a CPU-only 1080i repair path. The normal
// runtime policy still keeps the original target step and may step back up
// after the startup warmup settles.
func capLiveStartupProfile(intent Intent, profile model.ProfileSpec, targetStep runtimepolicy.PlaybackLadderStep) (model.ProfileSpec, bool) {
	if !shouldCapLiveStartupProfile(intent, profile, targetStep) {
		return profile, false
	}

	next := profile
	next.EffectiveModeSource = ports.RuntimeModeSourceRuntimeHardening
	next.TranscodeVideo = true
	next.Deinterlace = true
	next.VideoCodec = "libx264"
	next.HWAccel = ""
	next.VideoCRF = startupCapVideoCRF
	next.VideoMaxWidth = startupCapVideoMaxWidth
	next.VideoMaxRateK = startupCapVideoMaxRateK
	next.VideoBufSizeK = startupCapVideoBufSizeK
	next.AudioBitrateK = startupCapAudioBitrateK
	next.Preset = startupCapPreset
	return next, true
}

func shouldCapLiveStartupProfile(intent Intent, profile model.ProfileSpec, targetStep runtimepolicy.PlaybackLadderStep) bool {
	if strings.TrimSpace(intent.Mode) != "" && !strings.EqualFold(strings.TrimSpace(intent.Mode), model.ModeLive) {
		return false
	}
	if targetStep != runtimepolicy.PlaybackStepH2641080p {
		return false
	}
	if !profile.TranscodeVideo || !profile.Deinterlace {
		return false
	}
	if strings.TrimSpace(profile.HWAccel) != "" {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(profile.VideoCodec), "libx264") {
		return false
	}
	if profile.VideoMaxWidth > 0 && profile.VideoMaxWidth <= startupCapVideoMaxWidth && profile.VideoCRF >= startupCapVideoCRF {
		return false
	}
	return true
}

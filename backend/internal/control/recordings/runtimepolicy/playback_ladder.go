package runtimepolicy

import (
	"strings"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/normalize"
)

func NormalizePlaybackLadderStep(raw string) PlaybackLadderStep {
	switch normalize.Token(raw) {
	case string(PlaybackStepRepairLow):
		return PlaybackStepRepairLow
	case string(PlaybackStepH264720p):
		return PlaybackStepH264720p
	case string(PlaybackStepH2641080p):
		return PlaybackStepH2641080p
	case string(PlaybackStepAV11080p):
		return PlaybackStepAV11080p
	case string(PlaybackStepVideoCopyAudioAAC):
		return PlaybackStepVideoCopyAudioAAC
	case string(PlaybackStepDirectCopy):
		return PlaybackStepDirectCopy
	default:
		return PlaybackStepUnknown
	}
}

func PlaybackLadderStepFromQualityRung(rung playbackprofile.QualityRung) PlaybackLadderStep {
	switch playbackprofile.NormalizeQualityRung(string(rung)) {
	case playbackprofile.RungDirectCopy:
		return PlaybackStepDirectCopy
	case playbackprofile.RungQualityAudioAAC320Stereo,
		playbackprofile.RungCompatibleAudioAAC256Stereo,
		playbackprofile.RungRepairAudioAAC192Stereo:
		return PlaybackStepVideoCopyAudioAAC
	case playbackprofile.RungQualityVideoH264CRF20,
		playbackprofile.RungCompatibleVideoH264CRF23,
		playbackprofile.RungCompatibleHLSTS,
		playbackprofile.RungCompatibleHLSFMP4:
		return PlaybackStepH2641080p
	case playbackprofile.RungRepairVideoH264CRF28,
		playbackprofile.RungRepairH264AAC:
		return PlaybackStepRepairLow
	default:
		return PlaybackStepUnknown
	}
}

func PlaybackLadderStepFromTargetProfile(target *playbackprofile.TargetPlaybackProfile, rung playbackprofile.QualityRung) PlaybackLadderStep {
	if step := PlaybackLadderStepFromQualityRung(rung); step == PlaybackStepRepairLow {
		return step
	}
	if target == nil {
		return PlaybackLadderStepFromQualityRung(rung)
	}

	videoMode := strings.TrimSpace(string(target.Video.Mode))
	audioMode := strings.TrimSpace(string(target.Audio.Mode))
	switch {
	case videoMode == string(playbackprofile.MediaModeCopy) && audioMode == string(playbackprofile.MediaModeCopy):
		return PlaybackStepDirectCopy
	case videoMode == string(playbackprofile.MediaModeCopy) && audioMode == string(playbackprofile.MediaModeTranscode):
		return PlaybackStepVideoCopyAudioAAC
	case videoMode == string(playbackprofile.MediaModeTranscode):
		if strings.EqualFold(strings.TrimSpace(target.Video.Codec), "av1") {
			return PlaybackStepAV11080p
		}
		if target.Video.CRF >= 28 || strings.EqualFold(strings.TrimSpace(target.Video.Preset), "veryfast") {
			return PlaybackStepRepairLow
		}
		if target.Video.Width > 0 && target.Video.Width <= 1280 {
			return PlaybackStepH264720p
		}
		return PlaybackStepH2641080p
	default:
		return PlaybackLadderStepFromQualityRung(rung)
	}
}

func PlaybackLadderNextDown(step PlaybackLadderStep) (PlaybackLadderStep, bool) {
	switch NormalizePlaybackLadderStep(string(step)) {
	case PlaybackStepDirectCopy:
		return PlaybackStepVideoCopyAudioAAC, true
	case PlaybackStepVideoCopyAudioAAC:
		return PlaybackStepH2641080p, true
	case PlaybackStepAV11080p:
		return PlaybackStepH2641080p, true
	case PlaybackStepH2641080p:
		return PlaybackStepH264720p, true
	case PlaybackStepH264720p:
		return PlaybackStepRepairLow, true
	default:
		return PlaybackStepUnknown, false
	}
}

func PlaybackLadderNextUpTowards(current PlaybackLadderStep, target PlaybackLadderStep) (PlaybackLadderStep, bool) {
	path := playbackPathToTarget(target)
	if len(path) == 0 {
		return PlaybackStepUnknown, false
	}
	current = NormalizePlaybackLadderStep(string(current))
	for i, candidate := range path {
		if candidate != current {
			continue
		}
		if i+1 >= len(path) {
			return PlaybackStepUnknown, false
		}
		return path[i+1], true
	}
	return PlaybackStepUnknown, false
}

func playbackPathToTarget(target PlaybackLadderStep) []PlaybackLadderStep {
	switch NormalizePlaybackLadderStep(string(target)) {
	case PlaybackStepRepairLow:
		return []PlaybackLadderStep{PlaybackStepRepairLow}
	case PlaybackStepH264720p:
		return []PlaybackLadderStep{PlaybackStepRepairLow, PlaybackStepH264720p}
	case PlaybackStepH2641080p:
		return []PlaybackLadderStep{PlaybackStepRepairLow, PlaybackStepH264720p, PlaybackStepH2641080p}
	case PlaybackStepAV11080p:
		return []PlaybackLadderStep{PlaybackStepRepairLow, PlaybackStepH264720p, PlaybackStepH2641080p, PlaybackStepAV11080p}
	case PlaybackStepVideoCopyAudioAAC:
		return []PlaybackLadderStep{PlaybackStepRepairLow, PlaybackStepH264720p, PlaybackStepH2641080p, PlaybackStepVideoCopyAudioAAC}
	case PlaybackStepDirectCopy:
		return []PlaybackLadderStep{PlaybackStepRepairLow, PlaybackStepH264720p, PlaybackStepH2641080p, PlaybackStepVideoCopyAudioAAC, PlaybackStepDirectCopy}
	default:
		return nil
	}
}

func playbackLadderIndex(step PlaybackLadderStep) int {
	step = NormalizePlaybackLadderStep(string(step))
	switch step {
	case PlaybackStepRepairLow:
		return 0
	case PlaybackStepH264720p:
		return 1
	case PlaybackStepH2641080p:
		return 2
	case PlaybackStepAV11080p:
		return 3
	case PlaybackStepVideoCopyAudioAAC:
		return 4
	case PlaybackStepDirectCopy:
		return 5
	default:
		return -1
	}
}

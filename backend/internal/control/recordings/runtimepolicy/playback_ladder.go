package runtimepolicy

import (
	"strings"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/normalize"
)

var playbackLadder = []PlaybackLadderStep{
	PlaybackStepRepairLow,
	PlaybackStepH264720p,
	PlaybackStepH2641080p,
	PlaybackStepVideoCopyAudioAAC,
	PlaybackStepDirectCopy,
}

func NormalizePlaybackLadderStep(raw string) PlaybackLadderStep {
	switch normalize.Token(raw) {
	case string(PlaybackStepRepairLow):
		return PlaybackStepRepairLow
	case string(PlaybackStepH264720p):
		return PlaybackStepH264720p
	case string(PlaybackStepH2641080p):
		return PlaybackStepH2641080p
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
	index := playbackLadderIndex(step)
	if index <= 0 {
		return PlaybackStepUnknown, false
	}
	return playbackLadder[index-1], true
}

func PlaybackLadderNextUpTowards(current PlaybackLadderStep, target PlaybackLadderStep) (PlaybackLadderStep, bool) {
	currentIndex := playbackLadderIndex(current)
	targetIndex := playbackLadderIndex(target)
	if currentIndex < 0 || targetIndex < 0 || currentIndex >= targetIndex {
		return PlaybackStepUnknown, false
	}
	return playbackLadder[currentIndex+1], true
}

func playbackLadderIndex(step PlaybackLadderStep) int {
	step = NormalizePlaybackLadderStep(string(step))
	for i, candidate := range playbackLadder {
		if candidate == step {
			return i
		}
	}
	return -1
}

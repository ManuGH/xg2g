package artifacts

import (
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/normalize"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

func recordingTargetProfile(profile string) *playbackprofile.TargetPlaybackProfile {
	raw := normalize.Token(profile)
	publicProfile := profiles.PublicProfileName(raw)
	if raw == "android_native" || raw == "android_tv_native" {
		publicProfile = profiles.PublicProfileCompatible
	}
	packaging, segmentContainer, container := resolveRecordingPackaging(raw)

	target := playbackprofile.TargetPlaybackProfile{
		Container: container,
		Packaging: packaging,
		Video:     recordingVideoTarget(publicProfile),
		Audio:     recordingAudioTarget(publicProfile),
		HLS: playbackprofile.HLSTarget{
			Enabled:          true,
			SegmentContainer: segmentContainer,
			SegmentSeconds:   6,
		},
		HWAccel: playbackprofile.HWAccelNone,
	}
	canonical := playbackprofile.CanonicalizeTarget(target)
	return &canonical
}

func resolveRecordingPackaging(raw string) (playbackprofile.Packaging, string, string) {
	switch raw {
	case "android_native", "android_tv_native", "safari", "safari_dvr", "safari_dirty", "safari_hevc", "safari_hevc_hw", "safari_hevc_hw_ll", "h264_fmp4":
		return playbackprofile.PackagingFMP4, "fmp4", "mp4"
	default:
		return playbackprofile.PackagingTS, "mpegts", "mpegts"
	}
}

func recordingVideoTarget(publicProfile string) playbackprofile.VideoTarget {
	rung := recordingVideoQualityRung(publicProfile)
	if rung == playbackprofile.RungUnknown {
		return playbackprofile.VideoTarget{
			Mode: playbackprofile.MediaModeCopy,
		}
	}
	return playbackprofile.VideoTarget{
		Mode:   playbackprofile.MediaModeTranscode,
		Codec:  "h264",
		CRF:    playbackprofile.VideoCRFForRung(rung),
		Preset: playbackprofile.VideoPresetForRung(rung),
	}
}

func recordingAudioTarget(publicProfile string) playbackprofile.AudioTarget {
	target := playbackprofile.AudioTarget{
		Mode:       playbackprofile.MediaModeTranscode,
		Codec:      "aac",
		Channels:   2,
		SampleRate: 48000,
	}
	switch publicProfile {
	case string(playbackprofile.IntentQuality):
		target.BitrateKbps = 320
	case string(playbackprofile.IntentRepair):
		target.BitrateKbps = 192
	case string(playbackprofile.IntentDirect):
		target.Mode = playbackprofile.MediaModeCopy
	default:
		target.BitrateKbps = 256
	}
	return target
}

func recordingVideoQualityRung(publicProfile string) playbackprofile.QualityRung {
	switch publicProfile {
	case string(playbackprofile.IntentQuality):
		return playbackprofile.RungQualityVideoH264CRF20
	case string(playbackprofile.IntentRepair):
		return playbackprofile.RungRepairVideoH264CRF28
	case string(playbackprofile.IntentCompatible):
		return playbackprofile.RungCompatibleVideoH264CRF23
	default:
		return playbackprofile.RungUnknown
	}
}

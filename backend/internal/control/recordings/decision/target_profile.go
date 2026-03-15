package decision

import (
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/normalize"
)

const (
	defaultWebAACBitrateKbps = 256
	defaultWebAACChannels    = 2
	defaultWebAACSampleRate  = 48000
	qualityWebAACBitrateKbps = 320
	repairWebAACBitrateKbps  = 192
	hlsSegmentContainerTS    = "mpegts"
)

type targetProfileResolution struct {
	profile         *playbackprofile.TargetPlaybackProfile
	requestedIntent playbackprofile.PlaybackIntent
	resolvedIntent  playbackprofile.PlaybackIntent
	qualityRung     playbackprofile.QualityRung
	degradedFrom    playbackprofile.PlaybackIntent
}

func buildTargetProfile(mode Mode, pred Predicates, input DecisionInput) targetProfileResolution {
	requestedIntent := playbackprofile.NormalizeRequestedIntent(string(input.RequestedIntent))

	switch mode {
	case ModeDirectPlay:
		profile := playbackprofile.CanonicalizeTarget(playbackprofile.TargetPlaybackProfile{
			Container: normalizedContainer(input.Source.Container),
			Packaging: packagingFromContainer(input.Source.Container),
			Video: playbackprofile.VideoTarget{
				Mode:  playbackprofile.MediaModeCopy,
				Codec: input.Source.VideoCodec,
			},
			Audio: playbackprofile.AudioTarget{
				Mode:  playbackprofile.MediaModeCopy,
				Codec: input.Source.AudioCodec,
			},
			HWAccel: playbackprofile.HWAccelNone,
		})
		return targetProfileResolution{
			profile:         &profile,
			requestedIntent: requestedIntent,
			resolvedIntent:  playbackprofile.IntentDirect,
			qualityRung:     playbackprofile.RungDirectCopy,
		}
	case ModeDirectStream:
		profile := playbackprofile.CanonicalizeTarget(playbackprofile.TargetPlaybackProfile{
			Container: hlsSegmentContainerTS,
			Packaging: playbackprofile.PackagingTS,
			Video: playbackprofile.VideoTarget{
				Mode:  playbackprofile.MediaModeCopy,
				Codec: input.Source.VideoCodec,
			},
			Audio: playbackprofile.AudioTarget{
				Mode:  playbackprofile.MediaModeCopy,
				Codec: input.Source.AudioCodec,
			},
			HLS: playbackprofile.HLSTarget{
				Enabled:          true,
				SegmentContainer: hlsSegmentContainerTS,
			},
			HWAccel: playbackprofile.HWAccelNone,
		})
		resolution := targetProfileResolution{
			profile:         &profile,
			requestedIntent: requestedIntent,
			resolvedIntent:  playbackprofile.IntentCompatible,
			qualityRung:     playbackprofile.RungCompatibleHLSTS,
		}
		if requestedIntent == playbackprofile.IntentDirect {
			resolution.degradedFrom = requestedIntent
		}
		return resolution
	case ModeTranscode:
		resolvedIntent, degradedFrom := resolveTranscodeIntent(requestedIntent)
		video := playbackprofile.VideoTarget{
			Mode: playbackprofile.MediaModeCopy,
		}
		if pred.CanVideo && normalize.Token(input.Source.VideoCodec) != "" {
			video.Codec = input.Source.VideoCodec
		} else {
			video.Mode = playbackprofile.MediaModeTranscode
			video.Codec = "h264"
			video.Width = input.Source.Width
			video.Height = input.Source.Height
			video.FPS = input.Source.FPS
		}

		audio := playbackprofile.AudioTarget{
			Mode: playbackprofile.MediaModeCopy,
		}
		qualityRung := playbackprofile.RungUnknown
		if pred.CanAudio && normalize.Token(input.Source.AudioCodec) != "" {
			audio.Codec = input.Source.AudioCodec
		} else {
			audio = transcodeAudioTarget(resolvedIntent)
			qualityRung = rungForTranscodeIntent(resolvedIntent)
		}

		if video.Mode != playbackprofile.MediaModeTranscode && audio.Mode != playbackprofile.MediaModeTranscode {
			audio = transcodeAudioTarget(resolvedIntent)
			qualityRung = rungForTranscodeIntent(resolvedIntent)
		}

		profile := playbackprofile.CanonicalizeTarget(playbackprofile.TargetPlaybackProfile{
			Container: hlsSegmentContainerTS,
			Packaging: playbackprofile.PackagingTS,
			Video:     video,
			Audio:     audio,
			HLS: playbackprofile.HLSTarget{
				Enabled:          true,
				SegmentContainer: hlsSegmentContainerTS,
			},
			HWAccel: playbackprofile.HWAccelNone,
		})
		return targetProfileResolution{
			profile:         &profile,
			requestedIntent: requestedIntent,
			resolvedIntent:  resolvedIntent,
			qualityRung:     qualityRung,
			degradedFrom:    degradedFrom,
		}
	default:
		return targetProfileResolution{requestedIntent: requestedIntent}
	}
}

func resolveTranscodeIntent(requested playbackprofile.PlaybackIntent) (playbackprofile.PlaybackIntent, playbackprofile.PlaybackIntent) {
	switch requested {
	case playbackprofile.IntentQuality:
		return playbackprofile.IntentQuality, playbackprofile.IntentUnknown
	case playbackprofile.IntentRepair:
		return playbackprofile.IntentRepair, playbackprofile.IntentUnknown
	case playbackprofile.IntentDirect:
		return playbackprofile.IntentCompatible, playbackprofile.IntentDirect
	default:
		return playbackprofile.IntentCompatible, playbackprofile.IntentUnknown
	}
}

func transcodeAudioTarget(intent playbackprofile.PlaybackIntent) playbackprofile.AudioTarget {
	return playbackprofile.AudioTarget{
		Mode:        playbackprofile.MediaModeTranscode,
		Codec:       "aac",
		Channels:    defaultWebAACChannels,
		BitrateKbps: bitrateForIntent(intent),
		SampleRate:  defaultWebAACSampleRate,
	}
}

func bitrateForIntent(intent playbackprofile.PlaybackIntent) int {
	switch intent {
	case playbackprofile.IntentQuality:
		return qualityWebAACBitrateKbps
	case playbackprofile.IntentRepair:
		return repairWebAACBitrateKbps
	default:
		return defaultWebAACBitrateKbps
	}
}

func rungForTranscodeIntent(intent playbackprofile.PlaybackIntent) playbackprofile.QualityRung {
	switch intent {
	case playbackprofile.IntentQuality:
		return playbackprofile.RungQualityAudioAAC320Stereo
	case playbackprofile.IntentRepair:
		return playbackprofile.RungRepairAudioAAC192Stereo
	default:
		return playbackprofile.RungCompatibleAudioAAC256Stereo
	}
}

func packagingFromContainer(container string) playbackprofile.Packaging {
	switch normalize.Token(container) {
	case "mp4", "mov", "m4v":
		return playbackprofile.PackagingMP4
	case "mpegts", "ts":
		return playbackprofile.PackagingTS
	default:
		return playbackprofile.PackagingUnknown
	}
}

func normalizedContainer(container string) string {
	return normalize.Token(container)
}

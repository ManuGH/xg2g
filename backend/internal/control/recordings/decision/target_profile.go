package decision

import (
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/normalize"
)

const (
	defaultWebAACBitrateKbps = 256
	defaultWebAACChannels    = 2
	defaultWebAACSampleRate  = 48000
	hlsSegmentContainerTS    = "mpegts"
)

func buildTargetProfile(mode Mode, pred Predicates, input DecisionInput) *playbackprofile.TargetPlaybackProfile {
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
		return &profile
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
		return &profile
	case ModeTranscode:
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
		if pred.CanAudio && normalize.Token(input.Source.AudioCodec) != "" {
			audio.Codec = input.Source.AudioCodec
		} else {
			audio.Mode = playbackprofile.MediaModeTranscode
			audio.Codec = "aac"
			audio.Channels = defaultWebAACChannels
			audio.BitrateKbps = defaultWebAACBitrateKbps
			audio.SampleRate = defaultWebAACSampleRate
		}

		if video.Mode != playbackprofile.MediaModeTranscode && audio.Mode != playbackprofile.MediaModeTranscode {
			audio.Mode = playbackprofile.MediaModeTranscode
			audio.Codec = "aac"
			audio.Channels = defaultWebAACChannels
			audio.BitrateKbps = defaultWebAACBitrateKbps
			audio.SampleRate = defaultWebAACSampleRate
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
		return &profile
	default:
		return nil
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

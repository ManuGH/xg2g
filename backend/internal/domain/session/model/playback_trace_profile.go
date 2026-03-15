// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package model

import (
	"strings"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
)

func TraceTargetProfileFromProfile(profile ProfileSpec) *playbackprofile.TargetPlaybackProfile {
	container := "mpegts"
	packaging := playbackprofile.PackagingTS
	segmentContainer := "mpegts"
	if strings.EqualFold(strings.TrimSpace(profile.Container), "fmp4") {
		container = "fmp4"
		packaging = playbackprofile.PackagingFMP4
		segmentContainer = "fmp4"
	}

	videoMode := playbackprofile.MediaModeCopy
	videoCodec := ""
	if profile.TranscodeVideo {
		videoMode = playbackprofile.MediaModeTranscode
		videoCodec = normalizeTraceVideoCodec(profile.VideoCodec)
		if videoCodec == "" {
			videoCodec = "h264"
		}
	}

	audioBitrate := profile.AudioBitrateK
	if audioBitrate <= 0 {
		audioBitrate = 192
	}

	hwAccel := playbackprofile.HWAccelNone
	if strings.TrimSpace(profile.HWAccel) != "" {
		hwAccel = playbackprofile.HWAccelVAAPI
	}

	return &playbackprofile.TargetPlaybackProfile{
		Container: container,
		Packaging: packaging,
		Video: playbackprofile.VideoTarget{
			Mode:  videoMode,
			Codec: videoCodec,
			Width: profile.VideoMaxWidth,
		},
		Audio: playbackprofile.AudioTarget{
			Mode:        playbackprofile.MediaModeTranscode,
			Codec:       "aac",
			Channels:    2,
			BitrateKbps: audioBitrate,
			SampleRate:  48000,
		},
		HLS: playbackprofile.HLSTarget{
			Enabled:          true,
			SegmentContainer: segmentContainer,
		},
		HWAccel: hwAccel,
	}
}

func TraceFFmpegPlanFromProfile(profile ProfileSpec, inputKind string, segmentSeconds int) *FFmpegPlanTrace {
	target := TraceTargetProfileFromProfile(profile)
	if target == nil {
		return nil
	}

	audioCodec := target.Audio.Codec
	if audioCodec == "" {
		audioCodec = "aac"
	}
	videoCodec := target.Video.Codec
	if target.Video.Mode == playbackprofile.MediaModeCopy {
		videoCodec = "copy"
	} else if videoCodec == "" {
		videoCodec = "h264"
	}

	plan := &FFmpegPlanTrace{
		InputKind:  inputKind,
		Container:  target.Container,
		Packaging:  string(target.Packaging),
		HWAccel:    string(target.HWAccel),
		VideoMode:  string(target.Video.Mode),
		VideoCodec: videoCodec,
		AudioMode:  string(target.Audio.Mode),
		AudioCodec: audioCodec,
	}
	if plan.HWAccel == "" {
		plan.HWAccel = string(playbackprofile.HWAccelNone)
	}
	if segmentSeconds > 0 {
		_ = segmentSeconds
	}
	return plan
}

func TraceStopClassFromReason(reason ReasonCode) PlaybackStopClass {
	switch reason {
	case RClientStop, RCancelled, RIdleTimeout:
		return PlaybackStopClassOperator
	case RUpstreamCorrupt, RTuneFailed, RTuneTimeout:
		return PlaybackStopClassInput
	case RPackagerFailed:
		return PlaybackStopClassPackager
	case RProcessEnded, RInternalInvariantBreach, RPipelineStartFailed:
		return PlaybackStopClassServer
	default:
		return PlaybackStopClassServer
	}
}

func normalizeTraceVideoCodec(codec string) string {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "", "copy":
		return ""
	case "libx264", "h264_vaapi", "h264":
		return "h264"
	case "libx265", "hevc_vaapi", "hevc":
		return "hevc"
	default:
		return strings.ToLower(strings.TrimSpace(codec))
	}
}

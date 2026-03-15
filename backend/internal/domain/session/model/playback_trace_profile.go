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
			Mode:   videoMode,
			Codec:  videoCodec,
			CRF:    traceVideoCRF(profile),
			Preset: traceVideoPreset(profile),
			Width:  profile.VideoMaxWidth,
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

func TraceVideoQualityRungFromProfile(profile ProfileSpec) string {
	if !profile.TranscodeVideo {
		return ""
	}
	if strings.TrimSpace(profile.HWAccel) != "" {
		return ""
	}
	if traceResolvedVideoCodec(profile) != "h264" {
		return ""
	}

	switch {
	case profile.VideoCRF == playbackprofile.VideoCRFForRung(playbackprofile.RungQualityVideoH264CRF20) &&
		strings.EqualFold(strings.TrimSpace(profile.Preset), playbackprofile.VideoPresetForRung(playbackprofile.RungQualityVideoH264CRF20)):
		return string(playbackprofile.RungQualityVideoH264CRF20)
	case profile.VideoCRF == playbackprofile.VideoCRFForRung(playbackprofile.RungRepairVideoH264CRF28) &&
		strings.EqualFold(strings.TrimSpace(profile.Preset), playbackprofile.VideoPresetForRung(playbackprofile.RungRepairVideoH264CRF28)):
		return string(playbackprofile.RungRepairVideoH264CRF28)
	case profile.VideoCRF == playbackprofile.VideoCRFForRung(playbackprofile.RungCompatibleVideoH264CRF23) &&
		strings.EqualFold(strings.TrimSpace(profile.Preset), playbackprofile.VideoPresetForRung(playbackprofile.RungCompatibleVideoH264CRF23)):
		return string(playbackprofile.RungCompatibleVideoH264CRF23)
	default:
		return ""
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

func traceVideoCRF(profile ProfileSpec) int {
	if !profile.TranscodeVideo {
		return 0
	}
	if strings.TrimSpace(profile.HWAccel) != "" {
		return 0
	}
	if traceResolvedVideoCodec(profile) != "h264" {
		return 0
	}
	if profile.VideoCRF > 0 {
		return profile.VideoCRF
	}
	return 0
}

func traceVideoPreset(profile ProfileSpec) string {
	if !profile.TranscodeVideo {
		return ""
	}
	if strings.TrimSpace(profile.HWAccel) != "" {
		return ""
	}
	if traceResolvedVideoCodec(profile) != "h264" {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(profile.Preset))
}

func traceResolvedVideoCodec(profile ProfileSpec) string {
	codec := normalizeTraceVideoCodec(profile.VideoCodec)
	if codec != "" {
		return codec
	}
	if profile.TranscodeVideo {
		return "h264"
	}
	return ""
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

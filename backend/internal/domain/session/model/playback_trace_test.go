// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package model

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlaybackTraceClone_DeepCopiesNestedFields(t *testing.T) {
	trace := &PlaybackTrace{
		Source: &playbackprofile.SourceProfile{
			Container:     "mpegts",
			VideoCodec:    "h264",
			AudioCodec:    "aac",
			AudioChannels: 2,
		},
		RequestProfile:    "compatible",
		ClientPath:        "hlsjs",
		InputKind:         "receiver",
		TargetProfileHash: "hash-1",
		TargetProfile: &playbackprofile.TargetPlaybackProfile{
			Container: "mpegts",
			Packaging: playbackprofile.PackagingTS,
			Video: playbackprofile.VideoTarget{
				Mode:  playbackprofile.MediaModeCopy,
				Codec: "h264",
			},
			Audio: playbackprofile.AudioTarget{
				Mode:        playbackprofile.MediaModeTranscode,
				Codec:       "aac",
				Channels:    2,
				BitrateKbps: 256,
			},
		},
		FFmpegPlan: &FFmpegPlanTrace{
			InputKind: "receiver",
			Container: "mpegts",
			Packaging: "ts",
			HWAccel:   "none",
			VideoMode: "copy",
			AudioMode: "transcode",
		},
		Operator: &PlaybackOperatorTrace{
			ForcedIntent:           "repair",
			MaxQualityRung:         "repair_audio_aac_192_stereo",
			ClientFallbackDisabled: true,
			RuleName:               "problem-channel",
			RuleScope:              "live",
			OverrideApplied:        true,
		},
		Client: &PlaybackClientSnapshot{
			CapHash:             "cap-hash-1",
			ClientCapsSource:    "runtime_plus_family",
			ClientFamily:        "chromium_hlsjs",
			PreferredHLSEngine:  "hlsjs",
			DeviceType:          "web",
			RuntimeProbeUsed:    true,
			RuntimeProbeVersion: 2,
			DeviceContext: &PlaybackClientDeviceContext{
				Platform:  "browser",
				OSName:    "macos",
				OSVersion: "15.4",
				Model:     "macbookpro",
			},
			NetworkContext: &PlaybackClientNetworkContext{
				Kind:         "wifi",
				DownlinkKbps: 54000,
			},
		},
		HLS: &HLSAccessTrace{
			PlaylistRequestCount:   2,
			LastPlaylistAtUnix:     111,
			LastPlaylistIntervalMs: 2100,
			SegmentRequestCount:    1,
			LastSegmentAtUnix:      112,
			LastSegmentName:        "seg_000001.ts",
			LastSegmentGapMs:       1900,
			LatestSegmentLagMs:     1200,
			StallRisk:              "low",
			StartupMode:            "trace_guarded",
			StartupHeadroomSec:     10,
			StartupReasons:         []string{"client_family_native", "segment_cadence_guard"},
		},
		HostPressureBand:    "constrained",
		HostOverrideApplied: true,
		FirstFrameAtUnix:    123,
		Fallbacks: []PlaybackFallbackTrace{{
			AtUnix:          456,
			Trigger:         "mediaError",
			Reason:          "bufferAppendError",
			FromProfileHash: "hash-1",
			ToProfileHash:   "hash-2",
		}},
		StopReason: "playlist_not_ready",
		StopClass:  PlaybackStopClassPackager,
	}

	cloned := trace.Clone()
	require.NotNil(t, cloned)
	require.NotSame(t, trace, cloned)
	require.NotSame(t, trace.Source, cloned.Source)
	require.NotSame(t, trace.TargetProfile, cloned.TargetProfile)
	require.NotSame(t, trace.FFmpegPlan, cloned.FFmpegPlan)
	require.NotSame(t, trace.Operator, cloned.Operator)
	require.NotSame(t, trace.Client, cloned.Client)
	require.NotSame(t, trace.HLS, cloned.HLS)

	cloned.Source.AudioCodec = "ac3"
	cloned.TargetProfile.Audio.Codec = "mp3"
	cloned.FFmpegPlan.AudioCodec = "mp3"
	cloned.Operator.ForcedIntent = "quality"
	cloned.Operator.RuleName = "different-channel"
	cloned.Client.ClientFamily = "safari_native"
	cloned.Client.DeviceContext.Platform = "android"
	cloned.HLS.LastSegmentName = "seg_000777.ts"
	cloned.HLS.StallRisk = "segment_stale"
	cloned.HLS.StartupReasons[0] = "trace_segment_gap"
	cloned.HostPressureBand = "critical"
	cloned.HostOverrideApplied = false
	cloned.Fallbacks[0].Reason = "networkError"

	assert.Equal(t, "aac", trace.Source.AudioCodec)
	assert.Equal(t, "aac", trace.TargetProfile.Audio.Codec)
	assert.Equal(t, "", trace.FFmpegPlan.AudioCodec)
	assert.Equal(t, "repair", trace.Operator.ForcedIntent)
	assert.Equal(t, "problem-channel", trace.Operator.RuleName)
	assert.Equal(t, "chromium_hlsjs", trace.Client.ClientFamily)
	assert.Equal(t, "browser", trace.Client.DeviceContext.Platform)
	assert.Equal(t, "seg_000001.ts", trace.HLS.LastSegmentName)
	assert.Equal(t, "low", trace.HLS.StallRisk)
	assert.Equal(t, []string{"client_family_native", "segment_cadence_guard"}, trace.HLS.StartupReasons)
	assert.Equal(t, "constrained", trace.HostPressureBand)
	assert.True(t, trace.HostOverrideApplied)
	assert.Equal(t, "bufferAppendError", trace.Fallbacks[0].Reason)
}

func TestPlaybackTraceClone_NilSafe(t *testing.T) {
	var trace *PlaybackTrace
	assert.Nil(t, trace.Clone())
}

func TestTraceTargetProfileFromProfile_DefaultsToCompatibleHLSOutput(t *testing.T) {
	target := TraceTargetProfileFromProfile(ProfileSpec{Name: "compatible"})
	require.NotNil(t, target)
	assert.Equal(t, "mpegts", target.Container)
	assert.Equal(t, playbackprofile.PackagingTS, target.Packaging)
	assert.Equal(t, playbackprofile.MediaModeCopy, target.Video.Mode)
	assert.Equal(t, playbackprofile.MediaModeTranscode, target.Audio.Mode)
	assert.Equal(t, "aac", target.Audio.Codec)
	assert.Equal(t, 192, target.Audio.BitrateKbps)
}

func TestTraceTargetProfileFromProfile_MapsCPUH264VideoLadderFields(t *testing.T) {
	target := TraceTargetProfileFromProfile(ProfileSpec{
		Name:           "repair",
		TranscodeVideo: true,
		VideoCodec:     "libx264",
		VideoCRF:       28,
		Preset:         "veryfast",
	})
	require.NotNil(t, target)
	assert.Equal(t, playbackprofile.MediaModeTranscode, target.Video.Mode)
	assert.Equal(t, "h264", target.Video.Codec)
	assert.Equal(t, 28, target.Video.CRF)
	assert.Equal(t, "veryfast", target.Video.Preset)
}

func TestTraceVideoQualityRungFromProfile_MapsKnownCPUH264Ladders(t *testing.T) {
	assert.Equal(t, string(playbackprofile.RungCompatibleVideoH264CRF23), TraceVideoQualityRungFromProfile(ProfileSpec{
		Name:           "compatible",
		TranscodeVideo: true,
		VideoCodec:     "libx264",
		VideoCRF:       23,
		Preset:         "fast",
	}))
	assert.Equal(t, string(playbackprofile.RungRepairVideoH264CRF28), TraceVideoQualityRungFromProfile(ProfileSpec{
		Name:           "repair",
		TranscodeVideo: true,
		VideoCodec:     "libx264",
		VideoCRF:       28,
		Preset:         "veryfast",
	}))
	assert.Equal(t, "", TraceVideoQualityRungFromProfile(ProfileSpec{
		Name:           "safari",
		TranscodeVideo: true,
		VideoCodec:     "h264_vaapi",
		HWAccel:        "vaapi",
		VideoCRF:       16,
	}))
}

func TestTraceFFmpegPlanFromProfile_UsesFMP4AndVAAPIWhenConfigured(t *testing.T) {
	plan := TraceFFmpegPlanFromProfile(ProfileSpec{
		Name:           "safari",
		Container:      "fmp4",
		TranscodeVideo: true,
		VideoCodec:     "h264_vaapi",
		HWAccel:        "vaapi",
		AudioBitrateK:  256,
	}, "tuner", 6)
	require.NotNil(t, plan)
	assert.Equal(t, "tuner", plan.InputKind)
	assert.Equal(t, "fmp4", plan.Container)
	assert.Equal(t, "fmp4", plan.Packaging)
	assert.Equal(t, "vaapi", plan.HWAccel)
	assert.Equal(t, "transcode", plan.VideoMode)
	assert.Equal(t, "h264", plan.VideoCodec)
	assert.Equal(t, "transcode", plan.AudioMode)
	assert.Equal(t, "aac", plan.AudioCodec)
}

func TestTraceStopClassFromReason_MapsLifecycleReasons(t *testing.T) {
	assert.Equal(t, PlaybackStopClassInput, TraceStopClassFromReason(RTuneTimeout))
	assert.Equal(t, PlaybackStopClassPackager, TraceStopClassFromReason(RPackagerFailed))
	assert.Equal(t, PlaybackStopClassOperator, TraceStopClassFromReason(RClientStop))
}

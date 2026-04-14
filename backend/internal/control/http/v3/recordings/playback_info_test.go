package recordings

import (
	"context"
	"errors"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/playback"
	domainrecordings "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestService_ResolvePlaybackInfo_Unavailable(t *testing.T) {
	svc := NewService(stubDeps{})

	_, err := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:   "rec1",
		SubjectKind: PlaybackSubjectRecording,
	})
	require.NotNil(t, err)
	assert.Equal(t, PlaybackInfoErrorUnavailable, err.Kind)
	assert.Equal(t, "Recordings service is not initialized", err.Message)
}

func TestService_ResolvePlaybackInfo_RecordingSuccess(t *testing.T) {
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/demo.ts"
	recordingID := domainrecordings.EncodeRecordingID(serviceRef)
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			return playback.MediaTruth{
				Status:     playback.MediaStatusReady,
				Container:  "mp4",
				VideoCodec: "h264",
				AudioCodec: "aac",
				Width:      1920,
				Height:     1080,
				FPS:        25,
			}, nil
		},
	}

	svc := NewService(stubDeps{
		svc: recSvc,
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	})

	res, err := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:   recordingID,
		SubjectKind: PlaybackSubjectRecording,
		APIVersion:  "v3.1",
		SchemaType:  "compact",
		RequestID:   "req-1",
	})
	require.Nil(t, err)
	require.NotNil(t, res.Decision)
	assert.Equal(t, serviceRef, res.SourceRef)
	assert.Equal(t, playback.MediaStatusReady, res.Truth.Status)
	assert.Equal(t, 1, res.ResolvedCapabilities.CapabilitiesVersion)
	assert.Equal(t, "req-1", res.Decision.Trace.RequestID)
	assert.Equal(t, 1, recSvc.truthCalls)
	assert.Equal(t, recordingID, recSvc.lastTruthID)
}

func TestService_ResolvePlaybackInfo_RecordingNativeHLSUsesFMP4Target(t *testing.T) {
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/nfs-recordings/demo.ts"
	recordingID := domainrecordings.EncodeRecordingID(serviceRef)
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			return playback.MediaTruth{
				Status:     playback.MediaStatusReady,
				Container:  "mpegts",
				VideoCodec: "h264",
				AudioCodec: "mp2",
				Width:      1920,
				Height:     1080,
				FPS:        25,
			}, nil
		},
	}

	svc := NewService(stubDeps{
		svc: recSvc,
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	})

	allowTranscode := true
	res, err := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:   recordingID,
		SubjectKind: PlaybackSubjectRecording,
		APIVersion:  "v3.1",
		SchemaType:  "compact",
		RequestID:   "req-native-rec",
		Capabilities: &capabilities.PlaybackCapabilities{
			CapabilitiesVersion:  3,
			Containers:           []string{"hls", "mpegts", "mp4"},
			VideoCodecs:          []string{"h264"},
			AudioCodecs:          []string{"aac", "ac3"},
			SupportsHLS:          true,
			DeviceType:           "android_tv",
			HLSEngines:           []string{"native"},
			PreferredHLSEngine:   "native",
			ClientFamilyFallback: "android_tv_native",
			AllowTranscode:       &allowTranscode,
		},
	})
	require.Nil(t, err)
	require.NotNil(t, res.Decision)
	require.NotNil(t, res.Decision.TargetProfile)
	assert.Equal(t, decision.ModeTranscode, res.Decision.Mode)
	assert.Equal(t, playbackprofile.PackagingFMP4, res.Decision.TargetProfile.Packaging)
	assert.Equal(t, "mp4", res.Decision.TargetProfile.Container)
	assert.Equal(t, "fmp4", res.Decision.TargetProfile.HLS.SegmentContainer)
}

func TestService_ResolvePlaybackInfo_RecordingTranscodeUsesMeasuredAutoCodecProfile(t *testing.T) {
	hardware.SetVAAPIPreflightResult(true)
	hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{
		"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 90},
		"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 40},
	})
	t.Cleanup(func() {
		hardware.SetVAAPIPreflightResult(false)
		hardware.SetVAAPIEncoderCapabilities(nil)
	})

	serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/nfs-recordings/hevc-auto.ts"
	recordingID := domainrecordings.EncodeRecordingID(serviceRef)
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			return playback.MediaTruth{
				Status:     playback.MediaStatusReady,
				Container:  "mpegts",
				VideoCodec: "mpeg2",
				AudioCodec: "aac",
				Width:      1920,
				Height:     1080,
				FPS:        25,
			}, nil
		},
	}

	svc := NewService(stubDeps{
		svc: recSvc,
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	})

	hevcSmooth := true
	hevcEfficient := true
	h264Smooth := true
	h264Efficient := true
	allowTranscode := true
	res, err := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:        recordingID,
		SubjectKind:      PlaybackSubjectRecording,
		APIVersion:       "v3.1",
		SchemaType:       "compact",
		RequestID:        "req-recording-hevc-auto",
		RequestedProfile: "quality",
		Capabilities: &capabilities.PlaybackCapabilities{
			CapabilitiesVersion:  3,
			Containers:           []string{"hls", "mp4"},
			VideoCodecs:          []string{"hevc"},
			AudioCodecs:          []string{"aac"},
			SupportsHLS:          true,
			ClientFamilyFallback: "android_tv_native",
			AllowTranscode:       &allowTranscode,
			RuntimeProbeUsed:     true,
			RuntimeProbeVersion:  2,
			VideoCodecSignals: []capabilities.VideoCodecSignal{
				{Codec: "hevc", Supported: true, Smooth: &hevcSmooth, PowerEfficient: &hevcEfficient},
				{Codec: "h264", Supported: true, Smooth: &h264Smooth, PowerEfficient: &h264Efficient},
			},
		},
	})
	require.Nil(t, err)
	require.NotNil(t, res.Decision)
	require.NotNil(t, res.Decision.TargetProfile)
	assert.Equal(t, decision.ModeTranscode, res.Decision.Mode)
	assert.Equal(t, "hevc", res.Decision.Selected.VideoCodec)
	assert.Equal(t, "quality", res.Decision.Trace.RequestedIntent)
	assert.Equal(t, "quality", res.Decision.Trace.ResolvedIntent)
	assert.Equal(t, playbackprofile.PackagingFMP4, res.Decision.TargetProfile.Packaging)
	assert.Equal(t, "mp4", res.Decision.TargetProfile.Container)
	assert.Equal(t, "hevc", res.Decision.TargetProfile.Video.Codec)
}

func TestService_ResolvePlaybackInfo_RecordingNativePrefersDirectStreamForCopyableTS(t *testing.T) {
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/nfs-recordings/demo.ts"
	recordingID := domainrecordings.EncodeRecordingID(serviceRef)
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			return playback.MediaTruth{
				Status:     playback.MediaStatusReady,
				Container:  "mpegts",
				VideoCodec: "h264",
				AudioCodec: "ac3",
				Width:      1920,
				Height:     1080,
				FPS:        25,
			}, nil
		},
	}

	svc := NewService(stubDeps{
		svc: recSvc,
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	})

	allowTranscode := true
	res, err := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:   recordingID,
		SubjectKind: PlaybackSubjectRecording,
		APIVersion:  "v3.1",
		SchemaType:  "compact",
		RequestID:   "req-native-direct-stream",
		Capabilities: &capabilities.PlaybackCapabilities{
			CapabilitiesVersion:  3,
			Containers:           []string{"hls", "mpegts", "mp4"},
			VideoCodecs:          []string{"h264"},
			AudioCodecs:          []string{"aac", "ac3"},
			SupportsHLS:          true,
			DeviceType:           "android_tv",
			HLSEngines:           []string{"native"},
			PreferredHLSEngine:   "native",
			ClientFamilyFallback: "android_tv_native",
			AllowTranscode:       &allowTranscode,
		},
	})
	require.Nil(t, err)
	require.NotNil(t, res.Decision)
	require.NotNil(t, res.Decision.TargetProfile)
	assert.Equal(t, decision.ModeDirectStream, res.Decision.Mode)
	assert.Equal(t, []decision.ReasonCode{decision.ReasonDirectStreamMatch}, res.Decision.Reasons)
	assert.Equal(t, "hls", res.Decision.SelectedOutputKind)
	assert.Equal(t, playbackprofile.PackagingFMP4, res.Decision.TargetProfile.Packaging)
	assert.Equal(t, "mp4", res.Decision.TargetProfile.Container)
	assert.Equal(t, "fmp4", res.Decision.TargetProfile.HLS.SegmentContainer)
	assert.Equal(t, playbackprofile.MediaModeCopy, res.Decision.TargetProfile.Video.Mode)
	assert.Equal(t, "h264", res.Decision.TargetProfile.Video.Codec)
	assert.Equal(t, playbackprofile.MediaModeCopy, res.Decision.TargetProfile.Audio.Mode)
	assert.Equal(t, "ac3", res.Decision.TargetProfile.Audio.Codec)
	assert.Equal(t, string(playbackprofile.RungCompatibleHLSFMP4), res.Decision.Trace.QualityRung)
	assert.Equal(t, string(playbackprofile.IntentCompatible), res.Decision.Trace.ResolvedIntent)
}

func TestService_ResolvePlaybackInfo_RecordingIOSSafariNativeKeepsTSDirectStreamForCopyableTS(t *testing.T) {
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/nfs-recordings/demo.ts"
	recordingID := domainrecordings.EncodeRecordingID(serviceRef)
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			return playback.MediaTruth{
				Status:     playback.MediaStatusReady,
				Container:  "mpegts",
				VideoCodec: "h264",
				AudioCodec: "ac3",
				Width:      1920,
				Height:     1080,
				FPS:        25,
			}, nil
		},
	}

	svc := NewService(stubDeps{
		svc: recSvc,
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	})

	allowTranscode := true
	res, err := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:   recordingID,
		SubjectKind: PlaybackSubjectRecording,
		APIVersion:  "v3.1",
		SchemaType:  "compact",
		RequestID:   "req-safari-native-direct-stream",
		Capabilities: &capabilities.PlaybackCapabilities{
			CapabilitiesVersion:  3,
			Containers:           []string{"hls", "mpegts", "mp4"},
			VideoCodecs:          []string{"h264"},
			AudioCodecs:          []string{"aac", "ac3"},
			SupportsHLS:          true,
			DeviceType:           "safari",
			HLSEngines:           []string{"native"},
			PreferredHLSEngine:   "native",
			ClientFamilyFallback: "ios_safari_native",
			DeviceContext: &capabilities.DeviceContext{
				OSName:        "ios",
				PlatformClass: "ios_webkit",
			},
			AllowTranscode: &allowTranscode,
		},
	})
	require.Nil(t, err)
	require.NotNil(t, res.Decision)
	require.NotNil(t, res.Decision.TargetProfile)
	assert.Equal(t, decision.ModeDirectStream, res.Decision.Mode)
	assert.Equal(t, "hls", res.Decision.SelectedOutputKind)
	assert.Equal(t, []decision.ReasonCode{decision.ReasonDirectStreamMatch}, res.Decision.Reasons)
	assert.Equal(t, playbackprofile.PackagingTS, res.Decision.TargetProfile.Packaging)
	assert.Equal(t, "mpegts", res.Decision.TargetProfile.Container)
	assert.Equal(t, "mpegts", res.Decision.TargetProfile.HLS.SegmentContainer)
	assert.Equal(t, playbackprofile.MediaModeCopy, res.Decision.TargetProfile.Video.Mode)
	assert.Equal(t, "h264", res.Decision.TargetProfile.Video.Codec)
	assert.Equal(t, playbackprofile.MediaModeCopy, res.Decision.TargetProfile.Audio.Mode)
	assert.Equal(t, "ac3", res.Decision.TargetProfile.Audio.Codec)
	assert.Equal(t, string(playbackprofile.RungCompatibleHLSTS), res.Decision.Trace.QualityRung)
	assert.Equal(t, string(playbackprofile.IntentCompatible), res.Decision.Trace.ResolvedIntent)
}

func TestService_ResolvePlaybackInfo_RecordingSafariNativeWithoutAC3FallsBackToCompatibleFMP4(t *testing.T) {
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/nfs-recordings/demo.ts"
	recordingID := domainrecordings.EncodeRecordingID(serviceRef)
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			return playback.MediaTruth{
				Status:     playback.MediaStatusReady,
				Container:  "mpegts",
				VideoCodec: "h264",
				AudioCodec: "ac3",
				Width:      1920,
				Height:     1080,
				FPS:        25,
			}, nil
		},
	}

	svc := NewService(stubDeps{
		svc: recSvc,
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	})

	allowTranscode := true
	res, err := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:   recordingID,
		SubjectKind: PlaybackSubjectRecording,
		APIVersion:  "v3.1",
		SchemaType:  "compact",
		RequestID:   "req-safari-native-compatible",
		Capabilities: &capabilities.PlaybackCapabilities{
			CapabilitiesVersion:  3,
			Containers:           []string{"hls", "mpegts", "mp4"},
			VideoCodecs:          []string{"h264"},
			AudioCodecs:          []string{"aac"},
			SupportsHLS:          true,
			DeviceType:           "safari",
			HLSEngines:           []string{"native"},
			PreferredHLSEngine:   "native",
			ClientFamilyFallback: "safari_native",
			DeviceContext: &capabilities.DeviceContext{
				OSName:        "macos",
				PlatformClass: "macos_safari",
			},
			AllowTranscode: &allowTranscode,
		},
	})
	require.Nil(t, err)
	require.NotNil(t, res.Decision)
	require.NotNil(t, res.Decision.TargetProfile)
	assert.Equal(t, decision.ModeTranscode, res.Decision.Mode)
	assert.Equal(t, "hls", res.Decision.SelectedOutputKind)
	assert.Equal(t, playbackprofile.PackagingFMP4, res.Decision.TargetProfile.Packaging)
	assert.Equal(t, "mp4", res.Decision.TargetProfile.Container)
	assert.Equal(t, "fmp4", res.Decision.TargetProfile.HLS.SegmentContainer)
	assert.Equal(t, playbackprofile.MediaModeTranscode, res.Decision.TargetProfile.Video.Mode)
	assert.Equal(t, "h264", res.Decision.TargetProfile.Video.Codec)
	assert.Equal(t, playbackprofile.MediaModeTranscode, res.Decision.TargetProfile.Audio.Mode)
	assert.Equal(t, "aac", res.Decision.TargetProfile.Audio.Codec)
	assert.Equal(t, string(playbackprofile.RungCompatibleAudioAAC256Stereo), res.Decision.Trace.QualityRung)
	assert.Equal(t, string(playbackprofile.IntentRepair), res.Decision.Trace.ResolvedIntent)
}

func TestShouldPreserveNativeSafariRecordingTransport_PrefersPlatformClass(t *testing.T) {
	assert.True(t, shouldPreserveNativeSafariRecordingTransport(capabilities.PlaybackCapabilities{
		PreferredHLSEngine:   "native",
		ClientFamilyFallback: "safari_native",
		DeviceContext: &capabilities.DeviceContext{
			OSName:        "macos",
			PlatformClass: "ios_webkit",
		},
	}))

	assert.False(t, shouldPreserveNativeSafariRecordingTransport(capabilities.PlaybackCapabilities{
		PreferredHLSEngine:   "native",
		ClientFamilyFallback: "ios_safari_native",
		DeviceContext: &capabilities.DeviceContext{
			OSName:        "ios",
			PlatformClass: "macos_safari",
		},
	}))
}

func verifiedLiveTruthSource(cap scan.Capability) *stubTruthSource {
	return &stubTruthSource{
		getCapabilityFn: func(serviceRef string) (scan.Capability, bool) {
			if cap.ServiceRef == "" {
				cap.ServiceRef = serviceRef
			}
			return cap, true
		},
	}
}

func TestService_ResolvePlaybackInfo_LiveScannerUnavailableFailsClosed(t *testing.T) {
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			t.Fatal("GetMediaTruth must not be called for live playback")
			return playback.MediaTruth{}, nil
		},
	}
	svc := NewService(stubDeps{
		svc: recSvc,
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	})

	_, err := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:   "1:0:1:2B66:3F3:1:C00000:0:0:0:",
		SubjectKind: PlaybackSubjectLive,
		APIVersion:  "v3.1",
		SchemaType:  "live",
		RequestID:   "req-live",
	})
	require.NotNil(t, err)
	assert.Equal(t, PlaybackInfoErrorUnverified, err.Kind)
	assert.Equal(t, "unverified", err.TruthState)
	assert.Equal(t, "scanner_unavailable", err.TruthReason)
	assert.Equal(t, "live_unverified", err.TruthOrigin)
	assert.Contains(t, err.ProblemFlags, "live_truth_unverified")
	assert.Contains(t, err.ProblemFlags, "scanner_unavailable")
	assert.Equal(t, 0, recSvc.truthCalls)
}

func TestService_ResolvePlaybackInfo_LiveUsesScanTruthWhenAvailable(t *testing.T) {
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			t.Fatal("GetMediaTruth must not be called for live playback")
			return playback.MediaTruth{}, nil
		},
	}
	truthSource := &stubTruthSource{
		getCapabilityFn: func(serviceRef string) (scan.Capability, bool) {
			return scan.Capability{
				ServiceRef: serviceRef,
				State:      scan.CapabilityStateOK,
				Container:  "ts",
				VideoCodec: "hevc",
				AudioCodec: "ac3",
				Codec:      "hevc",
				Resolution: "3840x2160",
				Width:      3840,
				Height:     2160,
				FPS:        50,
				Interlaced: true,
			}, true
		},
	}
	svc := NewService(stubDeps{
		svc:         recSvc,
		truthSource: truthSource,
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	})

	res, err := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:   "1:0:1:2B66:3F3:1:C00000:0:0:0:",
		SubjectKind: PlaybackSubjectLive,
		APIVersion:  "v3.1",
		SchemaType:  "live",
		RequestID:   "req-live-scan",
	})
	require.Nil(t, err)
	require.NotNil(t, res.Decision)
	assert.Equal(t, "ts", res.Truth.Container)
	assert.Equal(t, "hevc", res.Truth.VideoCodec)
	assert.Equal(t, "ac3", res.Truth.AudioCodec)
	assert.Equal(t, 3840, res.Truth.Width)
	assert.Equal(t, 2160, res.Truth.Height)
	assert.Equal(t, 50.0, res.Truth.FPS)
	assert.True(t, res.Truth.Interlaced)
	assert.Equal(t, 0, recSvc.truthCalls)
	assert.Equal(t, 1, truthSource.calls)
	assert.Equal(t, "1:0:1:2B66:3F3:1:C00000:0:0:0:", truthSource.lastServiceRef)
}

func TestService_ResolvePlaybackInfo_LiveInterlacedTruthRepairsVideoInsteadOfPassthrough(t *testing.T) {
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			t.Fatal("GetMediaTruth must not be called for live playback")
			return playback.MediaTruth{}, nil
		},
	}
	truthSource := &stubTruthSource{
		getCapabilityFn: func(serviceRef string) (scan.Capability, bool) {
			return scan.Capability{
				ServiceRef: serviceRef,
				State:      scan.CapabilityStateOK,
				Container:  "mpegts",
				VideoCodec: "h264",
				AudioCodec: "aac",
				Codec:      "h264",
				Resolution: "1920x1080",
				Width:      1920,
				Height:     1080,
				FPS:        50,
				Interlaced: true,
			}, true
		},
	}
	svc := NewService(stubDeps{
		svc:         recSvc,
		truthSource: truthSource,
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	})

	allowTranscode := true
	res, err := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:        "1:0:1:2B66:3F3:1:C00000:0:0:0:",
		SubjectKind:      PlaybackSubjectLive,
		APIVersion:       "v3.1",
		SchemaType:       "live",
		RequestID:        "req-live-interlaced-repair",
		RequestedProfile: "compatible",
		Capabilities: &capabilities.PlaybackCapabilities{
			CapabilitiesVersion:  3,
			Containers:           []string{"hls", "mpegts", "mp4"},
			VideoCodecs:          []string{"h264"},
			AudioCodecs:          []string{"aac"},
			SupportsHLS:          true,
			ClientFamilyFallback: "chromium_hlsjs",
			AllowTranscode:       &allowTranscode,
		},
	})
	require.Nil(t, err)
	require.NotNil(t, res.Decision)
	require.NotNil(t, res.Decision.TargetProfile)
	assert.Equal(t, decision.ModeTranscode, res.Decision.Mode)
	assert.Equal(t, playbackprofile.MediaModeTranscode, res.Decision.TargetProfile.Video.Mode)
	assert.Equal(t, "h264", res.Decision.TargetProfile.Video.Codec)
	assert.Equal(t, playbackprofile.MediaModeCopy, res.Decision.TargetProfile.Audio.Mode)
	assert.Equal(t, "aac", res.Decision.TargetProfile.Audio.Codec)
}

func TestService_ResolvePlaybackInfo_LiveAndroidNativeCopyableTSReturnsFMP4DirectStream(t *testing.T) {
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			t.Fatal("GetMediaTruth must not be called for live playback")
			return playback.MediaTruth{}, nil
		},
	}
	truthSource := &stubTruthSource{
		getCapabilityFn: func(serviceRef string) (scan.Capability, bool) {
			return scan.Capability{
				ServiceRef: serviceRef,
				State:      scan.CapabilityStateOK,
				Container:  "mpegts",
				VideoCodec: "h264",
				AudioCodec: "ac3",
				Codec:      "h264",
				Resolution: "1920x1080",
				Width:      1920,
				Height:     1080,
				FPS:        25,
				Interlaced: false,
			}, true
		},
	}
	svc := NewService(stubDeps{
		svc:         recSvc,
		truthSource: truthSource,
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	})

	allowTranscode := true
	supportsRange := true
	res, err := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:   "1:0:1:2B66:3F3:1:C00000:0:0:0:",
		SubjectKind: PlaybackSubjectLive,
		APIVersion:  "v3.1",
		SchemaType:  "live",
		RequestID:   "req-live-android-native-fmp4",
		Capabilities: &capabilities.PlaybackCapabilities{
			CapabilitiesVersion:  3,
			Containers:           []string{"hls", "fmp4", "mpegts", "ts", "mp4"},
			VideoCodecs:          []string{"h264"},
			AudioCodecs:          []string{"aac", "ac3"},
			SupportsHLS:          true,
			SupportsRange:        &supportsRange,
			DeviceType:           "android_tv",
			HLSEngines:           []string{"native"},
			PreferredHLSEngine:   "native",
			ClientFamilyFallback: "android_tv_native",
			AllowTranscode:       &allowTranscode,
		},
	})
	require.Nil(t, err)
	require.NotNil(t, res.Decision)
	require.NotNil(t, res.Decision.TargetProfile)
	assert.Equal(t, decision.ModeDirectStream, res.Decision.Mode)
	assert.Equal(t, "hls", res.Decision.SelectedOutputKind)
	assert.Equal(t, []decision.ReasonCode{decision.ReasonDirectStreamMatch}, res.Decision.Reasons)
	assert.Equal(t, "fmp4", res.Decision.Selected.Container)
	assert.Equal(t, playbackprofile.PackagingFMP4, res.Decision.TargetProfile.Packaging)
	assert.Equal(t, "fmp4", res.Decision.TargetProfile.Container)
	assert.Equal(t, "fmp4", res.Decision.TargetProfile.HLS.SegmentContainer)
	assert.Equal(t, playbackprofile.MediaModeCopy, res.Decision.TargetProfile.Video.Mode)
	assert.Equal(t, "h264", res.Decision.TargetProfile.Video.Codec)
	assert.Equal(t, playbackprofile.MediaModeCopy, res.Decision.TargetProfile.Audio.Mode)
	assert.Equal(t, "ac3", res.Decision.TargetProfile.Audio.Codec)
	assert.Equal(t, string(playbackprofile.RungCompatibleHLSFMP4), res.Decision.Trace.QualityRung)
	assert.Equal(t, string(playbackprofile.IntentCompatible), res.Decision.Trace.ResolvedIntent)
}

func TestService_ResolvePlaybackInfo_LiveMissingScanTruthFailsClosed(t *testing.T) {
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			t.Fatal("GetMediaTruth must not be called for live playback")
			return playback.MediaTruth{}, nil
		},
	}
	truthSource := &stubTruthSource{
		getCapabilityFn: func(serviceRef string) (scan.Capability, bool) {
			return scan.Capability{}, false
		},
	}
	svc := NewService(stubDeps{
		svc:         recSvc,
		truthSource: truthSource,
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	})

	_, err := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:   "1:0:1:2B66:3F3:1:C00000:0:0:0:",
		SubjectKind: PlaybackSubjectLive,
		APIVersion:  "v3.1",
		SchemaType:  "live",
		RequestID:   "req-live-missing",
	})
	require.NotNil(t, err)
	assert.Equal(t, PlaybackInfoErrorUnverified, err.Kind)
	assert.Equal(t, "unverified", err.TruthState)
	assert.Equal(t, "missing_scan_truth", err.TruthReason)
	assert.Equal(t, "live_unverified", err.TruthOrigin)
	assert.Contains(t, err.ProblemFlags, "missing_scan_truth")
	assert.Equal(t, 0, recSvc.truthCalls)
	assert.Equal(t, 1, truthSource.calls)
}

func TestService_ResolvePlaybackInfo_LiveMissingScanTruthUsesTargetedProbe(t *testing.T) {
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			t.Fatal("GetMediaTruth must not be called for live playback")
			return playback.MediaTruth{}, nil
		},
	}

	capability := scan.Capability{
		ServiceRef: "1:0:1:2B66:3F3:1:C00000:0:0:0:",
		State:      scan.CapabilityStateOK,
		Container:  "ts",
		VideoCodec: "h264",
		AudioCodec: "aac",
		Codec:      "h264",
		Resolution: "1920x1080",
		Width:      1920,
		Height:     1080,
		FPS:        25,
	}

	truthSource := &stubTruthSource{}
	truthSource.getCapabilityFn = func(serviceRef string) (scan.Capability, bool) {
		if truthSource.probeCalls == 0 {
			return scan.Capability{}, false
		}
		return capability, true
	}
	truthSource.probeCapabilityFn = func(ctx context.Context, serviceRef string) (scan.Capability, bool, error) {
		return capability, true, nil
	}

	svc := NewService(stubDeps{
		svc:         recSvc,
		truthSource: truthSource,
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	})

	res, err := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:   "1:0:1:2B66:3F3:1:C00000:0:0:0:",
		SubjectKind: PlaybackSubjectLive,
		APIVersion:  "v3.1",
		SchemaType:  "live",
		RequestID:   "req-live-targeted-probe",
	})
	require.Nil(t, err)
	require.NotNil(t, res.Decision)
	assert.Equal(t, "ts", res.Truth.Container)
	assert.Equal(t, "h264", res.Truth.VideoCodec)
	assert.Equal(t, "aac", res.Truth.AudioCodec)
	assert.Equal(t, 1, truthSource.calls)
	assert.Equal(t, 1, truthSource.probeCalls)
	assert.Equal(t, "1:0:1:2B66:3F3:1:C00000:0:0:0:", truthSource.lastProbeRef)
}

func TestService_ResolvePlaybackInfo_LiveTargetedProbeFailureStillFailsClosed(t *testing.T) {
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			t.Fatal("GetMediaTruth must not be called for live playback")
			return playback.MediaTruth{}, nil
		},
	}

	truthSource := &stubTruthSource{
		getCapabilityFn: func(serviceRef string) (scan.Capability, bool) {
			return scan.Capability{}, false
		},
		probeCapabilityFn: func(ctx context.Context, serviceRef string) (scan.Capability, bool, error) {
			return scan.Capability{
				ServiceRef:    serviceRef,
				State:         scan.CapabilityStateFailed,
				FailureReason: "ffprobe failed: signal: killed",
			}, true, errors.New("ffprobe failed: signal: killed")
		},
	}

	svc := NewService(stubDeps{
		svc:         recSvc,
		truthSource: truthSource,
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	})

	_, err := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:   "1:0:1:2B66:3F3:1:C00000:0:0:0:",
		SubjectKind: PlaybackSubjectLive,
		APIVersion:  "v3.1",
		SchemaType:  "live",
		RequestID:   "req-live-targeted-probe-fail",
	})
	require.NotNil(t, err)
	assert.Equal(t, PlaybackInfoErrorUnverified, err.Kind)
	assert.Equal(t, "failed", err.TruthState)
	assert.Equal(t, "failed_scan_truth", err.TruthReason)
	assert.Contains(t, err.ProblemFlags, "failed_scan_truth")
	assert.Equal(t, 1, truthSource.probeCalls)
}

func TestService_ResolvePlaybackInfo_LiveIncompleteScanTruthFailsClosed(t *testing.T) {
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			t.Fatal("GetMediaTruth must not be called for live playback")
			return playback.MediaTruth{}, nil
		},
	}
	truthSource := &stubTruthSource{
		getCapabilityFn: func(serviceRef string) (scan.Capability, bool) {
			return scan.Capability{
				ServiceRef: serviceRef,
				State:      scan.CapabilityStatePartial,
				VideoCodec: "hevc",
				Codec:      "hevc",
				Resolution: "1920x1080",
				Width:      1920,
				Height:     1080,
			}, true
		},
	}
	svc := NewService(stubDeps{
		svc:         recSvc,
		truthSource: truthSource,
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	})

	_, err := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:   "1:0:1:2B66:3F3:1:C00000:0:0:0:",
		SubjectKind: PlaybackSubjectLive,
		APIVersion:  "v3.1",
		SchemaType:  "live",
		RequestID:   "req-live-partial",
	})
	require.NotNil(t, err)
	assert.Equal(t, PlaybackInfoErrorUnverified, err.Kind)
	assert.Equal(t, "partial", err.TruthState)
	assert.Equal(t, "partial_scan_truth", err.TruthReason)
	assert.Equal(t, "live_unverified", err.TruthOrigin)
	assert.Contains(t, err.ProblemFlags, "incomplete_scan_truth")
	assert.Contains(t, err.ProblemFlags, "partial_scan_truth")
	assert.Equal(t, 0, recSvc.truthCalls)
	assert.Equal(t, 1, truthSource.calls)
}

func TestService_ResolvePlaybackInfo_LiveInactiveEventFeedFailsClosed(t *testing.T) {
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			t.Fatal("GetMediaTruth must not be called for live playback")
			return playback.MediaTruth{}, nil
		},
	}
	svc := NewService(stubDeps{
		svc:         recSvc,
		truthSource: verifiedLiveTruthSource(scan.Capability{State: scan.CapabilityStateInactiveEventFeed, FailureReason: "inactive_event_feed_no_media_metadata"}),
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	})

	_, err := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:   "1:0:1:2B66:3F3:1:C00000:0:0:0:",
		SubjectKind: PlaybackSubjectLive,
		APIVersion:  "v3.1",
		SchemaType:  "live",
		RequestID:   "req-live-inactive",
	})
	require.NotNil(t, err)
	assert.Equal(t, PlaybackInfoErrorUnverified, err.Kind)
	assert.Equal(t, "inactive_event_feed", err.TruthState)
	assert.Equal(t, "inactive_event_feed", err.TruthReason)
	assert.Equal(t, "live_unverified", err.TruthOrigin)
	assert.Contains(t, err.ProblemFlags, "inactive_event_feed")
	assert.Equal(t, 0, recSvc.truthCalls)
}

func TestService_ResolvePlaybackInfo_LiveTranscodeUsesMeasuredAutoCodecProfile(t *testing.T) {
	hardware.SetVAAPIPreflightResult(true)
	hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{
		"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 90},
		"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 40},
	})
	t.Cleanup(func() {
		hardware.SetVAAPIPreflightResult(false)
		hardware.SetVAAPIEncoderCapabilities(nil)
	})

	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			t.Fatal("GetMediaTruth must not be called for live playback")
			return playback.MediaTruth{}, nil
		},
	}
	svc := NewService(stubDeps{
		svc:         recSvc,
		truthSource: verifiedLiveTruthSource(scan.Capability{State: scan.CapabilityStateOK, Container: "ts", VideoCodec: "h264", AudioCodec: "aac", Width: 1920, Height: 1080, FPS: 25}),
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	})

	hevcSmooth := true
	hevcEfficient := true
	h264Smooth := true
	h264Efficient := true
	res, err := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:   "1:0:19:EF75:3F9:1:C00000:0:0:0:",
		SubjectKind: PlaybackSubjectLive,
		APIVersion:  "v3.1",
		SchemaType:  "live",
		RequestID:   "req-live-transcode",
		Capabilities: &capabilities.PlaybackCapabilities{
			CapabilitiesVersion: 3,
			Containers:          []string{"hls", "mp4"},
			VideoCodecs:         []string{"hevc"},
			AudioCodecs:         []string{"aac", "ac3", "mp2"},
			SupportsHLS:         true,
			RuntimeProbeUsed:    true,
			RuntimeProbeVersion: 2,
			VideoCodecSignals: []capabilities.VideoCodecSignal{
				{Codec: "hevc", Supported: true, Smooth: &hevcSmooth, PowerEfficient: &hevcEfficient},
				{Codec: "h264", Supported: true, Smooth: &h264Smooth, PowerEfficient: &h264Efficient},
			},
		},
	})
	require.Nil(t, err)
	require.NotNil(t, res.Decision)
	require.NotNil(t, res.Decision.TargetProfile)
	assert.Equal(t, "fmp4", res.Decision.Selected.Container)
	assert.Equal(t, "hevc", res.Decision.Selected.VideoCodec)
	assert.Equal(t, "aac", res.Decision.Selected.AudioCodec)
	assert.Equal(t, "quality", res.Decision.Trace.RequestedIntent)
	assert.Equal(t, "quality", res.Decision.Trace.ResolvedIntent)
	assert.Equal(t, "hevc", res.Decision.TargetProfile.Video.Codec)
	assert.Equal(t, "fmp4", res.Decision.TargetProfile.Container)
}

func TestService_ResolvePlaybackInfo_LiveRepairIntentSkipsAutoCodecUpgrade(t *testing.T) {
	hardware.SetVAAPIPreflightResult(true)
	hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{
		"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 90},
		"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 40},
	})
	t.Cleanup(func() {
		hardware.SetVAAPIPreflightResult(false)
		hardware.SetVAAPIEncoderCapabilities(nil)
	})

	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			t.Fatal("GetMediaTruth must not be called for live playback")
			return playback.MediaTruth{}, nil
		},
	}
	svc := NewService(stubDeps{
		svc:         recSvc,
		truthSource: verifiedLiveTruthSource(scan.Capability{State: scan.CapabilityStateOK, Container: "ts", VideoCodec: "h264", AudioCodec: "aac", Width: 1920, Height: 1080, FPS: 25}),
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	})

	hevcSmooth := true
	hevcEfficient := true
	h264Smooth := true
	h264Efficient := true
	res, err := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:        "1:0:19:EF75:3F9:1:C00000:0:0:0:",
		SubjectKind:      PlaybackSubjectLive,
		APIVersion:       "v3.1",
		SchemaType:       "live",
		RequestID:        "req-live-repair-no-auto",
		RequestedProfile: "repair",
		Capabilities: &capabilities.PlaybackCapabilities{
			CapabilitiesVersion: 3,
			Containers:          []string{"hls", "mp4"},
			VideoCodecs:         []string{"hevc"},
			AudioCodecs:         []string{"aac", "ac3", "mp2"},
			SupportsHLS:         true,
			RuntimeProbeUsed:    true,
			RuntimeProbeVersion: 2,
			VideoCodecSignals: []capabilities.VideoCodecSignal{
				{Codec: "hevc", Supported: true, Smooth: &hevcSmooth, PowerEfficient: &hevcEfficient},
				{Codec: "h264", Supported: true, Smooth: &h264Smooth, PowerEfficient: &h264Efficient},
			},
		},
	})
	require.Nil(t, err)
	require.NotNil(t, res.Decision)
	require.NotNil(t, res.Decision.TargetProfile)
	assert.Equal(t, decision.ModeTranscode, res.Decision.Mode)
	assert.Equal(t, "repair", res.Decision.Trace.RequestedIntent)
	assert.Equal(t, "repair", res.Decision.Trace.ResolvedIntent)
	assert.Equal(t, "h264", res.Decision.TargetProfile.Video.Codec)
	assert.NotEqual(t, "hevc", res.Decision.Selected.VideoCodec)
}

func TestService_ResolvePlaybackInfo_RecordsDecisionAuditAfterFinalAlignment(t *testing.T) {
	hardware.SetVAAPIPreflightResult(true)
	hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{
		"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 90},
		"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 40},
	})
	t.Cleanup(func() {
		hardware.SetVAAPIPreflightResult(false)
		hardware.SetVAAPIEncoderCapabilities(nil)
	})

	auditSink := &stubDecisionAuditSink{}
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			t.Fatal("GetMediaTruth must not be called for live playback")
			return playback.MediaTruth{}, nil
		},
	}
	svc := NewService(stubDeps{
		svc:         recSvc,
		truthSource: verifiedLiveTruthSource(scan.Capability{State: scan.CapabilityStateOK, Container: "ts", VideoCodec: "h264", AudioCodec: "aac", Width: 1920, Height: 1080, FPS: 25}),
		auditSink:   auditSink,
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	})

	hevcSmooth := true
	hevcEfficient := true
	h264Smooth := true
	h264Efficient := true
	res, err := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:        "1:0:19:EF75:3F9:1:C00000:0:0:0:",
		SubjectKind:      PlaybackSubjectLive,
		APIVersion:       "v3.1",
		SchemaType:       "live",
		RequestID:        "req-live-audit",
		ClientProfile:    "safari",
		RequestedProfile: "quality",
		Capabilities: &capabilities.PlaybackCapabilities{
			CapabilitiesVersion: 3,
			Containers:          []string{"hls", "mp4"},
			VideoCodecs:         []string{"hevc"},
			AudioCodecs:         []string{"aac", "ac3", "mp2"},
			SupportsHLS:         true,
			RuntimeProbeUsed:    true,
			RuntimeProbeVersion: 2,
			ClientCapsSource:    "runtime_plus_family",
			VideoCodecSignals: []capabilities.VideoCodecSignal{
				{Codec: "hevc", Supported: true, Smooth: &hevcSmooth, PowerEfficient: &hevcEfficient},
				{Codec: "h264", Supported: true, Smooth: &h264Smooth, PowerEfficient: &h264Efficient},
			},
		},
	})
	require.Nil(t, err)
	require.NotNil(t, res.Decision)
	require.Equal(t, 1, auditSink.callCount)
	require.Equal(t, decision.OriginRuntime, auditSink.lastEvent.Origin)
	require.Equal(t, "safari", auditSink.lastEvent.ClientFamily)
	require.Equal(t, "quality", auditSink.lastEvent.RequestedIntent)
	require.Equal(t, res.Decision.Trace.InputHash, auditSink.lastEvent.BasisHash)
	require.Equal(t, decision.ModeTranscode, auditSink.lastEvent.Mode)
	require.NotNil(t, auditSink.lastEvent.TargetProfile)
	require.Equal(t, "fmp4", auditSink.lastEvent.TargetProfile.Container)
	require.Equal(t, "hevc", auditSink.lastEvent.TargetProfile.Video.Codec)
	require.Equal(t, "aac", auditSink.lastEvent.TargetProfile.Audio.Codec)
}

func TestService_ResolvePlaybackInfo_RecordsShadowDecisionAuditPerRequest(t *testing.T) {
	auditSink := &stubDecisionAuditSink{}
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/mystery.ts"
	recordingID := domainrecordings.EncodeRecordingID(serviceRef)
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			return playback.MediaTruth{
				Status:     playback.MediaStatusReady,
				Container:  "mp4",
				VideoCodec: "mysterycodec",
				AudioCodec: "aac",
				Width:      1280,
				Height:     720,
				FPS:        25,
			}, nil
		},
	}

	trueVal := true
	svc := NewService(stubDeps{
		svc:       recSvc,
		auditSink: auditSink,
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	})

	res, err := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:     recordingID,
		SubjectKind:   PlaybackSubjectRecording,
		APIVersion:    "v3.1",
		SchemaType:    "compact",
		RequestID:     "req-shadow-audit",
		ClientProfile: "browser",
		Capabilities: &capabilities.PlaybackCapabilities{
			CapabilitiesVersion: 3,
			Containers:          []string{"mp4", "hls"},
			VideoCodecs:         []string{"mysterycodec"},
			AudioCodecs:         []string{"aac"},
			SupportsHLS:         true,
			SupportsRange:       &trueVal,
			ClientCapsSource:    "runtime_plus_family",
		},
	})
	require.Nil(t, err)
	require.NotNil(t, res.Decision)
	require.Equal(t, 2, auditSink.callCount)
	require.Len(t, auditSink.events, 2)
	require.Equal(t, decision.OriginRuntime, auditSink.events[0].Origin)
	require.Equal(t, decision.OriginShadowDivergence, auditSink.events[1].Origin)
	require.NotNil(t, auditSink.events[1].Shadow)
	require.Equal(t, "video", auditSink.events[1].Shadow.Predicate)
	require.Equal(t, []string{"codec_mismatch"}, auditSink.events[1].Shadow.NewReasons)
	require.True(t, auditSink.events[1].Shadow.LegacyCompatibleWithoutRepair)
	require.False(t, auditSink.events[1].Shadow.NewCompatible)
}

func TestService_ResolvePlaybackInfo_PreparingStatus(t *testing.T) {
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/demo.ts"
	recordingID := domainrecordings.EncodeRecordingID(serviceRef)
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			return playback.MediaTruth{
				Status:     playback.MediaStatusPreparing,
				RetryAfter: 17,
				ProbeState: playback.ProbeStateInFlight,
			}, nil
		},
	}

	svc := NewService(stubDeps{svc: recSvc})
	_, err := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:   recordingID,
		SubjectKind: PlaybackSubjectRecording,
	})
	require.NotNil(t, err)
	assert.Equal(t, PlaybackInfoErrorPreparing, err.Kind)
	assert.Equal(t, 17, err.RetryAfterSeconds)
	assert.Equal(t, string(playback.ProbeStateInFlight), err.ProbeState)
	assert.Equal(t, 1, recSvc.truthCalls)
}

func TestService_ResolvePlaybackInfo_ClassifiesErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		inErr      error
		wantKind   PlaybackInfoErrorKind
		wantMsg    string
		retry      int
		probe      string
		checkCause bool
	}{
		{
			name:       "invalid argument",
			inErr:      domainrecordings.ErrInvalidArgument{Field: "id", Reason: "bad"},
			wantKind:   PlaybackInfoErrorInvalidInput,
			wantMsg:    "invalid argument id: bad",
			checkCause: true,
		},
		{
			name:       "forbidden",
			inErr:      domainrecordings.ErrForbidden{},
			wantKind:   PlaybackInfoErrorForbidden,
			wantMsg:    "forbidden",
			checkCause: true,
		},
		{
			name:       "not found",
			inErr:      domainrecordings.ErrNotFound{RecordingID: "rec1"},
			wantKind:   PlaybackInfoErrorNotFound,
			wantMsg:    "recording not found: rec1",
			checkCause: true,
		},
		{
			name:       "preparing",
			inErr:      domainrecordings.ErrPreparing{RecordingID: "rec1"},
			wantKind:   PlaybackInfoErrorPreparing,
			wantMsg:    "recording preparing: rec1",
			retry:      5,
			probe:      string(playback.ProbeStateInFlight),
			checkCause: true,
		},
		{
			name:       "unsupported",
			inErr:      domainrecordings.ErrRemoteProbeUnsupported,
			wantKind:   PlaybackInfoErrorUnsupported,
			wantMsg:    domainrecordings.ErrRemoteProbeUnsupported.Error(),
			checkCause: true,
		},
		{
			name:       "upstream",
			inErr:      domainrecordings.ErrUpstream{Op: "truth", Cause: errors.New("timeout")},
			wantKind:   PlaybackInfoErrorUpstreamUnavailable,
			wantMsg:    "upstream error in truth: timeout",
			checkCause: true,
		},
		{
			name:       "internal",
			inErr:      errors.New("boom"),
			wantKind:   PlaybackInfoErrorInternal,
			wantMsg:    "An unexpected error occurred",
			checkCause: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/demo.ts"
			recordingID := domainrecordings.EncodeRecordingID(serviceRef)
			recSvc := &stubRecordingsService{
				getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
					return playback.MediaTruth{}, tt.inErr
				},
			}
			svc := NewService(stubDeps{svc: recSvc})

			_, err := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
				SubjectID:   recordingID,
				SubjectKind: PlaybackSubjectRecording,
			})
			require.NotNil(t, err)
			assert.Equal(t, tt.wantKind, err.Kind)
			assert.Equal(t, tt.wantMsg, err.Message)
			assert.Equal(t, tt.retry, err.RetryAfterSeconds)
			assert.Equal(t, tt.probe, err.ProbeState)
			if tt.checkCause {
				assert.Equal(t, tt.inErr, err.Cause)
			}
			assert.Equal(t, 1, recSvc.truthCalls)
			assert.Equal(t, recordingID, recSvc.lastTruthID)
		})
	}
}

func TestService_ResolvePlaybackInfo_Problem(t *testing.T) {
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/demo.ts"
	recordingID := domainrecordings.EncodeRecordingID(serviceRef)
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			return playback.MediaTruth{
				Status:     playback.MediaStatusReady,
				Container:  "mp4",
				VideoCodec: "h264",
				AudioCodec: "aac",
			}, nil
		},
	}

	svc := NewService(stubDeps{svc: recSvc})
	_, err := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:   recordingID,
		SubjectKind: PlaybackSubjectRecording,
		APIVersion:  "",
		SchemaType:  "compact",
	})
	require.NotNil(t, err)
	assert.Equal(t, PlaybackInfoErrorProblem, err.Kind)
	require.NotNil(t, err.Problem)
	assert.Equal(t, 400, err.Problem.Status)
	assert.Equal(t, string(decision.ProblemCapabilitiesInvalid), err.Problem.Code)
}

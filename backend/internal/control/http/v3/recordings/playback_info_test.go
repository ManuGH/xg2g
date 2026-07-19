package recordings

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/playback"
	domainrecordings "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/control/recordings/capreg"
	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/control/recordings/runtimepolicy"
	"github.com/ManuGH/xg2g/internal/domain/playbackcompat"
	"github.com/ManuGH/xg2g/internal/domain/playbackplanner"
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

func TestBuildDecisionInput_PropagatesHostPerformanceAndBenchmarkClass(t *testing.T) {
	allowTranscode := true
	input := buildDecisionInput(
		PlaybackInfoRequest{
			RequestID:        "req-1",
			RequestedProfile: "quality",
			APIVersion:       "v3.1",
		},
		playback.MediaTruth{
			Container:         "mpegts",
			VideoCodec:        "mpeg2",
			AudioCodec:        "aac",
			BitrateKbps:       9500,
			BitrateConfidence: "high",
			Width:             1920,
			Height:            1080,
			FPS:               25,
			Interlaced:        true,
		},
		capabilities.PlaybackCapabilities{
			CapabilitiesVersion: 1,
			AllowTranscode:      &allowTranscode,
		},
		config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
		config.PlaybackOperatorConfig{},
		playbackprofile.HostPressureAssessment{EffectiveBand: playbackprofile.HostPressureNormal},
		playbackprofile.HostRuntimeSnapshot{
			PerformanceClass: "medium",
			Benchmark: playbackprofile.HostBenchmarkSnapshot{
				Class: "strong",
				Profiles: []playbackprofile.HostProfileBenchmark{
					{ProfileID: playbackprofile.BenchmarkProfileVideoH2641080I, Class: "weak"},
					{ProfileID: playbackprofile.BenchmarkProfileVideoH2641080P, Class: "moderate"},
				},
			},
		},
	)

	if input.Policy.Host.PerformanceClass != "medium" {
		t.Fatalf("expected medium host class in decision input, got %#v", input.Policy.Host)
	}
	if input.Policy.Host.BenchmarkClass != "weak" {
		t.Fatalf("expected h264-specific weak benchmark class in decision input, got %#v", input.Policy.Host)
	}
	if input.Source.BitrateKbps != 9500 {
		t.Fatalf("expected source bitrate to remain in decision input, got %#v", input.Source)
	}
	if input.Source.BitrateConfidence != "high" {
		t.Fatalf("expected bitrate confidence to propagate into decision input, got %#v", input.Source)
	}
}

func TestBuildDecisionInput_FallsBackToCodecBenchmarkClassWhenNoProfileBenchmarkExists(t *testing.T) {
	allowTranscode := true
	input := buildDecisionInput(
		PlaybackInfoRequest{
			RequestID:        "req-2",
			RequestedProfile: "quality",
			APIVersion:       "v3.1",
		},
		playback.MediaTruth{
			Container:   "mpegts",
			VideoCodec:  "mpeg2",
			AudioCodec:  "aac",
			BitrateKbps: 3500,
			Width:       1280,
			Height:      720,
			FPS:         25,
		},
		capabilities.PlaybackCapabilities{
			CapabilitiesVersion: 1,
			AllowTranscode:      &allowTranscode,
		},
		config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
		config.PlaybackOperatorConfig{},
		playbackprofile.HostPressureAssessment{EffectiveBand: playbackprofile.HostPressureNormal},
		playbackprofile.HostRuntimeSnapshot{
			PerformanceClass: "medium",
			Benchmark: playbackprofile.HostBenchmarkSnapshot{
				Class: "strong",
				Codecs: []playbackprofile.HostCodecBenchmark{
					{Codec: "h264", Class: "moderate"},
				},
			},
		},
	)

	if input.Policy.Host.BenchmarkClass != "moderate" {
		t.Fatalf("expected codec-level fallback benchmark class in decision input, got %#v", input.Policy.Host)
	}
}

func TestBuildDecisionInput_UsesAudioOnlyBenchmarkClassWhenVideoCanCopy(t *testing.T) {
	allowTranscode := true
	input := buildDecisionInput(
		PlaybackInfoRequest{
			RequestID:        "req-audio",
			RequestedProfile: "quality",
			APIVersion:       "v3.1",
		},
		playback.MediaTruth{
			Container:   "mp4",
			VideoCodec:  "h264",
			AudioCodec:  "ac3",
			BitrateKbps: 6000,
			Width:       1920,
			Height:      1080,
			FPS:         25,
		},
		capabilities.PlaybackCapabilities{
			CapabilitiesVersion: 1,
			AllowTranscode:      &allowTranscode,
			VideoCodecs:         []string{"h264"},
			AudioCodecs:         []string{"aac"},
			MaxVideo:            &capabilities.MaxVideo{Width: 1920, Height: 1080, Fps: 60},
		},
		config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
		config.PlaybackOperatorConfig{},
		playbackprofile.HostPressureAssessment{EffectiveBand: playbackprofile.HostPressureNormal},
		playbackprofile.HostRuntimeSnapshot{
			PerformanceClass: "low",
			Benchmark: playbackprofile.HostBenchmarkSnapshot{
				Class: "strong",
				Profiles: []playbackprofile.HostProfileBenchmark{
					{ProfileID: playbackprofile.BenchmarkProfileAudioAACStereo, Class: "weak"},
					{ProfileID: playbackprofile.BenchmarkProfileVideoH2641080P, Class: "strong"},
				},
			},
		},
	)

	if input.Policy.Host.BenchmarkClass != "weak" {
		t.Fatalf("expected audio-only benchmark class in decision input, got %#v", input.Policy.Host)
	}
}

func TestBuildDecisionInput_DoesNotUseAudioOnlyBenchmarkClassWhenVideoNeedsRepair(t *testing.T) {
	allowTranscode := true
	input := buildDecisionInput(
		PlaybackInfoRequest{
			RequestID:        "req-repair",
			RequestedProfile: "quality",
			APIVersion:       "v3.1",
		},
		playback.MediaTruth{
			Container:   "mp4",
			VideoCodec:  "h264",
			AudioCodec:  "ac3",
			BitrateKbps: 6000,
			Width:       1920,
			Height:      1080,
			FPS:         25,
			Interlaced:  true,
		},
		capabilities.PlaybackCapabilities{
			CapabilitiesVersion: 1,
			AllowTranscode:      &allowTranscode,
			VideoCodecs:         []string{"h264"},
			AudioCodecs:         []string{"aac"},
			MaxVideo:            &capabilities.MaxVideo{Width: 1920, Height: 1080, Fps: 60},
		},
		config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
		config.PlaybackOperatorConfig{},
		playbackprofile.HostPressureAssessment{EffectiveBand: playbackprofile.HostPressureNormal},
		playbackprofile.HostRuntimeSnapshot{
			PerformanceClass: "low",
			Benchmark: playbackprofile.HostBenchmarkSnapshot{
				Class: "strong",
				Profiles: []playbackprofile.HostProfileBenchmark{
					{ProfileID: playbackprofile.BenchmarkProfileAudioAACStereo, Class: "weak"},
					{ProfileID: playbackprofile.BenchmarkProfileVideoH2641080I, Class: "strong"},
				},
			},
		},
	)

	if input.Policy.Host.BenchmarkClass != "strong" {
		t.Fatalf("expected video-repair benchmark class in decision input, got %#v", input.Policy.Host)
	}
}

func TestBuildDecisionInput_UsesFiftyFpsProfileBenchmarkClassForHeavyLivePaths(t *testing.T) {
	allowTranscode := true
	input := buildDecisionInput(
		PlaybackInfoRequest{
			RequestID:        "req-50fps",
			RequestedProfile: "quality",
			APIVersion:       "v3.1",
		},
		playback.MediaTruth{
			Container:   "mpegts",
			VideoCodec:  "mpeg2",
			AudioCodec:  "aac",
			BitrateKbps: 18000,
			Width:       3840,
			Height:      2160,
			FPS:         25,
			SignalFPS:   50,
		},
		capabilities.PlaybackCapabilities{
			CapabilitiesVersion: 1,
			AllowTranscode:      &allowTranscode,
		},
		config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
		config.PlaybackOperatorConfig{},
		playbackprofile.HostPressureAssessment{EffectiveBand: playbackprofile.HostPressureNormal},
		playbackprofile.HostRuntimeSnapshot{
			PerformanceClass: "medium",
			Benchmark: playbackprofile.HostBenchmarkSnapshot{
				Class: "strong",
				Profiles: []playbackprofile.HostProfileBenchmark{
					{ProfileID: playbackprofile.BenchmarkProfileVideoH2642160P, Class: "moderate"},
					{ProfileID: playbackprofile.BenchmarkProfileVideoH2642160P50, Class: "weak"},
				},
			},
		},
	)

	if input.Policy.Host.BenchmarkClass != "weak" {
		t.Fatalf("expected 2160p50-specific benchmark class in decision input, got %#v", input.Policy.Host)
	}
}

func TestService_ApplyPlaybackFeedbackPolicy_ClampsQualityToCompatibleAfterRepeatedDecodeFailures(t *testing.T) {
	allowTranscode := true
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/feedback-quality.ts"
	recordingID := domainrecordings.EncodeRecordingID(serviceRef)
	truth := playback.MediaTruth{
		Status:     playback.MediaStatusReady,
		Container:  "mpegts",
		VideoCodec: "mpeg2",
		AudioCodec: "ac3",
		Width:      1920,
		Height:     1080,
		FPS:        25,
	}
	registry := capreg.NewMemoryStore()
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			return truth, nil
		},
	}
	cfg := config.AppConfig{
		FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
		HLS:    config.HLSConfig{Root: "/tmp/hls"},
	}

	req := PlaybackInfoRequest{
		SubjectID:        recordingID,
		SubjectKind:      PlaybackSubjectRecording,
		APIVersion:       "v3.1",
		SchemaType:       "compact",
		RequestID:        "req-feedback-quality",
		RequestedProfile: "quality",
		Capabilities: &capabilities.PlaybackCapabilities{
			CapabilitiesVersion:  3,
			Containers:           []string{"hls", "mp4"},
			VideoCodecs:          []string{"h264"},
			AudioCodecs:          []string{"aac"},
			SupportsHLS:          true,
			SupportsHLSExplicit:  true,
			DeviceType:           "browser",
			RuntimeProbeUsed:     true,
			RuntimeProbeVersion:  2,
			ClientFamilyFallback: "chromium_hlsjs",
			ClientCapsSource:     "runtime",
			AllowTranscode:       &allowTranscode,
			DeviceContext: &capabilities.DeviceContext{
				Platform:  "mac",
				Model:     "macbookpro",
				OSName:    "macos",
				OSVersion: "15.4",
			},
		},
	}

	hostRuntime := playbackprofile.HostRuntimeSnapshot{PerformanceClass: "high"}
	svc := NewService(stubDeps{
		svc:         recSvc,
		capRegistry: registry,
		hostRuntime: hostRuntime,
		cfg:         cfg,
	})

	resolvedCaps := domainrecordings.ResolveCapabilities(context.Background(), req.PrincipalID, req.APIVersion, req.RequestedProfile, req.Headers, req.Capabilities)
	hostFingerprint := hostSnapshotForRequest(hostRuntime).Identity.Fingerprint()
	deviceFingerprint := deviceIdentityForRequest(req, resolvedCaps).Fingerprint()
	sourceFingerprint := svc.sourceSnapshotForRequest(context.Background(), serviceRef, req, truth).Fingerprint()
	now := time.Now().UTC()

	for idx, code := range []int{3, 3} {
		require.NoError(t, registry.RecordObservation(context.Background(), capreg.PlaybackObservation{
			ObservedAt:        now.Add(time.Duration(idx-2) * time.Minute),
			RequestID:         "fb-quality-" + string(rune('a'+idx)),
			ObservationKind:   "feedback",
			Outcome:           "failed",
			SourceRef:         serviceRef,
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       string(PlaybackSubjectRecording),
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
			FeedbackEvent:     "error",
			FeedbackCode:      code,
		}))
	}

	operatorPolicy, _ := svc.applyPlaybackFeedbackPolicy(context.Background(), serviceRef, req, truth, resolvedCaps, requestHostContext{
		Snapshot: hostSnapshotForRequest(hostRuntime),
	}, playbackprofile.HostPressureAssessment{EffectiveBand: playbackprofile.HostPressureNormal}, config.PlaybackOperatorConfig{})

	assert.Equal(t, string(playbackprofile.RungCompatibleVideoH264CRF23), operatorPolicy.MaxQualityRung)

	input := buildDecisionInput(
		req,
		truth,
		resolvedCaps,
		cfg,
		operatorPolicy,
		playbackprofile.HostPressureAssessment{EffectiveBand: playbackprofile.HostPressureNormal},
		hostRuntime,
	)
	_, dec, prob := decision.Decide(context.Background(), input, req.SchemaType)
	require.Nil(t, prob)
	require.NotNil(t, dec)
	assert.Equal(t, "quality", dec.Trace.RequestedIntent)
	assert.Equal(t, "compatible", dec.Trace.ResolvedIntent)
	assert.Equal(t, string(playbackprofile.RungCompatibleVideoH264CRF23), dec.Trace.MaxQualityRung)
	assert.True(t, dec.Trace.OverrideApplied)
}

func TestService_ApplyPlaybackFeedbackPolicy_LowBitrateConfidenceDelaysGenericFailureClamp(t *testing.T) {
	allowTranscode := true
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/feedback-low-confidence.ts"
	recordingID := domainrecordings.EncodeRecordingID(serviceRef)
	truth := playback.MediaTruth{
		Status:              playback.MediaStatusReady,
		Container:           "mpegts",
		VideoCodec:          "mpeg2",
		AudioCodec:          "ac3",
		BitrateKbps:         9000,
		BitrateConfidence:   "low",
		Width:               1920,
		Height:              1080,
		FPS:                 25,
		BitrateObservedKbps: 9000,
		BitrateSamples:      1,
	}
	registry := capreg.NewMemoryStore()
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			return truth, nil
		},
	}
	cfg := config.AppConfig{
		FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
		HLS:    config.HLSConfig{Root: "/tmp/hls"},
	}

	req := PlaybackInfoRequest{
		SubjectID:        recordingID,
		SubjectKind:      PlaybackSubjectRecording,
		APIVersion:       "v3.1",
		SchemaType:       "compact",
		RequestID:        "req-feedback-low-confidence",
		RequestedProfile: "quality",
		Capabilities: &capabilities.PlaybackCapabilities{
			CapabilitiesVersion:  3,
			Containers:           []string{"hls", "mp4"},
			VideoCodecs:          []string{"h264"},
			AudioCodecs:          []string{"aac"},
			SupportsHLS:          true,
			SupportsHLSExplicit:  true,
			DeviceType:           "browser",
			RuntimeProbeUsed:     true,
			RuntimeProbeVersion:  2,
			ClientFamilyFallback: "chromium_hlsjs",
			ClientCapsSource:     "runtime",
			AllowTranscode:       &allowTranscode,
			DeviceContext: &capabilities.DeviceContext{
				Platform:  "linux",
				Model:     "desktop",
				OSName:    "linux",
				OSVersion: "6.12",
			},
		},
	}

	hostRuntime := playbackprofile.HostRuntimeSnapshot{PerformanceClass: "high"}
	svc := NewService(stubDeps{
		svc:         recSvc,
		capRegistry: registry,
		hostRuntime: hostRuntime,
		cfg:         cfg,
	})

	resolvedCaps := domainrecordings.ResolveCapabilities(context.Background(), req.PrincipalID, req.APIVersion, req.RequestedProfile, req.Headers, req.Capabilities)
	hostFingerprint := hostSnapshotForRequest(hostRuntime).Identity.Fingerprint()
	deviceFingerprint := deviceIdentityForRequest(req, resolvedCaps).Fingerprint()
	sourceFingerprint := svc.sourceSnapshotForRequest(context.Background(), serviceRef, req, truth).Fingerprint()
	now := time.Now().UTC()

	for idx := range []int{0, 1} {
		require.NoError(t, registry.RecordObservation(context.Background(), capreg.PlaybackObservation{
			ObservedAt:        now.Add(time.Duration(idx-2) * time.Minute),
			RequestID:         "fb-low-confidence-" + string(rune('a'+idx)),
			ObservationKind:   "feedback",
			Outcome:           "failed",
			SourceRef:         serviceRef,
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       string(PlaybackSubjectRecording),
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
			FeedbackEvent:     "error",
			FeedbackCode:      1,
		}))
	}

	operatorPolicy, _ := svc.applyPlaybackFeedbackPolicy(context.Background(), serviceRef, req, truth, resolvedCaps, requestHostContext{
		Snapshot: hostSnapshotForRequest(hostRuntime),
	}, playbackprofile.HostPressureAssessment{EffectiveBand: playbackprofile.HostPressureNormal}, config.PlaybackOperatorConfig{})

	assert.Empty(t, operatorPolicy.MaxQualityRung)

	input := buildDecisionInput(
		req,
		truth,
		resolvedCaps,
		cfg,
		operatorPolicy,
		playbackprofile.HostPressureAssessment{EffectiveBand: playbackprofile.HostPressureNormal},
		hostRuntime,
	)
	_, dec, prob := decision.Decide(context.Background(), input, req.SchemaType)
	require.Nil(t, prob)
	require.NotNil(t, dec)
	assert.Equal(t, "quality", dec.Trace.RequestedIntent)
	assert.Equal(t, "quality", dec.Trace.ResolvedIntent)
	assert.Empty(t, dec.Trace.MaxQualityRung)
	assert.False(t, dec.Trace.OverrideApplied)
}

func TestService_ApplyPlaybackFeedbackPolicy_HealthyStartsDelayGenericFailureClamp(t *testing.T) {
	allowTranscode := true
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/feedback-healthy-streak.ts"
	recordingID := domainrecordings.EncodeRecordingID(serviceRef)
	truth := playback.MediaTruth{
		Status:            playback.MediaStatusReady,
		Container:         "mpegts",
		VideoCodec:        "mpeg2",
		AudioCodec:        "ac3",
		BitrateKbps:       9000,
		BitrateConfidence: "high",
		Width:             1920,
		Height:            1080,
		FPS:               25,
	}
	registry := capreg.NewMemoryStore()
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			return truth, nil
		},
	}
	cfg := config.AppConfig{
		FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
		HLS:    config.HLSConfig{Root: "/tmp/hls"},
	}

	req := PlaybackInfoRequest{
		SubjectID:        recordingID,
		SubjectKind:      PlaybackSubjectRecording,
		APIVersion:       "v3.1",
		SchemaType:       "compact",
		RequestID:        "req-feedback-healthy-streak",
		RequestedProfile: "quality",
		Capabilities: &capabilities.PlaybackCapabilities{
			CapabilitiesVersion:  3,
			Containers:           []string{"hls", "mp4"},
			VideoCodecs:          []string{"h264"},
			AudioCodecs:          []string{"aac"},
			SupportsHLS:          true,
			SupportsHLSExplicit:  true,
			DeviceType:           "browser",
			RuntimeProbeUsed:     true,
			RuntimeProbeVersion:  2,
			ClientFamilyFallback: "chromium_hlsjs",
			ClientCapsSource:     "runtime",
			AllowTranscode:       &allowTranscode,
			DeviceContext: &capabilities.DeviceContext{
				Platform:  "windows",
				Model:     "desktop",
				OSName:    "windows",
				OSVersion: "11",
			},
		},
	}

	hostRuntime := playbackprofile.HostRuntimeSnapshot{PerformanceClass: "high"}
	svc := NewService(stubDeps{
		svc:         recSvc,
		capRegistry: registry,
		hostRuntime: hostRuntime,
		cfg:         cfg,
	})

	resolvedCaps := domainrecordings.ResolveCapabilities(context.Background(), req.PrincipalID, req.APIVersion, req.RequestedProfile, req.Headers, req.Capabilities)
	hostFingerprint := hostSnapshotForRequest(hostRuntime).Identity.Fingerprint()
	deviceFingerprint := deviceIdentityForRequest(req, resolvedCaps).Fingerprint()
	sourceFingerprint := svc.sourceSnapshotForRequest(context.Background(), serviceRef, req, truth).Fingerprint()
	now := time.Now().UTC()

	observations := []capreg.PlaybackObservation{
		{
			ObservedAt:        now.Add(-4 * time.Minute),
			RequestID:         "fb-healthy-a",
			ObservationKind:   "feedback",
			Outcome:           "started",
			SourceRef:         serviceRef,
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       string(PlaybackSubjectRecording),
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
			FeedbackEvent:     "info",
			FeedbackCode:      200,
		},
		{
			ObservedAt:        now.Add(-3 * time.Minute),
			RequestID:         "fb-healthy-b",
			ObservationKind:   "feedback",
			Outcome:           "started",
			SourceRef:         serviceRef,
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       string(PlaybackSubjectRecording),
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
			FeedbackEvent:     "info",
			FeedbackCode:      200,
		},
		{
			ObservedAt:        now.Add(-2 * time.Minute),
			RequestID:         "fb-healthy-c",
			ObservationKind:   "feedback",
			Outcome:           "failed",
			SourceRef:         serviceRef,
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       string(PlaybackSubjectRecording),
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
			FeedbackEvent:     "error",
			FeedbackCode:      1,
		},
		{
			ObservedAt:        now.Add(-1 * time.Minute),
			RequestID:         "fb-healthy-d",
			ObservationKind:   "feedback",
			Outcome:           "failed",
			SourceRef:         serviceRef,
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       string(PlaybackSubjectRecording),
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
			FeedbackEvent:     "error",
			FeedbackCode:      1,
		},
	}
	for _, observation := range observations {
		require.NoError(t, registry.RecordObservation(context.Background(), observation))
	}

	operatorPolicy, _ := svc.applyPlaybackFeedbackPolicy(context.Background(), serviceRef, req, truth, resolvedCaps, requestHostContext{
		Snapshot: hostSnapshotForRequest(hostRuntime),
	}, playbackprofile.HostPressureAssessment{EffectiveBand: playbackprofile.HostPressureNormal}, config.PlaybackOperatorConfig{})

	assert.Empty(t, operatorPolicy.MaxQualityRung)

	input := buildDecisionInput(
		req,
		truth,
		resolvedCaps,
		cfg,
		operatorPolicy,
		playbackprofile.HostPressureAssessment{EffectiveBand: playbackprofile.HostPressureNormal},
		hostRuntime,
	)
	_, dec, prob := decision.Decide(context.Background(), input, req.SchemaType)
	require.Nil(t, prob)
	require.NotNil(t, dec)
	assert.Equal(t, "quality", dec.Trace.RequestedIntent)
	assert.Equal(t, "quality", dec.Trace.ResolvedIntent)
	assert.Empty(t, dec.Trace.MaxQualityRung)
	assert.False(t, dec.Trace.OverrideApplied)
}

func TestService_ApplyPlaybackFeedbackPolicy_ClampsToCompatibleAfterRepeatedBufferWarnings(t *testing.T) {
	allowTranscode := true
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/feedback-buffer-warning.ts"
	recordingID := domainrecordings.EncodeRecordingID(serviceRef)
	truth := playback.MediaTruth{
		Status:            playback.MediaStatusReady,
		Container:         "mpegts",
		VideoCodec:        "mpeg2",
		AudioCodec:        "ac3",
		BitrateKbps:       9000,
		BitrateConfidence: "high",
		Width:             1920,
		Height:            1080,
		FPS:               25,
	}
	registry := capreg.NewMemoryStore()
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			return truth, nil
		},
	}
	cfg := config.AppConfig{
		FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
		HLS:    config.HLSConfig{Root: "/tmp/hls"},
	}

	req := PlaybackInfoRequest{
		SubjectID:        recordingID,
		SubjectKind:      PlaybackSubjectRecording,
		APIVersion:       "v3.1",
		SchemaType:       "compact",
		RequestID:        "req-feedback-buffer-warning",
		RequestedProfile: "quality",
		Capabilities: &capabilities.PlaybackCapabilities{
			CapabilitiesVersion:  3,
			Containers:           []string{"hls", "mp4"},
			VideoCodecs:          []string{"h264"},
			AudioCodecs:          []string{"aac"},
			SupportsHLS:          true,
			SupportsHLSExplicit:  true,
			DeviceType:           "browser",
			RuntimeProbeUsed:     true,
			RuntimeProbeVersion:  2,
			ClientFamilyFallback: "chromium_hlsjs",
			ClientCapsSource:     "runtime",
			AllowTranscode:       &allowTranscode,
			DeviceContext: &capabilities.DeviceContext{
				Platform:  "linux",
				Model:     "desktop",
				OSName:    "linux",
				OSVersion: "6.12",
			},
		},
	}

	hostRuntime := playbackprofile.HostRuntimeSnapshot{PerformanceClass: "high"}
	svc := NewService(stubDeps{
		svc:         recSvc,
		capRegistry: registry,
		hostRuntime: hostRuntime,
		cfg:         cfg,
	})

	resolvedCaps := domainrecordings.ResolveCapabilities(context.Background(), req.PrincipalID, req.APIVersion, req.RequestedProfile, req.Headers, req.Capabilities)
	hostFingerprint := hostSnapshotForRequest(hostRuntime).Identity.Fingerprint()
	deviceFingerprint := deviceIdentityForRequest(req, resolvedCaps).Fingerprint()
	sourceFingerprint := svc.sourceSnapshotForRequest(context.Background(), serviceRef, req, truth).Fingerprint()
	now := time.Now().UTC()

	for idx, code := range []int{102, 101, 101} {
		require.NoError(t, registry.RecordObservation(context.Background(), capreg.PlaybackObservation{
			ObservedAt:        now.Add(time.Duration(idx-3) * time.Minute),
			RequestID:         "fb-buffer-warning-" + string(rune('a'+idx)),
			ObservationKind:   "feedback",
			Outcome:           "warning",
			SourceRef:         serviceRef,
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       string(PlaybackSubjectRecording),
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
			FeedbackEvent:     "warning",
			FeedbackCode:      code,
		}))
	}

	operatorPolicy, _ := svc.applyPlaybackFeedbackPolicy(context.Background(), serviceRef, req, truth, resolvedCaps, requestHostContext{
		Snapshot: hostSnapshotForRequest(hostRuntime),
	}, playbackprofile.HostPressureAssessment{EffectiveBand: playbackprofile.HostPressureNormal}, config.PlaybackOperatorConfig{})

	assert.Equal(t, string(playbackprofile.RungCompatibleVideoH264CRF23), operatorPolicy.MaxQualityRung)

	input := buildDecisionInput(
		req,
		truth,
		resolvedCaps,
		cfg,
		operatorPolicy,
		playbackprofile.HostPressureAssessment{EffectiveBand: playbackprofile.HostPressureNormal},
		hostRuntime,
	)
	_, dec, prob := decision.Decide(context.Background(), input, req.SchemaType)
	require.Nil(t, prob)
	require.NotNil(t, dec)
	assert.Equal(t, "quality", dec.Trace.RequestedIntent)
	assert.Equal(t, "compatible", dec.Trace.ResolvedIntent)
	assert.Equal(t, string(playbackprofile.RungCompatibleVideoH264CRF23), dec.Trace.MaxQualityRung)
	assert.True(t, dec.Trace.OverrideApplied)
}

func TestService_ApplyPlaybackFeedbackPolicy_ClampsToCompatibleAfterRepeatedDecodeWarnings(t *testing.T) {
	allowTranscode := true
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/feedback-decode-warning.ts"
	recordingID := domainrecordings.EncodeRecordingID(serviceRef)
	truth := playback.MediaTruth{
		Status:            playback.MediaStatusReady,
		Container:         "mpegts",
		VideoCodec:        "mpeg2",
		AudioCodec:        "ac3",
		BitrateKbps:       9000,
		BitrateConfidence: "high",
		Width:             1920,
		Height:            1080,
		FPS:               25,
	}
	registry := capreg.NewMemoryStore()
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			return truth, nil
		},
	}
	cfg := config.AppConfig{
		FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
		HLS:    config.HLSConfig{Root: "/tmp/hls"},
	}

	req := PlaybackInfoRequest{
		SubjectID:        recordingID,
		SubjectKind:      PlaybackSubjectRecording,
		APIVersion:       "v3.1",
		SchemaType:       "compact",
		RequestID:        "req-feedback-decode-warning",
		RequestedProfile: "quality",
		Capabilities: &capabilities.PlaybackCapabilities{
			CapabilitiesVersion:  3,
			Containers:           []string{"hls", "mp4"},
			VideoCodecs:          []string{"h264"},
			AudioCodecs:          []string{"aac"},
			SupportsHLS:          true,
			SupportsHLSExplicit:  true,
			DeviceType:           "browser",
			RuntimeProbeUsed:     true,
			RuntimeProbeVersion:  2,
			ClientFamilyFallback: "chromium_hlsjs",
			ClientCapsSource:     "runtime",
			AllowTranscode:       &allowTranscode,
			DeviceContext: &capabilities.DeviceContext{
				Platform:  "linux",
				Model:     "desktop",
				OSName:    "linux",
				OSVersion: "6.12",
			},
		},
	}

	hostRuntime := playbackprofile.HostRuntimeSnapshot{PerformanceClass: "high"}
	svc := NewService(stubDeps{
		svc:         recSvc,
		capRegistry: registry,
		hostRuntime: hostRuntime,
		cfg:         cfg,
	})

	resolvedCaps := domainrecordings.ResolveCapabilities(context.Background(), req.PrincipalID, req.APIVersion, req.RequestedProfile, req.Headers, req.Capabilities)
	hostFingerprint := hostSnapshotForRequest(hostRuntime).Identity.Fingerprint()
	deviceFingerprint := deviceIdentityForRequest(req, resolvedCaps).Fingerprint()
	sourceFingerprint := svc.sourceSnapshotForRequest(context.Background(), serviceRef, req, truth).Fingerprint()
	now := time.Now().UTC()

	for idx := range []int{0, 1} {
		require.NoError(t, registry.RecordObservation(context.Background(), capreg.PlaybackObservation{
			ObservedAt:        now.Add(time.Duration(idx-2) * time.Minute),
			RequestID:         "fb-decode-warning-" + string(rune('a'+idx)),
			ObservationKind:   "feedback",
			Outcome:           "warning",
			SourceRef:         serviceRef,
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       string(PlaybackSubjectRecording),
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
			FeedbackEvent:     "warning",
			FeedbackCode:      103,
		}))
	}

	operatorPolicy, _ := svc.applyPlaybackFeedbackPolicy(context.Background(), serviceRef, req, truth, resolvedCaps, requestHostContext{
		Snapshot: hostSnapshotForRequest(hostRuntime),
	}, playbackprofile.HostPressureAssessment{EffectiveBand: playbackprofile.HostPressureNormal}, config.PlaybackOperatorConfig{})

	assert.Equal(t, string(playbackprofile.RungCompatibleVideoH264CRF23), operatorPolicy.MaxQualityRung)

	input := buildDecisionInput(
		req,
		truth,
		resolvedCaps,
		cfg,
		operatorPolicy,
		playbackprofile.HostPressureAssessment{EffectiveBand: playbackprofile.HostPressureNormal},
		hostRuntime,
	)
	_, dec, prob := decision.Decide(context.Background(), input, req.SchemaType)
	require.Nil(t, prob)
	require.NotNil(t, dec)
	assert.Equal(t, "quality", dec.Trace.RequestedIntent)
	assert.Equal(t, "compatible", dec.Trace.ResolvedIntent)
	assert.Equal(t, string(playbackprofile.RungCompatibleVideoH264CRF23), dec.Trace.MaxQualityRung)
	assert.True(t, dec.Trace.OverrideApplied)
}

func TestService_ApplyPlaybackFeedbackPolicy_ClampsToCompatibleAfterRepeatedNetworkWarnings(t *testing.T) {
	allowTranscode := true
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/feedback-network-warning.ts"
	recordingID := domainrecordings.EncodeRecordingID(serviceRef)
	truth := playback.MediaTruth{
		Status:            playback.MediaStatusReady,
		Container:         "mpegts",
		VideoCodec:        "mpeg2",
		AudioCodec:        "ac3",
		BitrateKbps:       9000,
		BitrateConfidence: "high",
		Width:             1920,
		Height:            1080,
		FPS:               25,
	}
	registry := capreg.NewMemoryStore()
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			return truth, nil
		},
	}
	cfg := config.AppConfig{
		FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
		HLS:    config.HLSConfig{Root: "/tmp/hls"},
	}

	req := PlaybackInfoRequest{
		SubjectID:        recordingID,
		SubjectKind:      PlaybackSubjectRecording,
		APIVersion:       "v3.1",
		SchemaType:       "compact",
		RequestID:        "req-feedback-network-warning",
		RequestedProfile: "quality",
		Capabilities: &capabilities.PlaybackCapabilities{
			CapabilitiesVersion:  3,
			Containers:           []string{"hls", "mp4"},
			VideoCodecs:          []string{"h264"},
			AudioCodecs:          []string{"aac"},
			SupportsHLS:          true,
			SupportsHLSExplicit:  true,
			DeviceType:           "browser",
			RuntimeProbeUsed:     true,
			RuntimeProbeVersion:  2,
			ClientFamilyFallback: "chromium_hlsjs",
			ClientCapsSource:     "runtime",
			AllowTranscode:       &allowTranscode,
			DeviceContext: &capabilities.DeviceContext{
				Platform:  "linux",
				Model:     "desktop",
				OSName:    "linux",
				OSVersion: "6.12",
			},
		},
	}

	hostRuntime := playbackprofile.HostRuntimeSnapshot{PerformanceClass: "high"}
	svc := NewService(stubDeps{
		svc:         recSvc,
		capRegistry: registry,
		hostRuntime: hostRuntime,
		cfg:         cfg,
	})

	resolvedCaps := domainrecordings.ResolveCapabilities(context.Background(), req.PrincipalID, req.APIVersion, req.RequestedProfile, req.Headers, req.Capabilities)
	hostFingerprint := hostSnapshotForRequest(hostRuntime).Identity.Fingerprint()
	deviceFingerprint := deviceIdentityForRequest(req, resolvedCaps).Fingerprint()
	sourceFingerprint := svc.sourceSnapshotForRequest(context.Background(), serviceRef, req, truth).Fingerprint()
	now := time.Now().UTC()

	for idx := range []int{0, 1, 2} {
		require.NoError(t, registry.RecordObservation(context.Background(), capreg.PlaybackObservation{
			ObservedAt:        now.Add(time.Duration(idx-3) * time.Minute),
			RequestID:         "fb-network-warning-" + string(rune('a'+idx)),
			ObservationKind:   "feedback",
			Outcome:           "warning",
			SourceRef:         serviceRef,
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       string(PlaybackSubjectRecording),
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
			FeedbackEvent:     "warning",
			FeedbackCode:      104,
		}))
	}

	operatorPolicy, _ := svc.applyPlaybackFeedbackPolicy(context.Background(), serviceRef, req, truth, resolvedCaps, requestHostContext{
		Snapshot: hostSnapshotForRequest(hostRuntime),
	}, playbackprofile.HostPressureAssessment{EffectiveBand: playbackprofile.HostPressureNormal}, config.PlaybackOperatorConfig{})

	assert.Equal(t, string(playbackprofile.RungCompatibleVideoH264CRF23), operatorPolicy.MaxQualityRung)

	input := buildDecisionInput(
		req,
		truth,
		resolvedCaps,
		cfg,
		operatorPolicy,
		playbackprofile.HostPressureAssessment{EffectiveBand: playbackprofile.HostPressureNormal},
		hostRuntime,
	)
	_, dec, prob := decision.Decide(context.Background(), input, req.SchemaType)
	require.Nil(t, prob)
	require.NotNil(t, dec)
	assert.Equal(t, "quality", dec.Trace.RequestedIntent)
	assert.Equal(t, "compatible", dec.Trace.ResolvedIntent)
	assert.Equal(t, string(playbackprofile.RungCompatibleVideoH264CRF23), dec.Trace.MaxQualityRung)
	assert.True(t, dec.Trace.OverrideApplied)
}

func TestService_ApplyPlaybackFeedbackPolicy_DelaysSoftWarningClampAfterRecoveryStart(t *testing.T) {
	allowTranscode := true
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/feedback-recovered-warning.ts"
	recordingID := domainrecordings.EncodeRecordingID(serviceRef)
	truth := playback.MediaTruth{
		Status:            playback.MediaStatusReady,
		Container:         "mpegts",
		VideoCodec:        "mpeg2",
		AudioCodec:        "ac3",
		BitrateKbps:       9000,
		BitrateConfidence: "high",
		Width:             1920,
		Height:            1080,
		FPS:               25,
	}
	registry := capreg.NewMemoryStore()
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			return truth, nil
		},
	}
	cfg := config.AppConfig{
		FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
		HLS:    config.HLSConfig{Root: "/tmp/hls"},
	}

	req := PlaybackInfoRequest{
		SubjectID:        recordingID,
		SubjectKind:      PlaybackSubjectRecording,
		APIVersion:       "v3.1",
		SchemaType:       "compact",
		RequestID:        "req-feedback-recovered-warning",
		RequestedProfile: "quality",
		Capabilities: &capabilities.PlaybackCapabilities{
			CapabilitiesVersion:  3,
			Containers:           []string{"hls", "mp4"},
			VideoCodecs:          []string{"h264"},
			AudioCodecs:          []string{"aac"},
			SupportsHLS:          true,
			SupportsHLSExplicit:  true,
			DeviceType:           "browser",
			RuntimeProbeUsed:     true,
			RuntimeProbeVersion:  2,
			ClientFamilyFallback: "chromium_hlsjs",
			ClientCapsSource:     "runtime",
			AllowTranscode:       &allowTranscode,
			DeviceContext: &capabilities.DeviceContext{
				Platform:  "linux",
				Model:     "desktop",
				OSName:    "linux",
				OSVersion: "6.12",
			},
		},
	}

	hostRuntime := playbackprofile.HostRuntimeSnapshot{PerformanceClass: "high"}
	svc := NewService(stubDeps{
		svc:         recSvc,
		capRegistry: registry,
		hostRuntime: hostRuntime,
		cfg:         cfg,
	})

	resolvedCaps := domainrecordings.ResolveCapabilities(context.Background(), req.PrincipalID, req.APIVersion, req.RequestedProfile, req.Headers, req.Capabilities)
	hostFingerprint := hostSnapshotForRequest(hostRuntime).Identity.Fingerprint()
	deviceFingerprint := deviceIdentityForRequest(req, resolvedCaps).Fingerprint()
	sourceFingerprint := svc.sourceSnapshotForRequest(context.Background(), serviceRef, req, truth).Fingerprint()
	now := time.Now().UTC()

	require.NoError(t, registry.RecordObservation(context.Background(), capreg.PlaybackObservation{
		ObservedAt:        now.Add(-3 * time.Minute),
		RequestID:         "fb-warning-recent",
		ObservationKind:   "feedback",
		Outcome:           "warning",
		SourceRef:         serviceRef,
		SourceFingerprint: sourceFingerprint,
		SubjectKind:       string(PlaybackSubjectRecording),
		HostFingerprint:   hostFingerprint,
		DeviceFingerprint: deviceFingerprint,
		FeedbackEvent:     "warning",
		FeedbackCode:      101,
	}))
	require.NoError(t, registry.RecordObservation(context.Background(), capreg.PlaybackObservation{
		ObservedAt:        now.Add(-4 * time.Minute),
		RequestID:         "fb-recovered-before-warning",
		ObservationKind:   "feedback",
		Outcome:           "started",
		SourceRef:         serviceRef,
		SourceFingerprint: sourceFingerprint,
		SubjectKind:       string(PlaybackSubjectRecording),
		HostFingerprint:   hostFingerprint,
		DeviceFingerprint: deviceFingerprint,
		FeedbackEvent:     "info",
		FeedbackCode:      211,
	}))

	operatorPolicy, _ := svc.applyPlaybackFeedbackPolicy(context.Background(), serviceRef, req, truth, resolvedCaps, requestHostContext{
		Snapshot: hostSnapshotForRequest(hostRuntime),
	}, playbackprofile.HostPressureAssessment{EffectiveBand: playbackprofile.HostPressureNormal}, config.PlaybackOperatorConfig{})

	assert.Empty(t, operatorPolicy.MaxQualityRung)

	input := buildDecisionInput(
		req,
		truth,
		resolvedCaps,
		cfg,
		operatorPolicy,
		playbackprofile.HostPressureAssessment{EffectiveBand: playbackprofile.HostPressureNormal},
		hostRuntime,
	)
	_, dec, prob := decision.Decide(context.Background(), input, req.SchemaType)
	require.Nil(t, prob)
	require.NotNil(t, dec)
	assert.Equal(t, "quality", dec.Trace.RequestedIntent)
	assert.Equal(t, "quality", dec.Trace.ResolvedIntent)
	assert.Empty(t, dec.Trace.MaxQualityRung)
	assert.False(t, dec.Trace.OverrideApplied)
}

func TestService_ApplyPlaybackFeedbackPolicy_MatchingRecoveryTrustDelaysSoftWarningClampFurther(t *testing.T) {
	allowTranscode := true
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/feedback-recovered-warning-trust.ts"
	recordingID := domainrecordings.EncodeRecordingID(serviceRef)
	truth := playback.MediaTruth{
		Status:            playback.MediaStatusReady,
		Container:         "mpegts",
		VideoCodec:        "mpeg2",
		AudioCodec:        "ac3",
		BitrateKbps:       9000,
		BitrateConfidence: "high",
		Width:             1920,
		Height:            1080,
		FPS:               25,
	}
	registry := capreg.NewMemoryStore()
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			return truth, nil
		},
	}
	cfg := config.AppConfig{
		FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
		HLS:    config.HLSConfig{Root: "/tmp/hls"},
	}

	req := PlaybackInfoRequest{
		SubjectID:        recordingID,
		SubjectKind:      PlaybackSubjectRecording,
		APIVersion:       "v3.1",
		SchemaType:       "compact",
		RequestID:        "req-feedback-recovered-warning-trust",
		RequestedProfile: "quality",
		Capabilities: &capabilities.PlaybackCapabilities{
			CapabilitiesVersion:  3,
			Containers:           []string{"hls", "mp4"},
			VideoCodecs:          []string{"h264"},
			AudioCodecs:          []string{"aac"},
			SupportsHLS:          true,
			SupportsHLSExplicit:  true,
			DeviceType:           "browser",
			RuntimeProbeUsed:     true,
			RuntimeProbeVersion:  2,
			ClientFamilyFallback: "chromium_hlsjs",
			ClientCapsSource:     "runtime",
			AllowTranscode:       &allowTranscode,
			DeviceContext: &capabilities.DeviceContext{
				Platform:  "linux",
				Model:     "desktop",
				OSName:    "linux",
				OSVersion: "6.12",
			},
		},
	}

	hostRuntime := playbackprofile.HostRuntimeSnapshot{PerformanceClass: "high"}
	svc := NewService(stubDeps{
		svc:         recSvc,
		capRegistry: registry,
		hostRuntime: hostRuntime,
		cfg:         cfg,
	})

	resolvedCaps := domainrecordings.ResolveCapabilities(context.Background(), req.PrincipalID, req.APIVersion, req.RequestedProfile, req.Headers, req.Capabilities)
	hostFingerprint := hostSnapshotForRequest(hostRuntime).Identity.Fingerprint()
	deviceFingerprint := deviceIdentityForRequest(req, resolvedCaps).Fingerprint()
	sourceFingerprint := svc.sourceSnapshotForRequest(context.Background(), serviceRef, req, truth).Fingerprint()
	now := time.Now().UTC()

	for idx := range []int{0, 1, 2, 3} {
		require.NoError(t, registry.RecordObservation(context.Background(), capreg.PlaybackObservation{
			ObservedAt:        now.Add(time.Duration(idx-4) * time.Minute),
			RequestID:         "fb-warning-trust-" + string(rune('a'+idx)),
			ObservationKind:   "feedback",
			Outcome:           "warning",
			SourceRef:         serviceRef,
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       string(PlaybackSubjectRecording),
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
			FeedbackEvent:     "warning",
			FeedbackCode:      101,
		}))
	}
	for idx := range []int{0, 1} {
		require.NoError(t, registry.RecordObservation(context.Background(), capreg.PlaybackObservation{
			ObservedAt:        now.Add(time.Duration(-5-idx) * time.Minute),
			RequestID:         "fb-recovered-trust-" + string(rune('a'+idx)),
			ObservationKind:   "feedback",
			Outcome:           "started",
			SourceRef:         serviceRef,
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       string(PlaybackSubjectRecording),
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
			FeedbackEvent:     "info",
			FeedbackCode:      211,
		}))
	}

	operatorPolicy, _ := svc.applyPlaybackFeedbackPolicy(context.Background(), serviceRef, req, truth, resolvedCaps, requestHostContext{
		Snapshot: hostSnapshotForRequest(hostRuntime),
	}, playbackprofile.HostPressureAssessment{EffectiveBand: playbackprofile.HostPressureNormal}, config.PlaybackOperatorConfig{})

	assert.Empty(t, operatorPolicy.MaxQualityRung)

	input := buildDecisionInput(
		req,
		truth,
		resolvedCaps,
		cfg,
		operatorPolicy,
		playbackprofile.HostPressureAssessment{EffectiveBand: playbackprofile.HostPressureNormal},
		hostRuntime,
	)
	_, dec, prob := decision.Decide(context.Background(), input, req.SchemaType)
	require.Nil(t, prob)
	require.NotNil(t, dec)
	assert.Equal(t, "quality", dec.Trace.RequestedIntent)
	assert.Equal(t, "quality", dec.Trace.ResolvedIntent)
	assert.Empty(t, dec.Trace.MaxQualityRung)
	assert.False(t, dec.Trace.OverrideApplied)
}

func TestService_ResolvePlaybackInfo_ClampsToRepairAfterRepeatedStallFailures(t *testing.T) {
	allowTranscode := true
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/feedback-repair.ts"
	recordingID := domainrecordings.EncodeRecordingID(serviceRef)
	truth := playback.MediaTruth{
		Status:     playback.MediaStatusReady,
		Container:  "mpegts",
		VideoCodec: "mpeg2",
		AudioCodec: "ac3",
		Width:      1920,
		Height:     1080,
		FPS:        25,
	}
	registry := capreg.NewMemoryStore()
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			return truth, nil
		},
	}

	req := PlaybackInfoRequest{
		SubjectID:        recordingID,
		SubjectKind:      PlaybackSubjectRecording,
		APIVersion:       "v3.1",
		SchemaType:       "compact",
		RequestID:        "req-feedback-repair",
		RequestedProfile: "compatible",
		Capabilities: &capabilities.PlaybackCapabilities{
			CapabilitiesVersion:  3,
			Containers:           []string{"hls", "mp4"},
			VideoCodecs:          []string{"h264"},
			AudioCodecs:          []string{"aac"},
			SupportsHLS:          true,
			SupportsHLSExplicit:  true,
			DeviceType:           "browser",
			RuntimeProbeUsed:     true,
			RuntimeProbeVersion:  2,
			ClientFamilyFallback: "chromium_hlsjs",
			ClientCapsSource:     "runtime",
			AllowTranscode:       &allowTranscode,
			DeviceContext: &capabilities.DeviceContext{
				Platform:  "windows",
				Model:     "desktop",
				OSName:    "windows",
				OSVersion: "11",
			},
		},
	}

	hostRuntime := playbackprofile.HostRuntimeSnapshot{PerformanceClass: "high"}
	svc := NewService(stubDeps{
		svc:         recSvc,
		capRegistry: registry,
		hostRuntime: hostRuntime,
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	})

	resolvedCaps := domainrecordings.ResolveCapabilities(context.Background(), req.PrincipalID, req.APIVersion, req.RequestedProfile, req.Headers, req.Capabilities)
	hostFingerprint := hostSnapshotForRequest(hostRuntime).Identity.Fingerprint()
	deviceFingerprint := deviceIdentityForRequest(req, resolvedCaps).Fingerprint()
	sourceFingerprint := svc.sourceSnapshotForRequest(context.Background(), serviceRef, req, truth).Fingerprint()
	now := time.Now().UTC()

	for idx := range []int{0, 1} {
		require.NoError(t, registry.RecordObservation(context.Background(), capreg.PlaybackObservation{
			ObservedAt:        now.Add(time.Duration(idx-2) * time.Minute),
			RequestID:         "fb-repair-" + string(rune('a'+idx)),
			ObservationKind:   "feedback",
			Outcome:           "failed",
			SourceRef:         serviceRef,
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       string(PlaybackSubjectRecording),
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
			FeedbackEvent:     "error",
			FeedbackCode:      4,
		}))
	}

	res, err := svc.ResolvePlaybackInfo(context.Background(), req)
	require.Nil(t, err)
	require.NotNil(t, res.Decision)
	assert.Equal(t, "compatible", res.Decision.Trace.RequestedIntent)
	assert.Equal(t, "repair", res.Decision.Trace.ResolvedIntent)
	assert.Equal(t, string(playbackprofile.RungRepairH264AAC), res.Decision.Trace.MaxQualityRung)
	assert.True(t, res.Decision.Trace.OverrideApplied)
}

func TestService_ResolvePlaybackInfo_SingleDecodeFailureTriggersConfidenceClamp(t *testing.T) {
	allowTranscode := true
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/feedback-single-decode-failure.ts"
	recordingID := domainrecordings.EncodeRecordingID(serviceRef)
	truth := playback.MediaTruth{
		Status:            playback.MediaStatusReady,
		Container:         "mpegts",
		VideoCodec:        "mpeg2",
		AudioCodec:        "ac3",
		BitrateKbps:       9000,
		BitrateConfidence: "high",
		Width:             1920,
		Height:            1080,
		FPS:               25,
	}
	registry := capreg.NewMemoryStore()
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			return truth, nil
		},
	}

	req := PlaybackInfoRequest{
		SubjectID:        recordingID,
		SubjectKind:      PlaybackSubjectRecording,
		APIVersion:       "v3.1",
		SchemaType:       "compact",
		RequestID:        "req-feedback-single-decode-failure",
		RequestedProfile: "quality",
		Capabilities: &capabilities.PlaybackCapabilities{
			CapabilitiesVersion:  3,
			Containers:           []string{"hls", "mp4"},
			VideoCodecs:          []string{"h264"},
			AudioCodecs:          []string{"aac"},
			SupportsHLS:          true,
			SupportsHLSExplicit:  true,
			DeviceType:           "browser",
			RuntimeProbeUsed:     true,
			RuntimeProbeVersion:  2,
			ClientFamilyFallback: "chromium_hlsjs",
			ClientCapsSource:     "runtime",
			AllowTranscode:       &allowTranscode,
			DeviceContext: &capabilities.DeviceContext{
				Platform:  "linux",
				Model:     "desktop",
				OSName:    "linux",
				OSVersion: "6.12",
			},
		},
	}

	hostRuntime := playbackprofile.HostRuntimeSnapshot{PerformanceClass: "high"}
	svc := NewService(stubDeps{
		svc:         recSvc,
		capRegistry: registry,
		hostRuntime: hostRuntime,
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	})

	resolvedCaps := domainrecordings.ResolveCapabilities(context.Background(), req.PrincipalID, req.APIVersion, req.RequestedProfile, req.Headers, req.Capabilities)
	hostFingerprint := hostSnapshotForRequest(hostRuntime).Identity.Fingerprint()
	deviceFingerprint := deviceIdentityForRequest(req, resolvedCaps).Fingerprint()
	sourceFingerprint := svc.sourceSnapshotForRequest(context.Background(), serviceRef, req, truth).Fingerprint()

	require.NoError(t, registry.RecordObservation(context.Background(), capreg.PlaybackObservation{
		ObservedAt:        time.Now().UTC().Add(-1 * time.Minute),
		RequestID:         "fb-single-decode-failure",
		ObservationKind:   "feedback",
		Outcome:           "failed",
		SourceRef:         serviceRef,
		SourceFingerprint: sourceFingerprint,
		SubjectKind:       string(PlaybackSubjectRecording),
		HostFingerprint:   hostFingerprint,
		DeviceFingerprint: deviceFingerprint,
		FeedbackEvent:     "error",
		FeedbackCode:      3,
	}))

	res, err := svc.ResolvePlaybackInfo(context.Background(), req)
	require.Nil(t, err)
	require.NotNil(t, res.Decision)
	assert.Equal(t, "degrade", res.RuntimePolicyAction)
	assert.Equal(t, "degraded", res.RuntimePolicyPhase)
	assert.Equal(t, string(playbackprofile.RungCompatibleVideoH264CRF23), res.Decision.Trace.MaxQualityRung)
	assert.True(t, res.Decision.Trace.OverrideApplied)
}

func TestService_ResolvePlaybackInfo_PersistedCooldownCarriesForwardCompatibleClamp(t *testing.T) {
	allowTranscode := true
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/persisted-cooldown.ts"
	recordingID := domainrecordings.EncodeRecordingID(serviceRef)
	truth := playback.MediaTruth{
		Status:            playback.MediaStatusReady,
		Container:         "mpegts",
		VideoCodec:        "mpeg2",
		AudioCodec:        "ac3",
		BitrateKbps:       9000,
		BitrateConfidence: "high",
		Width:             1920,
		Height:            1080,
		FPS:               25,
	}
	registry := capreg.NewMemoryStore()
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			return truth, nil
		},
	}

	req := PlaybackInfoRequest{
		SubjectID:        recordingID,
		SubjectKind:      PlaybackSubjectRecording,
		APIVersion:       "v3.1",
		SchemaType:       "compact",
		RequestID:        "req-persisted-cooldown",
		RequestedProfile: "quality",
		Capabilities: &capabilities.PlaybackCapabilities{
			CapabilitiesVersion:  3,
			Containers:           []string{"hls", "mp4"},
			VideoCodecs:          []string{"h264"},
			AudioCodecs:          []string{"aac"},
			SupportsHLS:          true,
			SupportsHLSExplicit:  true,
			DeviceType:           "browser",
			RuntimeProbeUsed:     true,
			RuntimeProbeVersion:  2,
			ClientFamilyFallback: "chromium_hlsjs",
			ClientCapsSource:     "runtime",
			AllowTranscode:       &allowTranscode,
			DeviceContext: &capabilities.DeviceContext{
				Platform:  "linux",
				Model:     "desktop",
				OSName:    "linux",
				OSVersion: "6.12",
			},
		},
	}

	hostRuntime := playbackprofile.HostRuntimeSnapshot{PerformanceClass: "high"}
	svc := NewService(stubDeps{
		svc:         recSvc,
		capRegistry: registry,
		hostRuntime: hostRuntime,
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	})

	resolvedCaps := domainrecordings.ResolveCapabilities(context.Background(), req.PrincipalID, req.APIVersion, req.RequestedProfile, req.Headers, req.Capabilities)
	hostFingerprint := hostSnapshotForRequest(hostRuntime).Identity.Fingerprint()
	deviceFingerprint := deviceIdentityForRequest(req, resolvedCaps).Fingerprint()
	sourceFingerprint := svc.sourceSnapshotForRequest(context.Background(), serviceRef, req, truth).Fingerprint()
	now := time.Now().UTC()

	require.NoError(t, registry.RecordObservation(context.Background(), capreg.PlaybackObservation{
		ObservedAt:        now.Add(-20 * time.Second),
		RequestID:         "fb-persisted-cooldown-started",
		ObservationKind:   "feedback",
		Outcome:           "started",
		SourceRef:         serviceRef,
		SourceFingerprint: sourceFingerprint,
		SubjectKind:       string(PlaybackSubjectRecording),
		HostFingerprint:   hostFingerprint,
		DeviceFingerprint: deviceFingerprint,
		FeedbackEvent:     "info",
		FeedbackCode:      200,
	}))

	require.NoError(t, registry.RememberPlaybackPolicyState(context.Background(), capreg.PlaybackPolicyState{
		SubjectKind:       string(PlaybackSubjectRecording),
		SourceFingerprint: sourceFingerprint,
		DeviceFingerprint: deviceFingerprint,
		HostFingerprint:   hostFingerprint,
		MaxQualityRung:    playbackprofile.RungCompatibleVideoH264CRF23,
		Confidence: runtimepolicy.ConfidenceSnapshot{
			Score:             -18,
			State:             runtimepolicy.ConfidenceRecovery,
			WindowCount:       2,
			CooldownUntil:     now.Add(20 * time.Second),
			PolicyConstraints: []string{runtimepolicy.ConstraintCooldownActive, runtimepolicy.ConstraintNoProbeUp},
			Reasons:           []string{runtimepolicy.ReasonNetworkRecentlyUnstable},
		},
		UpdatedAt: now,
	}))

	res, err := svc.ResolvePlaybackInfo(context.Background(), req)
	require.Nil(t, err)
	require.NotNil(t, res.Decision)
	assert.Equal(t, "cooldown", res.RuntimePolicyAction)
	assert.Equal(t, "cooldown", res.RuntimePolicyPhase)
	assert.Empty(t, res.RuntimeProbeCandidate)
	assert.Equal(t, string(playbackprofile.RungCompatibleVideoH264CRF23), res.Decision.Trace.MaxQualityRung)
	assert.True(t, res.Decision.Trace.OverrideApplied)
}

func TestService_ResolvePlaybackInfo_PersistedHighConfidenceProbesRepairUpToCompatible(t *testing.T) {
	allowTranscode := true
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/probe-up.ts"
	recordingID := domainrecordings.EncodeRecordingID(serviceRef)
	truth := playback.MediaTruth{
		Status:            playback.MediaStatusReady,
		Container:         "mpegts",
		VideoCodec:        "mpeg2",
		AudioCodec:        "ac3",
		BitrateKbps:       9000,
		BitrateConfidence: "high",
		Width:             1920,
		Height:            1080,
		FPS:               25,
	}
	registry := capreg.NewMemoryStore()
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			return truth, nil
		},
	}

	req := PlaybackInfoRequest{
		SubjectID:        recordingID,
		SubjectKind:      PlaybackSubjectRecording,
		APIVersion:       "v3.1",
		SchemaType:       "compact",
		RequestID:        "req-persisted-probe-up",
		RequestedProfile: "quality",
		Capabilities: &capabilities.PlaybackCapabilities{
			CapabilitiesVersion:  3,
			Containers:           []string{"hls", "mp4"},
			VideoCodecs:          []string{"h264"},
			AudioCodecs:          []string{"aac"},
			SupportsHLS:          true,
			SupportsHLSExplicit:  true,
			DeviceType:           "browser",
			RuntimeProbeUsed:     true,
			RuntimeProbeVersion:  2,
			ClientFamilyFallback: "chromium_hlsjs",
			ClientCapsSource:     "runtime",
			AllowTranscode:       &allowTranscode,
			DeviceContext: &capabilities.DeviceContext{
				Platform:  "linux",
				Model:     "desktop",
				OSName:    "linux",
				OSVersion: "6.12",
			},
		},
	}

	hostRuntime := playbackprofile.HostRuntimeSnapshot{PerformanceClass: "high"}
	svc := NewService(stubDeps{
		svc:         recSvc,
		capRegistry: registry,
		hostRuntime: hostRuntime,
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	})

	resolvedCaps := domainrecordings.ResolveCapabilities(context.Background(), req.PrincipalID, req.APIVersion, req.RequestedProfile, req.Headers, req.Capabilities)
	hostFingerprint := hostSnapshotForRequest(hostRuntime).Identity.Fingerprint()
	deviceFingerprint := deviceIdentityForRequest(req, resolvedCaps).Fingerprint()
	sourceFingerprint := svc.sourceSnapshotForRequest(context.Background(), serviceRef, req, truth).Fingerprint()
	now := time.Now().UTC()

	require.NoError(t, registry.RecordObservation(context.Background(), capreg.PlaybackObservation{
		ObservedAt:        now.Add(-20 * time.Second),
		RequestID:         "fb-persisted-probe-up-started",
		ObservationKind:   "feedback",
		Outcome:           "started",
		SourceRef:         serviceRef,
		SourceFingerprint: sourceFingerprint,
		SubjectKind:       string(PlaybackSubjectRecording),
		HostFingerprint:   hostFingerprint,
		DeviceFingerprint: deviceFingerprint,
		FeedbackEvent:     "info",
		FeedbackCode:      200,
	}))

	require.NoError(t, registry.RememberPlaybackPolicyState(context.Background(), capreg.PlaybackPolicyState{
		SubjectKind:       string(PlaybackSubjectRecording),
		SourceFingerprint: sourceFingerprint,
		DeviceFingerprint: deviceFingerprint,
		HostFingerprint:   hostFingerprint,
		MaxQualityRung:    playbackprofile.RungRepairH264AAC,
		Confidence: runtimepolicy.ConfidenceSnapshot{
			Score:       78,
			State:       runtimepolicy.ConfidenceHigh,
			StateSince:  now.Add(-15 * time.Second),
			WindowCount: 6,
			Reasons:     []string{runtimepolicy.ReasonHeadroomGood, runtimepolicy.ReasonCleanPlaybackWindow},
		},
		UpdatedAt: now.Add(-30 * time.Second),
	}))

	res, err := svc.ResolvePlaybackInfo(context.Background(), req)
	require.Nil(t, err)
	require.NotNil(t, res.Decision)
	assert.Equal(t, "probe_up", res.RuntimePolicyAction)
	assert.Equal(t, "probing", res.RuntimePolicyPhase)
	assert.Equal(t, string(playbackprofile.RungCompatibleVideoH264CRF23), res.RuntimeProbeCandidate)
	assert.Contains(t, res.RuntimePolicyReasons, runtimepolicy.ReasonProbeUpReady)
	assert.Equal(t, 0, res.RuntimeProbeSuccessStreak)
	assert.Equal(t, 0, res.RuntimeProbeFailureStreak)
	assert.Equal(t, string(playbackprofile.RungCompatibleVideoH264CRF23), res.Decision.Trace.MaxQualityRung)
	assert.True(t, res.Decision.Trace.OverrideApplied)
}

func TestService_ResolvePlaybackInfo_PersistedProbeSuccessAllowsNextUpgradeAtLowerScore(t *testing.T) {
	allowTranscode := true
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/probe-success-persisted.ts"
	recordingID := domainrecordings.EncodeRecordingID(serviceRef)
	truth := playback.MediaTruth{
		Status:            playback.MediaStatusReady,
		Container:         "mpegts",
		VideoCodec:        "mpeg2",
		AudioCodec:        "ac3",
		BitrateKbps:       9000,
		BitrateConfidence: "high",
		Width:             1920,
		Height:            1080,
		FPS:               25,
	}
	registry := capreg.NewMemoryStore()
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			return truth, nil
		},
	}

	req := PlaybackInfoRequest{
		SubjectID:        recordingID,
		SubjectKind:      PlaybackSubjectRecording,
		APIVersion:       "v3.1",
		SchemaType:       "compact",
		RequestID:        "req-persisted-probe-success",
		RequestedProfile: "quality",
		Capabilities: &capabilities.PlaybackCapabilities{
			CapabilitiesVersion:  3,
			Containers:           []string{"hls", "mp4"},
			VideoCodecs:          []string{"h264"},
			AudioCodecs:          []string{"aac"},
			SupportsHLS:          true,
			SupportsHLSExplicit:  true,
			DeviceType:           "browser",
			RuntimeProbeUsed:     true,
			RuntimeProbeVersion:  2,
			ClientFamilyFallback: "chromium_hlsjs",
			ClientCapsSource:     "runtime",
			AllowTranscode:       &allowTranscode,
			DeviceContext: &capabilities.DeviceContext{
				Platform:  "linux",
				Model:     "desktop",
				OSName:    "linux",
				OSVersion: "6.12",
			},
		},
	}

	hostRuntime := playbackprofile.HostRuntimeSnapshot{PerformanceClass: "high"}
	svc := NewService(stubDeps{
		svc:         recSvc,
		capRegistry: registry,
		hostRuntime: hostRuntime,
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	})

	resolvedCaps := domainrecordings.ResolveCapabilities(context.Background(), req.PrincipalID, req.APIVersion, req.RequestedProfile, req.Headers, req.Capabilities)
	hostFingerprint := hostSnapshotForRequest(hostRuntime).Identity.Fingerprint()
	deviceFingerprint := deviceIdentityForRequest(req, resolvedCaps).Fingerprint()
	sourceFingerprint := svc.sourceSnapshotForRequest(context.Background(), serviceRef, req, truth).Fingerprint()
	now := time.Now().UTC()

	require.NoError(t, registry.RememberPlaybackPolicyState(context.Background(), capreg.PlaybackPolicyState{
		SubjectKind:       string(PlaybackSubjectRecording),
		SourceFingerprint: sourceFingerprint,
		DeviceFingerprint: deviceFingerprint,
		HostFingerprint:   hostFingerprint,
		MaxQualityRung:    playbackprofile.RungCompatibleVideoH264CRF23,
		Confidence: runtimepolicy.ConfidenceSnapshot{
			Score:              58,
			State:              runtimepolicy.ConfidenceHigh,
			StateSince:         now.Add(-15 * time.Second),
			WindowCount:        6,
			ProbeSuccessStreak: 1,
			LastProbeEventAt:   now.Add(-30 * time.Second),
			Reasons:            []string{runtimepolicy.ReasonHeadroomGood, runtimepolicy.ReasonCleanPlaybackWindow, runtimepolicy.ReasonProbeRecentlyConfirmed},
		},
		UpdatedAt: now,
	}))

	res, err := svc.ResolvePlaybackInfo(context.Background(), req)
	require.Nil(t, err)
	require.NotNil(t, res.Decision)
	assert.Equal(t, "probe_up", res.RuntimePolicyAction)
	assert.Equal(t, "probing", res.RuntimePolicyPhase)
	assert.Equal(t, string(playbackprofile.RungQualityVideoH264CRF20), res.RuntimeProbeCandidate)
	assert.Contains(t, res.RuntimePolicyReasons, runtimepolicy.ReasonProbeRecentlyConfirmed)
	assert.Equal(t, 1, res.RuntimeProbeSuccessStreak)
	assert.Equal(t, 0, res.RuntimeProbeFailureStreak)
	assert.Equal(t, string(playbackprofile.RungQualityVideoH264CRF20), res.Decision.Trace.MaxQualityRung)
	assert.False(t, res.Decision.Trace.OverrideApplied)
}

func TestService_ResolvePlaybackInfo_PersistedProbeRegressionBlocksReprobeWithoutNewEvents(t *testing.T) {
	allowTranscode := true
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/probe-regression-persisted.ts"
	recordingID := domainrecordings.EncodeRecordingID(serviceRef)
	truth := playback.MediaTruth{
		Status:            playback.MediaStatusReady,
		Container:         "mpegts",
		VideoCodec:        "mpeg2",
		AudioCodec:        "ac3",
		BitrateKbps:       9000,
		BitrateConfidence: "high",
		Width:             1920,
		Height:            1080,
		FPS:               25,
	}
	registry := capreg.NewMemoryStore()
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			return truth, nil
		},
	}

	req := PlaybackInfoRequest{
		SubjectID:        recordingID,
		SubjectKind:      PlaybackSubjectRecording,
		APIVersion:       "v3.1",
		SchemaType:       "compact",
		RequestID:        "req-persisted-probe-regression",
		RequestedProfile: "quality",
		Capabilities: &capabilities.PlaybackCapabilities{
			CapabilitiesVersion:  3,
			Containers:           []string{"hls", "mp4"},
			VideoCodecs:          []string{"h264"},
			AudioCodecs:          []string{"aac"},
			SupportsHLS:          true,
			SupportsHLSExplicit:  true,
			DeviceType:           "browser",
			RuntimeProbeUsed:     true,
			RuntimeProbeVersion:  2,
			ClientFamilyFallback: "chromium_hlsjs",
			ClientCapsSource:     "runtime",
			AllowTranscode:       &allowTranscode,
			DeviceContext: &capabilities.DeviceContext{
				Platform:  "linux",
				Model:     "desktop",
				OSName:    "linux",
				OSVersion: "6.12",
			},
		},
	}

	hostRuntime := playbackprofile.HostRuntimeSnapshot{PerformanceClass: "high"}
	svc := NewService(stubDeps{
		svc:         recSvc,
		capRegistry: registry,
		hostRuntime: hostRuntime,
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	})

	resolvedCaps := domainrecordings.ResolveCapabilities(context.Background(), req.PrincipalID, req.APIVersion, req.RequestedProfile, req.Headers, req.Capabilities)
	hostFingerprint := hostSnapshotForRequest(hostRuntime).Identity.Fingerprint()
	deviceFingerprint := deviceIdentityForRequest(req, resolvedCaps).Fingerprint()
	sourceFingerprint := svc.sourceSnapshotForRequest(context.Background(), serviceRef, req, truth).Fingerprint()
	now := time.Now().UTC()

	require.NoError(t, registry.RememberPlaybackPolicyState(context.Background(), capreg.PlaybackPolicyState{
		SubjectKind:       string(PlaybackSubjectRecording),
		SourceFingerprint: sourceFingerprint,
		DeviceFingerprint: deviceFingerprint,
		HostFingerprint:   hostFingerprint,
		MaxQualityRung:    playbackprofile.RungRepairH264AAC,
		Confidence: runtimepolicy.ConfidenceSnapshot{
			Score:              82,
			State:              runtimepolicy.ConfidenceHigh,
			StateSince:         now.Add(-15 * time.Second),
			WindowCount:        6,
			ProbeFailureStreak: 1,
			LastProbeEventAt:   now.Add(-30 * time.Second),
			Reasons:            []string{runtimepolicy.ReasonHeadroomGood, runtimepolicy.ReasonProbeRecentlyRegressed},
		},
		UpdatedAt: now,
	}))

	res, err := svc.ResolvePlaybackInfo(context.Background(), req)
	require.Nil(t, err)
	require.NotNil(t, res.Decision)
	assert.Equal(t, "hold", res.RuntimePolicyAction)
	assert.Equal(t, "probe_regressed", res.RuntimePolicyPhase)
	assert.Empty(t, res.RuntimeProbeCandidate)
	assert.Contains(t, res.RuntimePolicyReasons, runtimepolicy.ReasonProbeRecentlyRegressed)
	assert.Contains(t, res.RuntimePolicyConstraints, runtimepolicy.ConstraintNoProbeUp)
	assert.Equal(t, 0, res.RuntimeProbeSuccessStreak)
	assert.Equal(t, 1, res.RuntimeProbeFailureStreak)
}

func TestService_ResolvePlaybackInfo_ProbeRegressionBlocksImmediateReprobe(t *testing.T) {
	allowTranscode := true
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/probe-regression.ts"
	recordingID := domainrecordings.EncodeRecordingID(serviceRef)
	truth := playback.MediaTruth{
		Status:            playback.MediaStatusReady,
		Container:         "mpegts",
		VideoCodec:        "mpeg2",
		AudioCodec:        "ac3",
		BitrateKbps:       9000,
		BitrateConfidence: "high",
		Width:             1920,
		Height:            1080,
		FPS:               25,
	}
	registry := capreg.NewMemoryStore()
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			return truth, nil
		},
	}

	req := PlaybackInfoRequest{
		SubjectID:        recordingID,
		SubjectKind:      PlaybackSubjectRecording,
		APIVersion:       "v3.1",
		SchemaType:       "compact",
		RequestID:        "req-probe-regression",
		RequestedProfile: "quality",
		Capabilities: &capabilities.PlaybackCapabilities{
			CapabilitiesVersion:  3,
			Containers:           []string{"hls", "mp4"},
			VideoCodecs:          []string{"h264"},
			AudioCodecs:          []string{"aac"},
			SupportsHLS:          true,
			SupportsHLSExplicit:  true,
			DeviceType:           "browser",
			RuntimeProbeUsed:     true,
			RuntimeProbeVersion:  2,
			ClientFamilyFallback: "chromium_hlsjs",
			ClientCapsSource:     "runtime",
			AllowTranscode:       &allowTranscode,
			DeviceContext: &capabilities.DeviceContext{
				Platform:  "linux",
				Model:     "desktop",
				OSName:    "linux",
				OSVersion: "6.12",
			},
		},
	}

	hostRuntime := playbackprofile.HostRuntimeSnapshot{PerformanceClass: "high"}
	svc := NewService(stubDeps{
		svc:         recSvc,
		capRegistry: registry,
		hostRuntime: hostRuntime,
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	})

	resolvedCaps := domainrecordings.ResolveCapabilities(context.Background(), req.PrincipalID, req.APIVersion, req.RequestedProfile, req.Headers, req.Capabilities)
	hostFingerprint := hostSnapshotForRequest(hostRuntime).Identity.Fingerprint()
	deviceFingerprint := deviceIdentityForRequest(req, resolvedCaps).Fingerprint()
	sourceFingerprint := svc.sourceSnapshotForRequest(context.Background(), serviceRef, req, truth).Fingerprint()
	now := time.Now().UTC()

	require.NoError(t, registry.RecordObservation(context.Background(), capreg.PlaybackObservation{
		ObservedAt:        now.Add(-20 * time.Second),
		RequestID:         "fb-probe-regression-started",
		ObservationKind:   "feedback",
		Outcome:           "started",
		SourceRef:         serviceRef,
		SourceFingerprint: sourceFingerprint,
		SubjectKind:       string(PlaybackSubjectRecording),
		HostFingerprint:   hostFingerprint,
		DeviceFingerprint: deviceFingerprint,
		FeedbackEvent:     "info",
		FeedbackCode:      220,
	}))
	require.NoError(t, registry.RecordObservation(context.Background(), capreg.PlaybackObservation{
		ObservedAt:        now.Add(-10 * time.Second),
		RequestID:         "fb-probe-regression-warning",
		ObservationKind:   "feedback",
		Outcome:           "warning",
		SourceRef:         serviceRef,
		SourceFingerprint: sourceFingerprint,
		SubjectKind:       string(PlaybackSubjectRecording),
		HostFingerprint:   hostFingerprint,
		DeviceFingerprint: deviceFingerprint,
		FeedbackEvent:     "warning",
		FeedbackCode:      104,
	}))

	require.NoError(t, registry.RememberPlaybackPolicyState(context.Background(), capreg.PlaybackPolicyState{
		SubjectKind:       string(PlaybackSubjectRecording),
		SourceFingerprint: sourceFingerprint,
		DeviceFingerprint: deviceFingerprint,
		HostFingerprint:   hostFingerprint,
		MaxQualityRung:    playbackprofile.RungRepairH264AAC,
		Confidence: runtimepolicy.ConfidenceSnapshot{
			Score:       78,
			State:       runtimepolicy.ConfidenceHigh,
			StateSince:  now.Add(-15 * time.Second),
			WindowCount: 6,
			Reasons:     []string{runtimepolicy.ReasonHeadroomGood, runtimepolicy.ReasonCleanPlaybackWindow},
		},
		UpdatedAt: now.Add(-30 * time.Second),
	}))

	res, err := svc.ResolvePlaybackInfo(context.Background(), req)
	require.Nil(t, err)
	require.NotNil(t, res.Decision)
	assert.Equal(t, "hold", res.RuntimePolicyAction)
	assert.Equal(t, "probe_regressed", res.RuntimePolicyPhase)
	assert.Empty(t, res.RuntimeProbeCandidate)
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

func verifiedLiveTruthSource(cap scan.Capability) *stubTruthSource {
	return &stubTruthSource{
		getCapabilityFn: func(serviceRef string) (scan.Capability, bool) {
			if cap.ServiceRef == "" {
				cap.ServiceRef = serviceRef
			}
			if cap.LastScan.IsZero() {
				now := time.Now().UTC()
				cap.LastScan = now
				if cap.LastSuccess.IsZero() {
					cap.LastSuccess = now
				}
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
				ServiceRef:         serviceRef,
				State:              scan.CapabilityStateOK,
				Container:          "ts",
				VideoCodec:         "hevc",
				AudioCodec:         "ac3",
				Codec:              "hevc",
				BitrateKbps:        8000,
				BitrateMeanKbps:    9500,
				BitratePeakKbps:    12000,
				BitrateSamples:     4,
				Resolution:         "3840x2160",
				Width:              3840,
				Height:             2160,
				FPS:                25,
				SignalFPS:          50,
				Interlaced:         true,
				FieldOrder:         "tt",
				AudioChannels:      6,
				AudioBitrateKbps:   384,
				AudioSampleRate:    48000,
				AudioChannelLayout: "5.1(side)",
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
	require.Nil(t, res.Decision)
	require.NotNil(t, res.PlannerEvaluation)
	assert.Equal(t, playbackcompat.PolicyVersion, res.CapabilityContract.PolicyVersion)
	assert.Equal(t, playbackcompat.PolicyVersion, res.PlannerEvaluation.Evidence.PolicyVersion)
	assert.Equal(t, playbackcompat.PolicyVersion, res.PlannerEvaluation.Result.Trace.PolicyVersion)
	assert.Equal(t, "ts", res.Truth.Container)
	assert.Equal(t, "hevc", res.Truth.VideoCodec)
	assert.Equal(t, "ac3", res.Truth.AudioCodec)
	assert.Equal(t, 10200, res.Truth.BitrateKbps)
	assert.Equal(t, 8000, res.Truth.BitrateObservedKbps)
	assert.Equal(t, 12000, res.Truth.BitratePeakKbps)
	assert.Equal(t, 4, res.Truth.BitrateSamples)
	assert.Equal(t, "high", res.Truth.BitrateConfidence)
	assert.Equal(t, 3840, res.Truth.Width)
	assert.Equal(t, 2160, res.Truth.Height)
	assert.Equal(t, 25.0, res.Truth.FPS)
	assert.Equal(t, 50.0, res.Truth.SignalFPS)
	assert.True(t, res.Truth.Interlaced)
	assert.Equal(t, "tt", res.Truth.FieldOrder)
	assert.Equal(t, 6, res.Truth.AudioChannels)
	assert.Equal(t, 384, res.Truth.AudioBitrateKbps)
	assert.Equal(t, 48000, res.Truth.AudioSampleRate)
	assert.Equal(t, "5.1(side)", res.Truth.AudioChannelLayout)
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
	require.Nil(t, res.Decision)
	require.NotNil(t, res.PlannerEvaluation)
	assert.Equal(t, playbackplanner.DecisionAllow, res.PlannerEvaluation.Result.Plan.Decision)
	assert.Equal(t, "transcode", res.PlannerEvaluation.Result.Plan.Mode)
	assert.Equal(t, "transcode", res.PlannerEvaluation.Result.Plan.Video.Mode)
	assert.Equal(t, "h264", res.PlannerEvaluation.Result.Plan.Video.Codec)
	assert.Equal(t, "copy", res.PlannerEvaluation.Result.Plan.Audio.Mode)
	assert.Equal(t, "aac", res.PlannerEvaluation.Result.Plan.Audio.Codec)
}

func TestService_ResolvePlaybackInfo_LiveAndroidTVNativeCopyableTSReturnsFMP4DirectStream(t *testing.T) {
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
	require.Nil(t, res.Decision)
	require.NotNil(t, res.PlannerEvaluation)
	assert.Equal(t, playbackplanner.DecisionAllow, res.PlannerEvaluation.Result.Plan.Decision)
	assert.Equal(t, "remux", res.PlannerEvaluation.Result.Plan.Mode)
	assert.Equal(t, "hls", res.PlannerEvaluation.Result.Plan.DeliveryEngine)
	assert.Equal(t, "mpegts", res.PlannerEvaluation.Result.Plan.Packaging.Container, "copied DVB H.264 uses MPEG-TS to prevent open-GOP fMP4 judder")
	assert.Equal(t, "copy", res.PlannerEvaluation.Result.Plan.Video.Mode)
	assert.Equal(t, "h264", res.PlannerEvaluation.Result.Plan.Video.Codec)
	assert.Equal(t, "copy", res.PlannerEvaluation.Result.Plan.Audio.Mode)
	assert.Equal(t, "ac3", res.PlannerEvaluation.Result.Plan.Audio.Codec)
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
	require.Nil(t, res.Decision)
	require.NotNil(t, res.PlannerEvaluation)
	assert.Equal(t, "ts", res.Truth.Container)
	assert.Equal(t, "h264", res.Truth.VideoCodec)
	assert.Equal(t, "aac", res.Truth.AudioCodec)
	assert.Equal(t, 1, truthSource.calls)
	assert.Equal(t, 1, truthSource.probeCalls)
	assert.Equal(t, "1:0:1:2B66:3F3:1:C00000:0:0:0:", truthSource.lastProbeRef)
}

func TestService_ResolvePlaybackInfo_LiveIncompleteScanTruthUsesTargetedProbe(t *testing.T) {
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			t.Fatal("GetMediaTruth must not be called for live playback")
			return playback.MediaTruth{}, nil
		},
	}

	staleCapability := scan.Capability{
		ServiceRef: "1:0:1:2B66:3F3:1:C00000:0:0:0:",
		State:      scan.CapabilityStateOK,
		VideoCodec: "h264",
		Codec:      "h264",
		Resolution: "1920x1080",
		Width:      1920,
		Height:     1080,
	}
	probedCapability := scan.Capability{
		ServiceRef: "1:0:1:2B66:3F3:1:C00000:0:0:0:",
		State:      scan.CapabilityStateOK,
		Container:  "ts",
		VideoCodec: "h264",
		AudioCodec: "ac3",
		Codec:      "h264",
		Resolution: "1920x1080",
		Width:      1920,
		Height:     1080,
	}

	truthSource := &stubTruthSource{
		getCapabilityFn: func(serviceRef string) (scan.Capability, bool) {
			return staleCapability, true
		},
		probeCapabilityFn: func(ctx context.Context, serviceRef string) (scan.Capability, bool, error) {
			return probedCapability, true, nil
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
		RequestID:   "req-live-incomplete-targeted-probe",
	})
	require.Nil(t, err)
	require.Nil(t, res.Decision)
	require.NotNil(t, res.PlannerEvaluation)
	assert.Equal(t, "ts", res.Truth.Container)
	assert.Equal(t, "h264", res.Truth.VideoCodec)
	assert.Equal(t, "ac3", res.Truth.AudioCodec)
	assert.Equal(t, 1, truthSource.calls)
	assert.Equal(t, 1, truthSource.probeCalls)
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

func staleScanTruthTestCapability() scan.Capability {
	return scan.Capability{
		ServiceRef:  "1:0:1:2B66:3F3:1:C00000:0:0:0:",
		State:       scan.CapabilityStateOK,
		Container:   "ts",
		VideoCodec:  "h264",
		AudioCodec:  "aac",
		Codec:       "h264",
		Resolution:  "1920x1080",
		Width:       1920,
		Height:      1080,
		FPS:         25,
		LastScan:    time.Now().UTC().Add(-3 * time.Hour),
		LastSuccess: time.Now().UTC().Add(-3 * time.Hour),
	}
}

// Stale-while-revalidate: stale-but-complete scan truth is served from cache
// immediately; the probe fires detached in the background and must not delay or
// alter the response.
func TestService_ResolvePlaybackInfo_LiveStaleScanTruthServedFromCacheAndRevalidated(t *testing.T) {
	prevWindow := liveTruthFreshnessWindow.Load()
	SetLiveTruthFreshnessWindow(time.Hour)
	t.Cleanup(func() { liveTruthFreshnessWindow.Store(prevWindow) })
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			t.Fatal("GetMediaTruth must not be called for live playback")
			return playback.MediaTruth{}, nil
		},
	}

	staleCapability := staleScanTruthTestCapability()
	probedCapability := staleCapability
	probedCapability.AudioCodec = "ac3"
	probedCapability.LastScan = time.Now().UTC()
	probedCapability.LastSuccess = time.Now().UTC()

	probeStarted := make(chan struct{})
	truthSource := &stubTruthSource{
		getCapabilityFn: func(serviceRef string) (scan.Capability, bool) {
			return staleCapability, true
		},
		probeCapabilityFn: func(ctx context.Context, serviceRef string) (scan.Capability, bool, error) {
			close(probeStarted)
			return probedCapability, true, nil
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
		RequestID:   "req-live-stale-swr",
	})
	require.Nil(t, err)
	require.Nil(t, res.Decision)
	require.NotNil(t, res.PlannerEvaluation)
	assert.Equal(t, "ts", res.Truth.Container)
	assert.Equal(t, "h264", res.Truth.VideoCodec)
	// The CACHED truth is served, not the probe result — the probe runs detached.
	assert.Equal(t, "aac", res.Truth.AudioCodec)
	assert.Equal(t, 1, truthSource.calls)

	select {
	case <-probeStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("stale truth must trigger a detached background revalidation probe")
	}
	assert.Equal(t, 1, truthSource.probeCalls)
	assert.Equal(t, "1:0:1:2B66:3F3:1:C00000:0:0:0:", truthSource.lastProbeRef)
}

// Even when the background probe finds nothing fresh (receiver cold, relay I/O
// error), the stale cache entry keeps serving playback — it must not fail closed.
func TestService_ResolvePlaybackInfo_LiveStaleScanTruthServedWhenProbeFindsNothing(t *testing.T) {
	prevWindow := liveTruthFreshnessWindow.Load()
	SetLiveTruthFreshnessWindow(time.Hour)
	t.Cleanup(func() { liveTruthFreshnessWindow.Store(prevWindow) })
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			t.Fatal("GetMediaTruth must not be called for live playback")
			return playback.MediaTruth{}, nil
		},
	}

	staleCapability := staleScanTruthTestCapability()
	probeStarted := make(chan struct{})
	truthSource := &stubTruthSource{
		getCapabilityFn: func(serviceRef string) (scan.Capability, bool) {
			return staleCapability, true
		},
		probeCapabilityFn: func(ctx context.Context, serviceRef string) (scan.Capability, bool, error) {
			close(probeStarted)
			return scan.Capability{}, false, nil
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
		RequestID:   "req-live-stale-probe-empty",
	})
	require.Nil(t, err)
	require.Nil(t, res.Decision)
	require.NotNil(t, res.PlannerEvaluation)
	assert.Equal(t, "ts", res.Truth.Container)
	assert.Equal(t, "aac", res.Truth.AudioCodec)

	select {
	case <-probeStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("stale truth must trigger a detached background revalidation probe")
	}
	assert.Equal(t, 1, truthSource.probeCalls)
}

// epg_badge requests are passive fan-out over the channel grid: they get the
// stale cache entry served but must NOT each fire a revalidation probe.
func TestService_ResolvePlaybackInfo_LiveStaleScanTruthEpgBadgeDoesNotProbe(t *testing.T) {
	prevWindow := liveTruthFreshnessWindow.Load()
	SetLiveTruthFreshnessWindow(time.Hour)
	t.Cleanup(func() { liveTruthFreshnessWindow.Store(prevWindow) })
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			t.Fatal("GetMediaTruth must not be called for live playback")
			return playback.MediaTruth{}, nil
		},
	}

	staleCapability := staleScanTruthTestCapability()
	truthSource := &stubTruthSource{
		getCapabilityFn: func(serviceRef string) (scan.Capability, bool) {
			return staleCapability, true
		},
		probeCapabilityFn: func(ctx context.Context, serviceRef string) (scan.Capability, bool, error) {
			t.Error("epg_badge request must not trigger a revalidation probe")
			return scan.Capability{}, false, nil
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
		RequestID:   "req-live-stale-epg-badge",
		Headers:     map[string]string{PlaybackInfoContextHeader: PlaybackInfoContextEpgBadge},
	})
	require.Nil(t, err)
	require.Nil(t, res.Decision)
	require.NotNil(t, res.PlannerEvaluation)
	assert.Equal(t, "ts", res.Truth.Container)
	assert.Equal(t, "aac", res.Truth.AudioCodec)

	// Give a mistakenly fired detached probe a moment to surface before the test ends.
	time.Sleep(100 * time.Millisecond)
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
	assert.Equal(t, 1, truthSource.probeCalls)
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
		truthSource: verifiedLiveTruthSource(scan.Capability{State: scan.CapabilityStateOK, Container: "ts", VideoCodec: "h264", AudioCodec: "ac3", Width: 1920, Height: 1080, FPS: 25}),
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
	require.Nil(t, res.Decision)
	require.NotNil(t, res.PlannerEvaluation)
	assert.Equal(t, playbackplanner.DecisionAllow, res.PlannerEvaluation.Result.Plan.Decision)
	assert.Equal(t, "transcode", res.PlannerEvaluation.Result.Plan.Mode)
	assert.Equal(t, "fmp4", res.PlannerEvaluation.Result.Plan.Packaging.Container)
	assert.Equal(t, "hevc", res.PlannerEvaluation.Result.Plan.Video.Codec)
	assert.Equal(t, "aac", res.PlannerEvaluation.Result.Plan.Audio.Codec)
}

func TestService_ResolvePlaybackInfo_LiveNativeAV1OnIOSUsesFMP4AndIgnoresMeasuredH264Preference(t *testing.T) {
	t.Setenv("XG2G_EXPERIMENTAL_AV1_MPEGTS_ENABLED", "true")

	hardware.SetVAAPIPreflightResult(true)
	hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{
		"av1_vaapi":  {Verified: true, AutoEligible: true, ProbeElapsed: 50},
		"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 10},
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
		truthSource: verifiedLiveTruthSource(scan.Capability{State: scan.CapabilityStateOK, Container: "ts", VideoCodec: "h264", AudioCodec: "ac3", Width: 1920, Height: 1080, FPS: 25, Interlaced: false}),
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	})

	supportsRange := true
	av1Smooth := true
	av1Efficient := true
	h264Smooth := true
	h264Efficient := true
	res, err := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:        "1:0:19:EF75:3F9:1:C00000:0:0:0:",
		SubjectKind:      PlaybackSubjectLive,
		APIVersion:       "v3.1",
		SchemaType:       "live",
		RequestedProfile: "quality",
		RequestID:        "req-live-native-av1-ts",
		Capabilities: &capabilities.PlaybackCapabilities{
			CapabilitiesVersion: 3,
			Containers:          []string{"mp4", "ts", "fmp4"},
			VideoCodecs:         []string{"av1", "hevc", "h264"},
			VideoCodecSignals: []capabilities.VideoCodecSignal{
				{Codec: "av1", Supported: true, Smooth: &av1Smooth, PowerEfficient: &av1Efficient},
				{Codec: "h264", Supported: true, Smooth: &h264Smooth, PowerEfficient: &h264Efficient},
			},
			AudioCodecs:          []string{"aac"},
			SupportsHLS:          true,
			SupportsRange:        &supportsRange,
			DeviceType:           "mobile",
			HLSEngines:           []string{"native"},
			PreferredHLSEngine:   "native",
			RuntimeProbeUsed:     true,
			RuntimeProbeVersion:  2,
			ClientFamilyFallback: "ios_safari_native",
			ClientCapsSource:     "runtime_plus_family",
			DeviceContext:        &capabilities.DeviceContext{OSName: "ios", OSVersion: "17.5", Model: "iPhone 15 Pro A17 Pro"},
		},
	})
	require.Nil(t, err)
	require.Nil(t, res.Decision)
	require.NotNil(t, res.PlannerEvaluation)
	assert.Equal(t, playbackplanner.DecisionAllow, res.PlannerEvaluation.Result.Plan.Decision)
	assert.Equal(t, "transcode", res.PlannerEvaluation.Result.Plan.Mode)
	assert.Equal(t, "hls", res.PlannerEvaluation.Result.Plan.DeliveryEngine)
	assert.Equal(t, "h264", res.PlannerEvaluation.Result.Plan.Video.Codec)
	assert.Equal(t, "mpegts", res.PlannerEvaluation.Result.Plan.Packaging.Container, "copied DVB H.264 uses MPEG-TS to prevent open-GOP fMP4 judder")
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
	require.Nil(t, res.Decision)
	require.NotNil(t, res.PlannerEvaluation)
	assert.Equal(t, playbackplanner.DecisionAllow, res.PlannerEvaluation.Result.Plan.Decision)
	assert.Equal(t, "transcode", res.PlannerEvaluation.Result.Plan.Mode)
	assert.Equal(t, "h264", res.PlannerEvaluation.Result.Plan.Video.Codec)
	assert.NotEqual(t, "hevc", res.PlannerEvaluation.Result.Plan.Video.Codec)
}

func TestService_ResolvePlaybackInfo_LiveSkipsLegacyDecisionAudit(t *testing.T) {
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
	require.Nil(t, res.Decision)
	require.NotNil(t, res.PlannerEvaluation)
	require.Equal(t, 0, auditSink.callCount)
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

// TestService_ResolvePlaybackInfo_LiveAgedTruthWithinWindowServedWithoutProbe is the
// load-bearing assertion for the widened freshness window: a capability a few hours old
// (well within the default 7d window) must be served straight from cache WITHOUT a
// blocking synchronous probe. Narrow the window back toward 2h and this goes red.
func TestService_ResolvePlaybackInfo_LiveAgedTruthWithinWindowServedWithoutProbe(t *testing.T) {
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			t.Fatal("GetMediaTruth must not be called for live playback")
			return playback.MediaTruth{}, nil
		},
	}

	agedCapability := scan.Capability{
		ServiceRef:  "1:0:1:2B66:3F3:1:C00000:0:0:0:",
		State:       scan.CapabilityStateOK,
		Container:   "ts",
		VideoCodec:  "h264",
		AudioCodec:  "aac",
		Codec:       "h264",
		Resolution:  "1920x1080",
		Width:       1920,
		Height:      1080,
		FPS:         25,
		LastScan:    time.Now().UTC().Add(-3 * time.Hour),
		LastSuccess: time.Now().UTC().Add(-3 * time.Hour),
	}

	truthSource := &stubTruthSource{
		getCapabilityFn: func(serviceRef string) (scan.Capability, bool) {
			return agedCapability, true
		},
		probeCapabilityFn: func(ctx context.Context, serviceRef string) (scan.Capability, bool, error) {
			t.Fatal("ProbeCapability must not be called for truth within the freshness window")
			return scan.Capability{}, false, nil
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
		RequestID:   "req-live-aged-within-window",
	})
	require.Nil(t, err)
	require.Nil(t, res.Decision)
	require.NotNil(t, res.PlannerEvaluation)
	assert.Equal(t, "ts", res.Truth.Container)
	assert.Equal(t, "h264", res.Truth.VideoCodec)
	assert.Equal(t, "aac", res.Truth.AudioCodec)
	assert.Equal(t, 1, truthSource.calls)
	assert.Equal(t, 0, truthSource.probeCalls)
}

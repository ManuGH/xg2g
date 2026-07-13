package recordings

import (
	"context"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/playback"
	domainrecordings "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/playbackshadow"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockShadowObserver struct {
	observations []playbackshadow.ShadowObservation
}

func (m *mockShadowObserver) TryObserve(obs playbackshadow.ShadowObservation) bool {
	m.observations = append(m.observations, obs)
	return true
}

type stubChannelSource struct {
	cap   scan.Capability
	found bool
}

func (s *stubChannelSource) GetCapability(serviceRef string) (scan.Capability, bool) {
	return s.cap, s.found
}

func TestResolvePlaybackInfo_ShadowObserver_RecordingAndOff(t *testing.T) {
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

	obs := &mockShadowObserver{}

	svc := NewService(stubDeps{
		svc: recSvc,
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	}, WithPlannerShadowObserver(obs))

	// Test Shadow ON (recording)
	res1, err := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:   recordingID,
		SubjectKind: PlaybackSubjectRecording,
		APIVersion:  "v3.1",
		SchemaType:  "compact",
		RequestID:   "req-1",
	})
	require.Nil(t, err)
	require.NotNil(t, res1.Decision)
	assert.Equal(t, 1, len(obs.observations))
	assert.Equal(t, "recordings", obs.observations[0].Evidence.Provenance)
	assert.Equal(t, "ok", obs.observations[0].Evidence.Confidence)

	// Test Shadow OFF
	svcOff := NewService(stubDeps{
		svc: recSvc,
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	}) // Without observer option
	res2, err := svcOff.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:   recordingID,
		SubjectKind: PlaybackSubjectRecording,
		APIVersion:  "v3.1",
		SchemaType:  "compact",
		RequestID:   "req-2",
	})
	require.Nil(t, err)

	// Decisions should be identical
	assert.Equal(t, res1.Decision.Mode, res2.Decision.Mode)
	assert.Equal(t, res1.Decision.Selected, res2.Decision.Selected)
}

func TestResolvePlaybackInfo_ShadowObserver_Live(t *testing.T) {
	serviceRef := "1:0:1:2B66:3F3:1:C00000:0:0:0:"
	recSvc := &stubRecordingsService{}

	obs := &mockShadowObserver{}

	svc := NewService(stubDeps{
		svc: recSvc,
		truthSource: &stubChannelSource{
			cap: scan.Capability{
				ServiceRef: serviceRef,
				Container:  "mpegts",
				VideoCodec: "h264",
				AudioCodec: "aac",
				LastScan:   time.Now().UTC(),
			},
			found: true,
		},
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	}, WithPlannerShadowObserver(obs))

	res, err := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:   serviceRef,
		SubjectKind: PlaybackSubjectLive,
		APIVersion:  "v3.1",
		SchemaType:  "compact",
		RequestID:   "req-3",
	})
	require.Nil(t, err)
	require.NotNil(t, res.Decision)
	assert.Equal(t, 1, len(obs.observations))
	assert.Equal(t, "live_scan", obs.observations[0].Evidence.Provenance)

	// Verify shadow OFF produces 0 observations and identical decision for live
	obsOff := &mockShadowObserver{}
	svcOff := NewService(stubDeps{
		svc: recSvc,
		truthSource: &stubChannelSource{
			cap: scan.Capability{
				ServiceRef: serviceRef,
				Container:  "mpegts",
				VideoCodec: "h264",
				AudioCodec: "aac",
				LastScan:   time.Now().UTC(),
			},
			found: true,
		},
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	}) // No observer option passed
	resOff, err := svcOff.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:   serviceRef,
		SubjectKind: PlaybackSubjectLive,
		APIVersion:  "v3.1",
		SchemaType:  "compact",
		RequestID:   "req-4",
	})
	require.Nil(t, err)
	assert.Equal(t, 0, len(obsOff.observations))
	assert.Equal(t, res.Decision.Mode, resOff.Decision.Mode)
	assert.Equal(t, res.Decision.Selected, resOff.Decision.Selected)
}

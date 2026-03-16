package recordings

import (
	"context"
	"errors"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/playback"
	domainrecordings "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
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

func TestService_ResolvePlaybackInfo_LiveSuccess(t *testing.T) {
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

	res, err := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:   "1:0:1:2B66:3F3:1:C00000:0:0:0:",
		SubjectKind: PlaybackSubjectLive,
		APIVersion:  "v3.1",
		SchemaType:  "live",
		RequestID:   "req-live",
	})
	require.Nil(t, err)
	require.NotNil(t, res.Decision)
	assert.Equal(t, "1:0:1:2B66:3F3:1:C00000:0:0:0:", res.SourceRef)
	assert.Equal(t, "mpegts", res.Truth.Container)
	assert.Equal(t, "h264", res.Truth.VideoCodec)
	assert.Equal(t, "aac", res.Truth.AudioCodec)
	assert.Equal(t, 0, recSvc.truthCalls)
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

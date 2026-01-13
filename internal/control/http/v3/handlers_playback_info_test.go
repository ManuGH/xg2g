package v3

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockRecordingsService for testing handler interaction
type MockRecordingsService struct {
	mock.Mock
}

func (m *MockRecordingsService) ResolvePlayback(ctx context.Context, recordingID, profile string) (recservice.PlaybackResolution, error) {
	args := m.Called(ctx, recordingID, profile)
	return args.Get(0).(recservice.PlaybackResolution), args.Error(1)
}
func (m *MockRecordingsService) List(ctx context.Context, in recservice.ListInput) (recservice.ListResult, error) {
	return recservice.ListResult{}, nil
}
func (m *MockRecordingsService) GetPlaybackInfo(ctx context.Context, in recservice.PlaybackInfoInput) (recservice.PlaybackInfoResult, error) {
	return recservice.PlaybackInfoResult{}, nil
}
func (m *MockRecordingsService) GetStatus(ctx context.Context, in recservice.StatusInput) (recservice.StatusResult, error) {
	return recservice.StatusResult{}, nil
}
func (m *MockRecordingsService) Stream(ctx context.Context, in recservice.StreamInput) (recservice.StreamResult, error) {
	return recservice.StreamResult{}, nil
}
func (m *MockRecordingsService) Delete(ctx context.Context, in recservice.DeleteInput) (recservice.DeleteResult, error) {
	return recservice.DeleteResult{}, nil
}

func TestGetRecordingPlaybackInfo_StrictTruthfulness(t *testing.T) {
	// 1. Matrix: Error Codes
	tests := []struct {
		name       string
		mockErr    error
		wantStatus int
		wantBody   string
		wantHeader map[string]string
	}{
		{
			name:       "NotFound",
			mockErr:    recservice.ErrNotFound{RecordingID: "rec1"},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "Preparing",
			mockErr:    recservice.ErrPreparing{RecordingID: "rec1"},
			wantStatus: http.StatusServiceUnavailable,
			wantHeader: map[string]string{"Retry-After": "5"},
		},
		{
			name:       "InvalidArgument",
			mockErr:    recservice.ErrInvalidArgument{Field: "id", Reason: "bad"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "UpstreamError",
			mockErr:    recservice.ErrUpstream{Op: "probe", Cause: errors.New("timeout")},
			wantStatus: http.StatusBadGateway,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := new(MockRecordingsService)
			svc.On("ResolvePlayback", mock.Anything, "rec1", "generic").Return(recservice.PlaybackResolution{}, tt.mockErr)

			s := &Server{recordingsService: svc}
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/api/v3/recordings/rec1/stream-info", nil)

			s.GetRecordingPlaybackInfo(w, r, "rec1")

			assert.Equal(t, tt.wantStatus, w.Code)
			if tt.wantHeader != nil {
				for k, v := range tt.wantHeader {
					assert.Equal(t, v, w.Header().Get(k))
				}
			}
		})
	}

	// 2. Strict DTO Mapping (Nil Semantics)
	// We define a local test DTO since the generated one doesn't have codec fields yet,
	// but the handler returns them.
	type testPlaybackInfoDTO struct {
		Mode            PlaybackInfoMode            `json:"mode"`
		Url             string                      `json:"url"`
		Seekable        *bool                       `json:"seekable,omitempty"`
		DurationSeconds *int64                      `json:"duration_seconds,omitempty"`
		DurationSource  *PlaybackInfoDurationSource `json:"duration_source,omitempty"`

		Container  *string `json:"container,omitempty"`
		VideoCodec *string `json:"video_codec,omitempty"`
		AudioCodec *string `json:"audio_codec,omitempty"`
	}

	t.Run("DTO_Mapping_UnknownDuration_UnknownCodecs", func(t *testing.T) {
		svc := new(MockRecordingsService)
		res := recservice.PlaybackResolution{
			Strategy:       recservice.StrategyDirect,
			CanSeek:        true,
			DurationSec:    nil, // Unknown
			DurationSource: nil, // Unknown
			Container:      nil, // Unknown
			VideoCodec:     nil, // Unknown
			AudioCodec:     nil, // Unknown
		}
		svc.On("ResolvePlayback", mock.Anything, "rec1", "generic").Return(res, nil)

		s := &Server{recordingsService: svc}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/v3/recordings/rec1/stream-info", nil)

		s.GetRecordingPlaybackInfo(w, r, "rec1")

		assert.Equal(t, http.StatusOK, w.Code)
		var dto testPlaybackInfoDTO
		err := json.Unmarshal(w.Body.Bytes(), &dto)
		assert.NoError(t, err)

		// Assertions
		assert.Equal(t, DirectMp4, dto.Mode)
		assert.Equal(t, "/api/v3/recordings/rec1/stream.mp4", dto.Url)
		assert.Equal(t, true, *dto.Seekable)
		assert.Nil(t, dto.DurationSeconds) // Strict omission
		assert.Nil(t, dto.DurationSource)  // Strict omission
		assert.Nil(t, dto.Container)       // Strict omission
		assert.Nil(t, dto.VideoCodec)
		assert.Nil(t, dto.AudioCodec)
	})

	t.Run("DTO_Mapping_KnownDuration_KnownCodecs", func(t *testing.T) {
		svc := new(MockRecordingsService)
		dur := int64(3600)
		src := recservice.DurationSourceStore
		c := "mp4"
		v := "h264"
		a := "aac"
		res := recservice.PlaybackResolution{
			Strategy:       recservice.StrategyHLS,
			CanSeek:        true,
			DurationSec:    &dur,
			DurationSource: &src,
			Container:      &c,
			VideoCodec:     &v,
			AudioCodec:     &a,
		}
		svc.On("ResolvePlayback", mock.Anything, "rec1", "generic").Return(res, nil)

		s := &Server{recordingsService: svc}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/v3/recordings/rec1/stream-info", nil)

		s.GetRecordingPlaybackInfo(w, r, "rec1")

		assert.Equal(t, http.StatusOK, w.Code)
		var dto testPlaybackInfoDTO
		err := json.Unmarshal(w.Body.Bytes(), &dto)
		assert.NoError(t, err)

		assert.Equal(t, Hls, dto.Mode)
		assert.Equal(t, "/api/v3/recordings/rec1/playlist.m3u8", dto.Url)
		assert.Equal(t, int64(3600), *dto.DurationSeconds)
		expectedSrc := Store
		assert.Equal(t, &expectedSrc, dto.DurationSource)

		assert.Equal(t, "mp4", *dto.Container)
		assert.Equal(t, "h264", *dto.VideoCodec)
		assert.Equal(t, "aac", *dto.AudioCodec)
	})
}

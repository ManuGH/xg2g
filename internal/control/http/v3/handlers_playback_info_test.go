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
	"github.com/stretchr/testify/require"
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
			mockErr:    recservice.ErrNotFound{RecordingID: "1:0:0:0:0:0:0:0:0:0:/hdd/movie/rec1.ts"},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "Preparing",
			mockErr:    recservice.ErrPreparing{RecordingID: "1:0:0:0:0:0:0:0:0:0:/hdd/movie/rec1.ts"},
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
			serviceRef := "1:0:0:0:0:0:0:0:0:0:/hdd/movie/rec1.ts"
			recordingID := recservice.EncodeRecordingID(serviceRef)

			svc := new(MockRecordingsService)
			svc.On("ResolvePlayback", mock.Anything, recordingID, "generic").Return(recservice.PlaybackResolution{}, tt.mockErr)

			s := &Server{recordingsService: svc}
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/api/v3/recordings/"+recordingID+"/stream-info", nil)

			s.GetRecordingPlaybackInfo(w, r, recordingID)

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
		serviceRef := "1:0:0:0:0:0:0:0:0:0:/hdd/movie/rec1.ts"
		recordingID := recservice.EncodeRecordingID(serviceRef)

		svc := new(MockRecordingsService)
		res := recservice.PlaybackResolution{
			Strategy:       recservice.StrategyDirect,
			CanSeek:        true,
			DurationSec:    nil, // Unknown
			DurationSource: nil, // Unknown
			Container:      nil, // Unknown
			VideoCodec:     nil, // Unknown
			AudioCodec:     nil, // Unknown
			Reason:         recservice.ReasonDirectPlayMatch,
		}
		svc.On("ResolvePlayback", mock.Anything, recordingID, "generic").Return(res, nil)

		s := &Server{recordingsService: svc}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/v3/recordings/"+recordingID+"/stream-info", nil)

		s.GetRecordingPlaybackInfo(w, r, recordingID)

		require.Equal(t, http.StatusOK, w.Code)
		var dto testPlaybackInfoDTO
		err := json.Unmarshal(w.Body.Bytes(), &dto)
		assert.NoError(t, err)

		// Assertions
		assert.Equal(t, DirectMp4, dto.Mode)
		assert.Equal(t, "/api/v3/recordings/"+recordingID+"/stream.mp4", dto.Url)
		require.NotNil(t, dto.Seekable)
		assert.Equal(t, true, *dto.Seekable)
		assert.Nil(t, dto.DurationSeconds) // Strict omission
		assert.Nil(t, dto.DurationSource)  // Strict omission
		assert.Nil(t, dto.Container)       // Strict omission
		assert.Nil(t, dto.VideoCodec)
		assert.Nil(t, dto.AudioCodec)
	})

	t.Run("DTO_Mapping_KnownDuration_KnownCodecs", func(t *testing.T) {
		serviceRef := "1:0:0:0:0:0:0:0:0:0:/hdd/movie/rec1.ts"
		recordingID := recservice.EncodeRecordingID(serviceRef)

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
			Reason:         "transcode_all",
		}
		svc.On("ResolvePlayback", mock.Anything, recordingID, "generic").Return(res, nil)

		s := &Server{recordingsService: svc}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/v3/recordings/"+recordingID+"/stream-info", nil)

		s.GetRecordingPlaybackInfo(w, r, recordingID)

		require.Equal(t, http.StatusOK, w.Code)
		var dto testPlaybackInfoDTO
		err := json.Unmarshal(w.Body.Bytes(), &dto)
		assert.NoError(t, err)

		assert.Equal(t, Hls, dto.Mode)
		assert.Equal(t, "/api/v3/recordings/"+recordingID+"/playlist.m3u8", dto.Url)
		require.NotNil(t, dto.DurationSeconds)
		assert.Equal(t, int64(3600), *dto.DurationSeconds)
		expectedSrc := Store
		assert.Equal(t, &expectedSrc, dto.DurationSource)

		require.NotNil(t, dto.Container)
		assert.Equal(t, "mp4", *dto.Container)
		require.NotNil(t, dto.VideoCodec)
		assert.Equal(t, "h264", *dto.VideoCodec)
		require.NotNil(t, dto.AudioCodec)
		assert.Equal(t, "aac", *dto.AudioCodec)
	})
}

// Regression Test: ID Ownership (Double-Decode Prevention)
// CTO Mandate: Service Layer is the sole owner of decoding.
// Handler must pass through the raw ID. Service must reject non-Hex.
func TestGetRecordingPlaybackInfo_ID_Ownership_StrictHexRequirement(t *testing.T) {
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/hdd/movie/fail.ts"
	recordingID_Hex := recservice.EncodeRecordingID(serviceRef)

	svc := new(MockRecordingsService)

	// 1. Valid Path: Handler gets Hex -> Service gets Hex
	svc.On("ResolvePlayback", mock.Anything, recordingID_Hex, "generic").Return(recservice.PlaybackResolution{
		Strategy: recservice.StrategyDirect,
		CanSeek:  true,
		Reason:   recservice.ReasonDirectPlayMatch,
	}, nil).Once()

	s := &Server{recordingsService: svc}

	// Request with Hex
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v3/recordings/"+recordingID_Hex+"/stream-info", nil)
	s.GetRecordingPlaybackInfo(w, r, recordingID_Hex)
	assert.Equal(t, http.StatusOK, w.Code, "Hex ID must succeed")

	// 2. Invalid Path (The 'Double Decode' Trap):
	// If the handler were to decode the ID before passing it to the service,
	// the service would receive the Canonical ID.
	// Since we mandates strict Hex at the service boundary, the service would (correctly)
	// return an error if it tried to decode a already-decoded ID.
	svc.On("ResolvePlayback", mock.Anything, serviceRef, "generic").Return(recservice.PlaybackResolution{}, recservice.ErrInvalidArgument{Field: "recordingID", Reason: "invalid format"}).Once()

	w = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/api/v3/recordings/"+serviceRef+"/stream-info", nil)
	s.GetRecordingPlaybackInfo(w, r, serviceRef)

	// We expect 400 because the service layer (real or mock following the spec)
	// treats non-hex IDs as invalid format.
	assert.Equal(t, http.StatusBadRequest, w.Code, "Canonical ID passed to handler must fail at service boundary")
}

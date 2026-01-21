package v3

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/playback"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/log"
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
	args := m.Called(ctx, in)
	return args.Get(0).(recservice.StreamResult), args.Error(1)
}
func (m *MockRecordingsService) Delete(ctx context.Context, in recservice.DeleteInput) (recservice.DeleteResult, error) {
	return recservice.DeleteResult{}, nil
}
func (m *MockRecordingsService) GetMediaTruth(ctx context.Context, recordingID string) (playback.MediaTruth, error) {
	args := m.Called(ctx, recordingID)
	return args.Get(0).(playback.MediaTruth), args.Error(1)
}

func createTestServerDTO(svc recservice.Service) *Server {
	cfg := config.AppConfig{}
	cfg.FFmpeg.Bin = "/usr/bin/ffmpeg"
	cfg.HLS.Root = "/tmp/hls"
	s := &Server{cfg: cfg, recordingsService: svc}
	return s
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
			svc.On("GetMediaTruth", mock.Anything, recordingID).Return(playback.MediaTruth{}, tt.mockErr)

			s := createTestServerDTO(svc)
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
	type testPlaybackInfoDTO struct {
		Mode            PlaybackInfoMode            `json:"mode"`
		Url             *string                     `json:"url"`
		Seekable        *bool                       `json:"seekable,omitempty"`
		DurationSeconds *int64                      `json:"durationSeconds,omitempty"`
		DurationSource  *PlaybackInfoDurationSource `json:"duration_source,omitempty"`
		RequestId       string                      `json:"requestId"`
		SessionId       string                      `json:"sessionId"`

		Container  *string `json:"container,omitempty"`
		VideoCodec *string `json:"videoCodec,omitempty"`
		AudioCodec *string `json:"audioCodec,omitempty"`
	}

	t.Run("DTO_Mapping_UnknownDuration_UnknownCodecs", func(t *testing.T) {
		serviceRef := "1:0:0:0:0:0:0:0:0:0:/hdd/movie/rec1.ts"
		recordingID := recservice.EncodeRecordingID(serviceRef)

		svc := new(MockRecordingsService)
		// Decision engine now returns 422 if codecs are unknown/ambiguous
		svc.On("GetMediaTruth", mock.Anything, recordingID).Return(playback.MediaTruth{
			Container: "", VideoCodec: "", AudioCodec: "",
		}, nil)

		s := createTestServerDTO(svc)
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/v3/recordings/"+recordingID+"/stream-info", nil)

		s.GetRecordingPlaybackInfo(w, r, recordingID)

		require.Equal(t, http.StatusUnprocessableEntity, w.Code)
	})

	t.Run("DTO_Mapping_KnownDuration_KnownCodecs", func(t *testing.T) {
		serviceRef := "1:0:0:0:0:0:0:0:0:0:/hdd/movie/rec1.ts"
		recordingID := recservice.EncodeRecordingID(serviceRef)

		svc := new(MockRecordingsService)
		truth := playback.MediaTruth{
			Duration:   3600,
			Container:  "ts",
			VideoCodec: "vp9", // Force transcode on web_conservative
			AudioCodec: "aac",
		}
		svc.On("GetMediaTruth", mock.Anything, recordingID).Return(truth, nil)

		s := createTestServerDTO(svc)
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/v3/recordings/"+recordingID+"/stream-info", nil)
		r = r.WithContext(log.ContextWithRequestID(r.Context(), "test-req-123"))

		s.GetRecordingPlaybackInfo(w, r, recordingID)

		require.Equal(t, http.StatusOK, w.Code)
		var dto testPlaybackInfoDTO
		err := json.Unmarshal(w.Body.Bytes(), &dto)
		assert.NoError(t, err)

		// Expect HLS because VP9 is not in web_conservative (H264)
		assert.Equal(t, PlaybackInfoModeHls, dto.Mode)
		require.NotNil(t, dto.Url)
		assert.Equal(t, "/api/v3/recordings/"+recordingID+"/playlist.m3u8", *dto.Url)
		assert.NotEmpty(t, dto.RequestId)
		assert.NotEmpty(t, dto.SessionId)
	})
}

// Regression Test: ID Ownership
func TestGetRecordingPlaybackInfo_ID_Ownership_StrictHexRequirement(t *testing.T) {
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/hdd/movie/fail.ts"
	recordingID_Hex := recservice.EncodeRecordingID(serviceRef)

	svc := new(MockRecordingsService)

	svc.On("GetMediaTruth", mock.Anything, recordingID_Hex).Return(playback.MediaTruth{
		Container:  "mp4",
		VideoCodec: "h264",
		AudioCodec: "aac",
	}, nil).Once()

	s := createTestServerDTO(svc)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v3/recordings/"+recordingID_Hex+"/stream-info", nil)
	s.GetRecordingPlaybackInfo(w, r, recordingID_Hex)
	assert.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/api/v3/recordings/"+serviceRef+"/stream-info", nil)
	s.GetRecordingPlaybackInfo(w, r, serviceRef)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetRecordingPlaybackInfo_Deny_OptionB(t *testing.T) {
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/hdd/movie/deny.ts"
	recordingID := recservice.EncodeRecordingID(serviceRef)

	svc := new(MockRecordingsService)
	// Force a Deny decision via policy: AllowTranscode=false, Input needs transcode (flv)
	truth := playback.MediaTruth{
		Container:  "flv",
		VideoCodec: "h264",
		AudioCodec: "aac",
	}
	svc.On("GetMediaTruth", mock.Anything, recordingID).Return(truth, nil)

	// We use the real server to exercise the mapPlaybackInfoV2 logic
	cfg := config.AppConfig{}
	// FFmpeg bin empty -> serverCanTranscode = false -> allowTranscode = false
	s := &Server{cfg: cfg, recordingsService: svc}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v3/recordings/"+recordingID+"/stream-info", nil)
	s.GetRecordingPlaybackInfo(w, r, recordingID)

	require.Equal(t, http.StatusOK, w.Code)

	var raw map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &raw)
	assert.NoError(t, err)

	// 1. Legacy Mode is still direct_mp4
	assert.Equal(t, "direct_mp4", raw["mode"])
	// 2. URL is NIL (not present or null in JSON)
	assert.Nil(t, raw["url"])

	// 3. Decision sub-object is TRUTHFUL
	dec, ok := raw["decision"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "deny", dec["mode"])
	assert.Equal(t, "", dec["selectedOutputKind"]) // Invariant #11: Strict Empty
	assert.Empty(t, dec["outputs"])
}

package v3_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/pipeline/resume"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Helpers
func s(v string) *string { return &v }

type MockRecordingsService struct {
	mock.Mock
}

func (m *MockRecordingsService) ResolvePlayback(ctx context.Context, id string, profile string) (recordings.PlaybackResolution, error) {
	args := m.Called(ctx, id, profile)
	return args.Get(0).(recordings.PlaybackResolution), args.Error(1)
}

func (m *MockRecordingsService) List(ctx context.Context, in recordings.ListInput) (recordings.ListResult, error) {
	return recordings.ListResult{}, nil
}
func (m *MockRecordingsService) GetPlaybackInfo(ctx context.Context, in recordings.PlaybackInfoInput) (recordings.PlaybackInfoResult, error) {
	return recordings.PlaybackInfoResult{}, nil
}
func (m *MockRecordingsService) GetStatus(ctx context.Context, in recordings.StatusInput) (recordings.StatusResult, error) {
	return recordings.StatusResult{}, nil
}
func (m *MockRecordingsService) Stream(ctx context.Context, in recordings.StreamInput) (recordings.StreamResult, error) {
	return recordings.StreamResult{}, nil
}
func (m *MockRecordingsService) Delete(ctx context.Context, in recordings.DeleteInput) (recordings.DeleteResult, error) {
	return recordings.DeleteResult{}, nil
}

func (m *MockRecordingsService) GetMediaTruth(ctx context.Context, recordingID string) (playback.MediaTruth, error) {
	args := m.Called(ctx, recordingID)
	return args.Get(0).(playback.MediaTruth), args.Error(1)
}

type MockResumeStore struct {
	mock.Mock
}

func (m *MockResumeStore) Get(ctx context.Context, principalID, serviceRef string) (*resume.State, error) {
	return nil, nil // Not found
}
func (m *MockResumeStore) Put(ctx context.Context, principalID, serviceRef string, state *resume.State) error {
	return nil
}
func (m *MockResumeStore) Delete(ctx context.Context, principalID, serviceRef string) error {
	return nil
}
func (m *MockResumeStore) Close() error {
	return nil
}

// Valid recording ID for testing (Hex of a dummy service ref)
const validRecordingRef = "1:0:0:0:0:0:0:0:0:0:/hdd/movie/rec1.ts"

var validRecordingID = hex.EncodeToString([]byte(validRecordingRef))

func createTestServer(svc recordings.Service) *v3.Server {
	s_srv := v3.NewServer(config.AppConfig{}, nil, nil)
	s_srv.SetRecordingsService(svc)
	// Inject NOOP dependencies to avoid nil panics in mapping logic
	s_srv.SetDependencies(v3.Dependencies{
		ResumeStore:       new(MockResumeStore),
		RecordingsService: svc,
	})
	return s_srv
}

func TestContract_PlaybackInfo_Preparing(t *testing.T) {
	svc := new(MockRecordingsService)
	svc.On("GetMediaTruth", mock.Anything, validRecordingID).Return(playback.MediaTruth{}, recordings.ErrPreparing{RecordingID: validRecordingID})

	s_srv := createTestServer(svc)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v3/recordings/"+validRecordingID+"/stream-info", nil)

	s_srv.GetRecordingPlaybackInfo(w, r, validRecordingID)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Equal(t, "5", w.Header().Get("Retry-After"))

	var prob struct {
		Type  string `json:"type"`
		Title string `json:"title"`
		Code  string `json:"code"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &prob)
	require.NoError(t, err)
	assert.Equal(t, "recordings/preparing", prob.Type)
}

func TestContract_PlaybackInfo_Forbidden(t *testing.T) {
	svc := new(MockRecordingsService)
	svc.On("GetMediaTruth", mock.Anything, validRecordingID).Return(playback.MediaTruth{}, recordings.ErrForbidden{RequiredScopes: []string{"read"}})

	s_srv := createTestServer(svc)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v3/recordings/"+validRecordingID+"/stream-info", nil)

	s_srv.GetRecordingPlaybackInfo(w, r, validRecordingID)

	assert.Equal(t, http.StatusForbidden, w.Code)
	var prob struct {
		Type string `json:"type"`
	}
	json.Unmarshal(w.Body.Bytes(), &prob)
	assert.Equal(t, "recordings/forbidden", prob.Type)
}

func TestContract_PlaybackInfo_NotFound(t *testing.T) {
	svc := new(MockRecordingsService)
	svc.On("GetMediaTruth", mock.Anything, validRecordingID).Return(playback.MediaTruth{}, recordings.ErrNotFound{RecordingID: validRecordingID})

	s_srv := createTestServer(svc)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v3/recordings/"+validRecordingID+"/stream-info", nil)

	s_srv.GetRecordingPlaybackInfo(w, r, validRecordingID)

	assert.Equal(t, http.StatusNotFound, w.Code)
	var prob struct {
		Type string `json:"type"`
	}
	json.Unmarshal(w.Body.Bytes(), &prob)
	assert.Equal(t, "recordings/not-found", prob.Type)
}

func TestContract_PlaybackInfo_UpstreamError(t *testing.T) {
	svc := new(MockRecordingsService)
	svc.On("GetMediaTruth", mock.Anything, validRecordingID).Return(playback.MediaTruth{}, recordings.ErrUpstream{Op: "probe", Cause: errors.New("timeout")})

	s_srv := createTestServer(svc)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v3/recordings/"+validRecordingID+"/stream-info", nil)

	s_srv.GetRecordingPlaybackInfo(w, r, validRecordingID)

	assert.Equal(t, http.StatusBadGateway, w.Code)
	var prob struct {
		Type string `json:"type"`
	}
	json.Unmarshal(w.Body.Bytes(), &prob)
	assert.Equal(t, "recordings/upstream", prob.Type)
}

func TestInvariant_SuccessAlwaysSeekable(t *testing.T) {
	svc := new(MockRecordingsService)

	// Case 1: Direct Play
	svc.On("GetMediaTruth", mock.Anything, validRecordingID).Return(playback.MediaTruth{
		Container:  "mp4",
		VideoCodec: "h264",
		AudioCodec: "aac",
	}, nil).Once()

	s_srv := createTestServer(svc)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v3/recordings/"+validRecordingID+"/stream-info", nil)
	s_srv.GetRecordingPlaybackInfo(w, r, validRecordingID)

	assert.Equal(t, http.StatusOK, w.Code)
	var dto map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &dto)
	seekable, ok := dto["isSeekable"].(bool)
	assert.True(t, ok, "isSeekable field missing or not bool")
	assert.True(t, seekable, "isSeekable must be true for 200 responses (Fail Close otherwise)")

	// Case 2: HLS (Transcode)
	svc.On("GetMediaTruth", mock.Anything, validRecordingID).Return(playback.MediaTruth{
		Container:  "ts",
		VideoCodec: "h264",
		AudioCodec: "mp2",
	}, nil).Once()

	w = httptest.NewRecorder()
	s_srv.GetRecordingPlaybackInfo(w, r, validRecordingID)

	assert.Equal(t, http.StatusOK, w.Code)
	json.Unmarshal(w.Body.Bytes(), &dto)
	seekable, ok = dto["isSeekable"].(bool)
	assert.True(t, ok, "isSeekable field missing or not bool")
	assert.True(t, seekable, "isSeekable must be true for 200 responses (Fail Close otherwise)")
}

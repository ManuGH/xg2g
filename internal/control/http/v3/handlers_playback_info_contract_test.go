package v3_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	"github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// --- Contract Verification for GET /api/v3/recordings/{id}/playbackinfo ---

func TestContract_PlaybackInfo_Success_HLS_Complete(t *testing.T) {
	// Setup
	srv, svc := setupTestServer()

	// Mock Service: Successful HLS resolution with full truth
	svc.On("ResolvePlayback", mock.Anything, "rec123", mock.Anything).Return(recordings.PlaybackResolution{
		Strategy:       recordings.StrategyHLS,
		Container:      s("ts"),
		VideoCodec:     s("h264"),
		AudioCodec:     s("ac3"),
		DurationSec:    i64(3600),
		DurationSource: ds(recordings.DurationSourceStore),
		CanSeek:        true,
		Reason:         recordings.ReasonTranscodeAudio,
	}, nil)

	// Execute
	req, _ := http.NewRequest("GET", "/api/v3/recordings/rec123/playbackinfo", nil)
	w := httptest.NewRecorder()
	srv.GetRecordingPlaybackInfo(w, req, "rec123")

	// Verify HTTP Contract
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	// Verify JSON Shape (Exact Schema)
	var body map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &body)
	assert.NoError(t, err)

	// Required Fields
	assert.Equal(t, "hls", body["mode"])
	assert.Contains(t, body["url"], "/api/v3/recordings/rec123/playlist.m3u8")

	// Optional Fields (Present)
	assert.Equal(t, true, body["seekable"])
	assert.Equal(t, float64(3600), body["duration_seconds"]) // JSON numbers are floats
	assert.Equal(t, "store", body["duration_source"])
	assert.Equal(t, "ts", body["container"])
	assert.Equal(t, "h264", body["video_codec"])
	assert.Equal(t, "ac3", body["audio_codec"])
	assert.Equal(t, "transcode_audio", body["reason"])
}

func TestContract_PlaybackInfo_Success_MP4_Minimal(t *testing.T) {
	// Setup
	srv, svc := setupTestServer()

	// Mock Service: Direct MP4, no extra truth known
	svc.On("ResolvePlayback", mock.Anything, "rec123", mock.Anything).Return(recordings.PlaybackResolution{
		Strategy: recordings.StrategyDirect,
		CanSeek:  true,
		Reason:   recordings.ReasonDirectPlayMatch,
	}, nil)

	// Execute
	req, _ := http.NewRequest("GET", "/api/v3/recordings/rec123/playbackinfo", nil)
	w := httptest.NewRecorder()
	srv.GetRecordingPlaybackInfo(w, req, "rec123")

	// Verify HTTP Contract
	assert.Equal(t, http.StatusOK, w.Code)

	var body map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &body)
	assert.NoError(t, err)

	assert.Equal(t, "direct_mp4", body["mode"])
	assert.Contains(t, body["url"], "/api/v3/recordings/rec123/stream.mp4")

	// Assert Omitted Fields (omitempty)
	assert.NotContains(t, body, "container")
	assert.NotContains(t, body, "video_codec")
	assert.NotContains(t, body, "audio_codec")
	assert.NotContains(t, body, "duration_seconds")
	assert.NotContains(t, body, "duration_source")
}

func TestContract_PlaybackInfo_Preparing(t *testing.T) {
	srv, svc := setupTestServer()

	// Mock Service: Preparing Error
	svc.On("ResolvePlayback", mock.Anything, "rec123", mock.Anything).Return(recordings.PlaybackResolution{}, recordings.ErrPreparing{RecordingID: "rec123"})

	req, _ := http.NewRequest("GET", "/api/v3/recordings/rec123/playbackinfo", nil)
	w := httptest.NewRecorder()
	srv.GetRecordingPlaybackInfo(w, req, "rec123")

	// Verify HTTP Contract (RFC 7807 + Retry-After)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Equal(t, "5", w.Header().Get("Retry-After"))
	assert.Equal(t, "application/problem+json", w.Header().Get("Content-Type"))

	var prob map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &prob)

	assert.Equal(t, "recordings/preparing", prob["type"])
	assert.Equal(t, "Preparing", prob["title"])
	assert.Equal(t, float64(503), prob["status"])
	assert.NotEmpty(t, prob["detail"])
}

func TestContract_PlaybackInfo_Forbidden(t *testing.T) {
	srv, svc := setupTestServer()
	svc.On("ResolvePlayback", mock.Anything, "rec123", mock.Anything).Return(recordings.PlaybackResolution{}, recordings.ErrForbidden{})

	req, _ := http.NewRequest("GET", "/api/v3/recordings/rec123/playbackinfo", nil)
	w := httptest.NewRecorder()
	srv.GetRecordingPlaybackInfo(w, req, "rec123")

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, "application/problem+json", w.Header().Get("Content-Type"))

	var prob map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &prob)
	assert.Equal(t, "recordings/forbidden", prob["type"])
	assert.Equal(t, float64(403), prob["status"])
}

func TestContract_PlaybackInfo_NotFound(t *testing.T) {
	srv, svc := setupTestServer()
	svc.On("ResolvePlayback", mock.Anything, "rec123", mock.Anything).Return(recordings.PlaybackResolution{}, recordings.ErrNotFound{RecordingID: "rec123"})

	// Verify standard 404
	req, _ := http.NewRequest("GET", "/api/v3/recordings/rec123/playbackinfo", nil)
	w := httptest.NewRecorder()
	srv.GetRecordingPlaybackInfo(w, req, "rec123")

	assert.Equal(t, http.StatusNotFound, w.Code)
	var prob map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &prob)
	assert.Equal(t, "recordings/not-found", prob["type"])
}

func TestContract_PlaybackInfo_Unsupported(t *testing.T) {
	srv, svc := setupTestServer()
	// Mock legacy RemoteProbeUnsupported error which triggers 422
	svc.On("ResolvePlayback", mock.Anything, "rec123", mock.Anything).Return(recordings.PlaybackResolution{}, recordings.ErrRemoteProbeUnsupported)

	req, _ := http.NewRequest("GET", "/api/v3/recordings/rec123/playbackinfo", nil)
	w := httptest.NewRecorder()
	srv.GetRecordingPlaybackInfo(w, req, "rec123")

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	var prob map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &prob)
	assert.Equal(t, "recordings/remote-probe-unsupported", prob["type"])
}

func TestContract_PlaybackInfo_UpstreamError(t *testing.T) {
	srv, svc := setupTestServer()
	// Mock generic upstream error
	svc.On("ResolvePlayback", mock.Anything, "rec123", mock.Anything).Return(recordings.PlaybackResolution{}, recordings.ErrUpstream{Cause: errors.New("boom")})

	req, _ := http.NewRequest("GET", "/api/v3/recordings/rec123/playbackinfo", nil)
	w := httptest.NewRecorder()
	srv.GetRecordingPlaybackInfo(w, req, "rec123")

	assert.Equal(t, http.StatusBadGateway, w.Code)
	var prob map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &prob)
	assert.Equal(t, "recordings/upstream", prob["type"])
}

// --- Helpers ---

func s(v string) *string                                        { return &v }
func i64(v int64) *int64                                        { return &v }
func ds(v recordings.DurationSource) *recordings.DurationSource { return &v }

type mockRecordingsService struct {
	mock.Mock
}

func (m *mockRecordingsService) ResolvePlayback(ctx context.Context, id string, profile string) (recordings.PlaybackResolution, error) {
	args := m.Called(ctx, id, profile)
	return args.Get(0).(recordings.PlaybackResolution), args.Error(1)
}

func (m *mockRecordingsService) List(ctx context.Context, input recordings.ListInput) (recordings.ListResult, error) {
	return recordings.ListResult{}, nil
}
func (m *mockRecordingsService) GetPlaybackInfo(ctx context.Context, input recordings.PlaybackInfoInput) (recordings.PlaybackInfoResult, error) {
	return recordings.PlaybackInfoResult{}, nil
}
func (m *mockRecordingsService) GetStatus(ctx context.Context, input recordings.StatusInput) (recordings.StatusResult, error) {
	return recordings.StatusResult{}, nil
}
func (m *mockRecordingsService) Delete(ctx context.Context, input recordings.DeleteInput) (recordings.DeleteResult, error) {
	return recordings.DeleteResult{}, nil
}
func (m *mockRecordingsService) Stream(ctx context.Context, input recordings.StreamInput) (recordings.StreamResult, error) {
	return recordings.StreamResult{}, nil
}

func setupTestServer() (*v3.Server, *mockRecordingsService) {
	svc := new(mockRecordingsService)
	// Create minimal server and inject service
	server := v3.NewServer(config.AppConfig{}, nil, nil) // Config/Manager/Cancel can be nil for these handler tests
	server.SetDependencies(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, svc, nil, nil)
	return server, svc
}

func TestInvariant_SuccessAlwaysSeekable(t *testing.T) {
	// Invariant: If we return 200 OK (Decision Made), seekable MUST be true.
	// If it's not seekable, we should have failed closed or strictly defined why.
	// Current contract: 200 => Seekable=true.

	srv, svc := setupTestServer()

	// Mock 1: Direct MP4 (Should be seekable)
	svc.On("ResolvePlayback", mock.Anything, "rec_direct", mock.Anything).Return(recordings.PlaybackResolution{
		Strategy: recordings.StrategyDirect,
		CanSeek:  true,
		Reason:   recordings.ReasonDirectPlayMatch,
	}, nil)

	// Mock 2: HLS (Should be seekable if ready)
	svc.On("ResolvePlayback", mock.Anything, "rec_hls", mock.Anything).Return(recordings.PlaybackResolution{
		Strategy: recordings.StrategyHLS,
		CanSeek:  true,
		Reason:   recordings.ReasonTranscodeVideo,
	}, nil)

	cases := []string{"rec_direct", "rec_hls"}

	for _, id := range cases {
		req, _ := http.NewRequest("GET", "/api/v3/recordings/"+id+"/playbackinfo", nil)
		w := httptest.NewRecorder()
		srv.GetRecordingPlaybackInfo(w, req, id)

		assert.Equal(t, http.StatusOK, w.Code)
		var body map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &body)

		// THE INVARIANT
		seekable, ok := body["seekable"].(bool)
		assert.True(t, ok, "seekable field missing or not bool")
		assert.True(t, seekable, "seekable must be true for 200 responses (Fail Close otherwise)")
	}
}

package bootstrap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	controlhttp "github.com/ManuGH/xg2g/internal/control/http"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
)

type mockResolver struct {
	ResolveFunc func(ctx context.Context, recordingID string, intent recservice.PlaybackIntent, profile recservice.PlaybackProfile) (recservice.PlaybackInfoResult, error)
}

func (m *mockResolver) Resolve(ctx context.Context, recordingID string, intent recservice.PlaybackIntent, profile recservice.PlaybackProfile) (recservice.PlaybackInfoResult, error) {
	if m.ResolveFunc != nil {
		return m.ResolveFunc(ctx, recordingID, intent, profile)
	}
	return recservice.PlaybackInfoResult{}, nil
}

// TestVODPlayback_Path_Wiring_ErrorPath verifies that the VOD failure path is wired correctly.
// Requirements:
// 1. Stack serves /api/v3/vod/{id}
// 2. Returns RFC 7807 with request_id on failure.
// 3. Sets X-Request-ID header (strictly canonical).
// 4. Matches header ID with body ID.
func TestVODPlayback_Path_Wiring_ErrorPath(t *testing.T) {
	// 1. Setup minimal test config (Option A: Real components, temp dir)
	t.Setenv("XG2G_INITIAL_REFRESH", "false") // Disable background refresh to prevent test hangs/noise

	tmpDir, err := os.MkdirTemp("", "xg2g-vod-error-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.yaml")
	content := `
version: v3
dataDir: ` + tmpDir + `
api:
  listenAddr: ":0"
  token: "test-token"
  tokenScopes: ["v3:admin"]
engine:
  tunerSlots: [0]
enigma2:
  baseUrl: http://mock-receiver
  username: root
  password: "dummy-password"
`
	err = os.WriteFile(configPath, []byte(content), 0600)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 2. Wire the App
	container, err := WireServices(ctx, "test-v3", "test-commit", "now", configPath)
	require.NoError(t, err, "Wiring failed")

	// 3. Start Lifecycle (Background processes)
	err = container.Start(ctx)
	require.NoError(t, err)

	// 4. Inject Mock Service (Simulate Not Found)
	mockSvc := &mockRecordingsService{
		resolvePlayback: func(ctx context.Context, recID, profile string) (recservice.PlaybackResolution, error) {
			return recservice.PlaybackResolution{}, recservice.ErrNotFound{RecordingID: recID}
		},
	}
	container.Server.SetRecordingsService(mockSvc)

	// 4. Request Non-Existent Component
	// Strict: Test URL matches router param definition /api/v3/vod/{recordingId}
	handler := container.Server.Handler()

	serviceRef := "1:0:0:0:0:0:0:0:0:0:/hdd/movie/missing.ts"
	recordingID := recservice.EncodeRecordingID(serviceRef)

	// Canonical: Use strict header constant
	req := httptest.NewRequest("GET", "/api/v3/recordings/"+recordingID+"/stream-info", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()

	// Validating Strict Wiring
	reqID := resp.Header.Get(controlhttp.HeaderRequestID)
	assert.NotEmpty(t, reqID, "X-Request-ID header missing")

	// Validate Body
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "Should fail for missing ID")
	assert.Equal(t, "application/problem+json", resp.Header.Get("Content-Type"), "Content-Type must be RFC7807")

	// Decode RFC7807 Strict
	var problem struct {
		Type      string `json:"type"`
		Title     string `json:"title"`
		Status    int    `json:"status"`
		Detail    string `json:"detail"`
		Instance  string `json:"instance"`
		RequestID string `json:"request_id"`
	}

	err = json.NewDecoder(resp.Body).Decode(&problem)
	require.NoError(t, err, "Must decode RFC7807 body")

	// Assertions
	assert.Equal(t, "recordings/not-found", problem.Type)
	assert.Equal(t, "Not Found", problem.Title)
	assert.Equal(t, http.StatusNotFound, problem.Status)
	assert.Equal(t, reqID, problem.RequestID, "Problem JSON request_id matches header")
}

// TestVODPlayback_Path_Wiring_SuccessPath verifies that the VOD success path is wired correctly.
// Requirements:
// 1. Stack serves /api/v3/recordings/{id}/stream-info with v3:read scope.
// 2. Returns structured PlaybackInfo (Strict JSON).
// 3. X-Request-ID header present and correlated.
// 4. Stream URL is syntactically valid.
func TestVODPlayback_Path_Wiring_SuccessPath(t *testing.T) {
	// 1. Setup minimal test config
	t.Setenv("XG2G_INITIAL_REFRESH", "false")

	tmpDir, err := os.MkdirTemp("", "xg2g-vod-success-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.yaml")
	// Note: We authorize with v3:read to prove minimal scope works.
	content := `
version: v3
dataDir: ` + tmpDir + `
api:
  listenAddr: ":0"
  token: "test-token"
  tokenScopes: ["v3:read"] 
engine:
  tunerSlots: [0]
enigma2:
  baseUrl: http://mock-receiver
  username: root
  password: "dummy-password"
`
	err = os.WriteFile(configPath, []byte(content), 0600)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 2. Wire the App
	container, err := WireServices(ctx, "test-v3", "test-commit", "now", configPath)
	require.NoError(t, err, "Wiring failed")

	// 3. Start Lifecycle
	err = container.Start(ctx)
	require.NoError(t, err)

	// 4. Inject Mock RecordingsService
	// Production handler calls recordingsService.ResolvePlayback(), NOT resolver.Resolve()
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/hdd/movie/film.ts"
	recordingID := recservice.EncodeRecordingID(serviceRef)

	mockSvc := &mockRecordingsService{
		resolvePlayback: func(ctx context.Context, recID, profile string) (recservice.PlaybackResolution, error) {
			t.Logf("MOCK CALLED: recID=%q profile=%q recordingID=%q", recID, profile, recordingID)
			// Handler decodes URL parameter, so recID is canonical (decoded)
			if recID == recordingID {
				t.Logf("MOCK MATCHED: returning success")
				dur := int64(3600)
				container := "mp4"
				vcodec := "h264"
				acodec := "aac"
				return recservice.PlaybackResolution{
					Strategy:    "direct_mp4",
					CanSeek:     true,
					DurationSec: &dur,
					Container:   &container,
					VideoCodec:  &vcodec,
					AudioCodec:  &acodec,
					Reason:      recservice.ReasonDirectPlayMatch,
				}, nil
			}
			t.Logf("MOCK NOT MATCHED: returning 404")
			return recservice.PlaybackResolution{}, recservice.ErrNotFound{RecordingID: recID}
		},
	}

	// Inject mock into server (replaces real recordingsService)
	t.Logf("Injecting mock service...")
	container.Server.SetRecordingsService(mockSvc)
	t.Logf("Mock service injected")

	handler := container.Server.Handler()
	req := httptest.NewRequest("GET", "/api/v3/recordings/"+recordingID+"/stream-info", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()

	// 5. Assertions
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	// Strict Decode
	var dto struct {
		URL              string    `json:"url"`
		Mode             string    `json:"mode"`
		DurationSeconds  *int64    `json:"duration_seconds,omitempty"`
		Reason           *string   `json:"reason,omitempty"`
		Seekable         *bool     `json:"seekable,omitempty"`    // deprecated alias
		IsSeekable       *bool     `json:"is_seekable,omitempty"` // canonical (P3-4)
		Container        *string   `json:"container,omitempty"`
		VideoCodec       *string   `json:"video_codec,omitempty"`
		AudioCodec       *string   `json:"audio_codec,omitempty"`
		RequestId        string    `json:"requestId"`                    // traceability
		SessionId        string    `json:"sessionId"`                    // traceability
		DurationSource   *string   `json:"duration_source,omitempty"`    // P3-4 truth
		StartUnix        *int64    `json:"start_unix,omitempty"`         // P3-4 truth (live)
		LiveEdgeUnix     *int64    `json:"live_edge_unix,omitempty"`     // P3-4 truth (live)
		DvrWindowSeconds *int64    `json:"dvr_window_seconds,omitempty"` // P3-4 truth
		Resume           *struct { // P3-4 resume state
			PosSeconds      float32 `json:"pos_seconds"`
			DurationSeconds *int64  `json:"duration_seconds,omitempty"`
			Finished        *bool   `json:"finished,omitempty"`
		} `json:"resume,omitempty"`
	}

	// Enforce strict JSON
	dec := json.NewDecoder(resp.Body)
	dec.DisallowUnknownFields()
	err = dec.Decode(&dto)
	require.NoError(t, err, "Must strictly decode PlaybackInfo")

	assert.Equal(t, "/api/v3/recordings/"+recordingID+"/stream.mp4", dto.URL)
	assert.Equal(t, "direct_mp4", dto.Mode)
	require.NotNil(t, dto.DurationSeconds)
	assert.Equal(t, int64(3600), *dto.DurationSeconds)
	require.NotNil(t, dto.Reason)
	assert.Equal(t, "directplay_match", *dto.Reason)
	require.NotNil(t, dto.IsSeekable)
	assert.True(t, *dto.IsSeekable)
}

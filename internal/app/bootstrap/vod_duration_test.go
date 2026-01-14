package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockProber struct {
	ProbeFunc func(ctx context.Context, path string) (*vod.StreamInfo, error)
}

func (m *mockProber) Probe(ctx context.Context, path string) (*vod.StreamInfo, error) {
	if m.ProbeFunc != nil {
		return m.ProbeFunc(ctx, path)
	}
	return nil, nil
}

func TestVODPlayback_DurationTruth(t *testing.T) {
	t.Setenv("XG2G_INITIAL_REFRESH", "false")

	tmpDir, err := os.MkdirTemp("", "xg2g-vod-duration-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	moviePath := filepath.Join(tmpDir, "movies", "film.ts")
	err = os.MkdirAll(filepath.Dir(moviePath), 0750)
	require.NoError(t, err)
	err = os.WriteFile(moviePath, []byte("fake-stream"), 0600)
	require.NoError(t, err)

	configPath := filepath.Join(tmpDir, "config.yaml")
	content := `
version: v3
dataDir: ` + tmpDir + `
api:
  listenAddr: ":0"
  token: "test-token"
  tokenScopes: ["v3:read"]
library:
  enabled: true
  db_path: ` + filepath.Join(tmpDir, "lib.db") + `
  roots:
    - id: root1
      path: ` + filepath.Join(tmpDir, "movies") + `
      type: local
      include_ext: [".ts"]
recordingPathMappings:
  - receiver_root: /hdd/movie
    local_root: ` + filepath.Join(tmpDir, "movies") + `
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

	container, err := WireServices(ctx, "test-v3", "test-commit", "now", configPath)
	require.NoError(t, err)
	err = container.Start(ctx)
	require.NoError(t, err)

	serviceRef := "1:0:0:0:0:0:0:0:0:0:/hdd/movie/film.ts"
	recordingID := recservice.EncodeRecordingID(serviceRef)

	v3Handler := container.Server.Handler()

	t.Run("FallbackPath_ProbeAndPersist", func(t *testing.T) {
		// 1. Setup Mock Prober
		probeCalled := 0
		mock := &mockProber{
			ProbeFunc: func(ctx context.Context, path string) (*vod.StreamInfo, error) {
				probeCalled++
				return &vod.StreamInfo{
					Video:     vod.VideoStreamInfo{Duration: 123, CodecName: "h264"},
					Audio:     vod.AudioStreamInfo{CodecName: "aac"},
					Container: "mpegts",
				}, nil
			},
		}
		container.Server.SetVODProber(mock)

		// 2. First Request: Should trigger probe
		req := httptest.NewRequest("GET", "/api/v3/recordings/"+recordingID+"/stream-info", nil)
		req.Header.Set("Authorization", "Bearer test-token")
		w := httptest.NewRecorder()
		v3Handler.ServeHTTP(w, req)

		// Accept both 200 (if probe completes immediately) or 503 (preparing)
		// Modern async probing may return 503 with Retry-After
		if w.Code == http.StatusServiceUnavailable {
			// 503 = Preparing, verify it returns eventually
			require.Equal(t, "application/problem+json", w.Header().Get("Content-Type"))
			// Retry the request (probe should complete)
			time.Sleep(100 * time.Millisecond)
			w = httptest.NewRecorder()
			v3Handler.ServeHTTP(w, req)
		}

		require.Equal(t, http.StatusOK, w.Code)
		var resp struct {
			DurationSeconds int64 `json:"duration_seconds"`
		}
		err = json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, int64(123), resp.DurationSeconds)
		assert.Equal(t, 1, probeCalled, "Probe should have been called once")

		// 3. Verify Persistence in Store
		libSvc := container.Server.LibraryService()
		require.NotNil(t, libSvc)
		item, err := libSvc.GetStore().GetItem(ctx, "root1", "film.ts")
		require.NoError(t, err)
		require.NotNil(t, item)
		assert.Equal(t, int64(123), item.DurationSeconds, "Duration should be persisted")
	})

	t.Run("SuccessPath_FromStore", func(t *testing.T) {
		// Clear VOD Manager's in-memory cache to force store lookup
		container.Server.VODManager().MarkUnknown(serviceRef)

		// Setup Mock Prober that should NOT be called
		probeCalled := 0
		mock := &mockProber{
			ProbeFunc: func(ctx context.Context, path string) (*vod.StreamInfo, error) {
				probeCalled++
				return nil, fmt.Errorf("should not be called")
			},
		}
		container.Server.SetVODProber(mock)

		// Request again: Should use persisted duration from Store
		req := httptest.NewRequest("GET", "/api/v3/recordings/"+recordingID+"/stream-info", nil)
		req.Header.Set("Authorization", "Bearer test-token")
		w := httptest.NewRecorder()
		v3Handler.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		var resp struct {
			DurationSeconds int64 `json:"duration_seconds"`
		}
		err = json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, int64(123), resp.DurationSeconds)
		assert.Equal(t, 0, probeCalled, "Probe should NOT have been called")
	})
}

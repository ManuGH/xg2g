package v3

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/control/http/v3/recordings/artifacts"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockArtifactsResolver helpers
type MockArtifactsResolver struct {
	mock.Mock
}

func (m *MockArtifactsResolver) ResolvePlaylist(ctx context.Context, recordingID, profile string) (artifacts.ArtifactOK, *artifacts.ArtifactError) {
	args := m.Called(ctx, recordingID, profile)
	if args.Get(1) != nil {
		return artifacts.ArtifactOK{}, args.Get(1).(*artifacts.ArtifactError)
	}
	return args.Get(0).(artifacts.ArtifactOK), nil
}

func (m *MockArtifactsResolver) ResolveSegment(ctx context.Context, recordingID, segment string) (artifacts.ArtifactOK, *artifacts.ArtifactError) {
	return artifacts.ArtifactOK{}, nil
}
func (m *MockArtifactsResolver) ResolveTimeshift(ctx context.Context, recordingID, profile string) (artifacts.ArtifactOK, *artifacts.ArtifactError) {
	return artifacts.ArtifactOK{}, nil
}

func TestGetRecordingPlaybackInfo_P3_4_SegmentTruth(t *testing.T) {
	type testPlaybackInfoDTO struct {
		IsSeekable       *bool  `json:"is_seekable"`
		StartUnix        *int64 `json:"start_unix"`
		LiveEdgeUnix     *int64 `json:"live_edge_unix"`
		DvrWindowSeconds *int64 `json:"dvr_window_seconds"`
		SessionId        string `json:"sessionId"`
		RequestId        string `json:"requestId"`
	}

	t.Run("Traceability_Propagation", func(t *testing.T) {
		// Scenario: Verify SessionId namespacing and RequestId from Context
		recordingID := recservice.EncodeRecordingID("1:0:0:0:0:0:0:0:0:0:/hdd/movie/trace.ts")

		svc := new(MockRecordingsService)
		// Mock logic: DirectMp4 resolution (simplest path to verify DTO enrichment)
		svc.On("ResolvePlayback", mock.Anything, recordingID, "generic").Return(recservice.PlaybackResolution{
			Strategy: recservice.StrategyDirect,
			CanSeek:  true,
		}, nil)

		s := &Server{recordingsService: svc}

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/v3/recordings/"+recordingID+"/stream-info", nil)

		// Inject RequestID into Context (mocking middleware)
		ctx := log.ContextWithRequestID(r.Context(), "req-test-123")
		r = r.WithContext(ctx)

		s.GetRecordingPlaybackInfo(w, r, recordingID)

		require.Equal(t, http.StatusOK, w.Code)
		var dto testPlaybackInfoDTO
		json.Unmarshal(w.Body.Bytes(), &dto)

		assert.Equal(t, "req-test-123", dto.RequestId, "RequestId must come from context")
		assert.Equal(t, "rec:"+recordingID, dto.SessionId, "SessionId must be namespaced")
	})

	t.Run("Live_FailClosed_ImplausibleWindow", func(t *testing.T) {
		// Scenario: Live stream where edge calculation implies zero/neg window (e.g. single 0s segment)
		// Expectation: Seekable=false (Fail-Closed)

		recordingID := recservice.EncodeRecordingID("1:0:0:0:0:0:0:0:0:0:/hdd/zero_window.ts")

		svc := new(MockRecordingsService)
		svc.On("ResolvePlayback", mock.Anything, recordingID, "generic").Return(recservice.PlaybackResolution{
			Strategy: recservice.StrategyHLS,
			CanSeek:  true,
		}, nil)

		// Playlist: Live, 1 segment, 0 duration? (Edge case)
		playlist := `#EXTM3U
#EXT-X-TARGETDURATION:10
#EXT-X-PROGRAM-DATE-TIME:2024-01-01T12:00:00Z
#EXTINF:0.0,
seg1.ts`

		art := new(MockArtifactsResolver)
		art.On("ResolvePlaylist", mock.Anything, recordingID, "generic").Return(artifacts.ArtifactOK{
			Data: []byte(playlist),
		}, nil)

		s := &Server{recordingsService: svc, artifacts: art}

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/v3/recordings/"+recordingID+"/stream-info", nil)
		s.GetRecordingPlaybackInfo(w, r, recordingID)

		require.Equal(t, http.StatusOK, w.Code)
		var dto testPlaybackInfoDTO
		json.Unmarshal(w.Body.Bytes(), &dto)

		require.NotNil(t, dto.IsSeekable)
		assert.False(t, *dto.IsSeekable, "Must fail-closed for implausible/zero window")
	})

	t.Run("Live_FailClosed_PartialPDT", func(t *testing.T) {
		// Scenario: Live stream (no ENDLIST) with missing PDT on one segment
		// Expectation: Seekable=false due to strict check

		recordingID := recservice.EncodeRecordingID("1:0:0:0:0:0:0:0:0:0:/hdd/fail_live.ts")

		// 1. Mock Service Resolution (HLS)
		svc := new(MockRecordingsService)
		svc.On("ResolvePlayback", mock.Anything, recordingID, "generic").Return(recservice.PlaybackResolution{
			Strategy: recservice.StrategyHLS,
			CanSeek:  true, // Service thinks it's seekable (file exists), but Truth says otherwise
		}, nil)

		// 2. Mock Artifact (Broken Playlist)
		// Segment 2 missing PDT
		playlist := `#EXTM3U
#EXT-X-TARGETDURATION:10
#EXT-X-PROGRAM-DATE-TIME:2024-01-01T12:00:00Z
#EXTINF:10.0,
seg1.ts
#EXTINF:10.0,
seg2.ts`

		art := new(MockArtifactsResolver)
		art.On("ResolvePlaylist", mock.Anything, recordingID, "generic").Return(artifacts.ArtifactOK{
			Data: []byte(playlist),
		}, nil)

		s := &Server{
			recordingsService: svc,
			artifacts:         art,
		}

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/v3/recordings/"+recordingID+"/stream-info", nil)
		s.GetRecordingPlaybackInfo(w, r, recordingID)

		require.Equal(t, http.StatusOK, w.Code)
		var dto testPlaybackInfoDTO
		json.Unmarshal(w.Body.Bytes(), &dto)

		require.NotNil(t, dto.IsSeekable)
		assert.False(t, *dto.IsSeekable, "Must fail-closed to false for broken live")
		assert.Nil(t, dto.StartUnix)
	})

	t.Run("VOD_Robust_NoPDT", func(t *testing.T) {
		// Scenario: VOD stream (ENDLIST) with NO PDT
		// Expectation: Seekable=true (Robust), Window=Duration, Unix=Nil

		recordingID := recservice.EncodeRecordingID("1:0:0:0:0:0:0:0:0:0:/hdd/vod.ts")

		svc := new(MockRecordingsService)
		svc.On("ResolvePlayback", mock.Anything, recordingID, "generic").Return(recservice.PlaybackResolution{
			Strategy: recservice.StrategyHLS,
			CanSeek:  true,
		}, nil)

		// Playlist: VOD, 20s duration
		playlist := `#EXTM3U
#EXT-X-TARGETDURATION:10
#EXTINF:10.0,
seg1.ts
#EXTINF:10.0,
seg2.ts
#EXT-X-ENDLIST`

		art := new(MockArtifactsResolver)
		art.On("ResolvePlaylist", mock.Anything, recordingID, "generic").Return(artifacts.ArtifactOK{
			Data: []byte(playlist),
		}, nil)

		s := &Server{recordingsService: svc, artifacts: art}

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/v3/recordings/"+recordingID+"/stream-info", nil)
		s.GetRecordingPlaybackInfo(w, r, recordingID)

		require.Equal(t, http.StatusOK, w.Code)
		var dto testPlaybackInfoDTO
		json.Unmarshal(w.Body.Bytes(), &dto)

		require.NotNil(t, dto.IsSeekable)
		assert.True(t, *dto.IsSeekable, "VOD must be seekable even without PDT")

		require.NotNil(t, dto.DvrWindowSeconds)
		assert.Equal(t, int64(20), *dto.DvrWindowSeconds)

		assert.Nil(t, dto.StartUnix, "Unix fields should be nil for VOD without PDT")
	})

	t.Run("Live_Valid_FullTruth", func(t *testing.T) {
		// Scenario: Live stream with valid PDT
		// Expectation: Seekable=true, Unix fields populated

		recordingID := recservice.EncodeRecordingID("1:0:0:0:0:0:0:0:0:0:/hdd/live.ts")

		svc := new(MockRecordingsService)
		svc.On("ResolvePlayback", mock.Anything, recordingID, "generic").Return(recservice.PlaybackResolution{
			Strategy: recservice.StrategyHLS,
			CanSeek:  true,
		}, nil)

		// Playlist: Live, 20s span
		// Start: 12:00:00Z
		// End: 12:00:20Z (Last Start 12:00:10 + 10s)
		playlist := `#EXTM3U
#EXT-X-TARGETDURATION:10
#EXT-X-PROGRAM-DATE-TIME:2024-01-01T12:00:00Z
#EXTINF:10.0,
seg1.ts
#EXT-X-PROGRAM-DATE-TIME:2024-01-01T12:00:10Z
#EXTINF:10.0,
seg2.ts`

		art := new(MockArtifactsResolver)
		art.On("ResolvePlaylist", mock.Anything, recordingID, "generic").Return(artifacts.ArtifactOK{
			Data: []byte(playlist),
		}, nil)

		s := &Server{recordingsService: svc, artifacts: art}

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/v3/recordings/"+recordingID+"/stream-info", nil)
		s.GetRecordingPlaybackInfo(w, r, recordingID)

		require.Equal(t, http.StatusOK, w.Code)
		var dto testPlaybackInfoDTO
		json.Unmarshal(w.Body.Bytes(), &dto)

		require.NotNil(t, dto.IsSeekable)
		assert.True(t, *dto.IsSeekable)

		require.NotNil(t, dto.StartUnix)
		start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC).Unix()
		assert.Equal(t, start, *dto.StartUnix)

		require.NotNil(t, dto.LiveEdgeUnix)
		end := time.Date(2024, 1, 1, 12, 0, 20, 0, time.UTC).Unix()
		assert.Equal(t, end, *dto.LiveEdgeUnix)

		require.NotNil(t, dto.DvrWindowSeconds)
		assert.Equal(t, int64(20), *dto.DvrWindowSeconds)
	})
}

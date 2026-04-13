package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeProgramDateTimeLine(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Already RFC3339 (Z)",
			input:    "#EXT-X-PROGRAM-DATE-TIME:2026-01-04T16:17:53.066Z",
			expected: "#EXT-X-PROGRAM-DATE-TIME:2026-01-04T16:17:53.066Z",
		},
		{
			name:     "Already RFC3339 (Colon Offset)",
			input:    "#EXT-X-PROGRAM-DATE-TIME:2026-01-04T16:17:53.066+00:00",
			expected: "#EXT-X-PROGRAM-DATE-TIME:2026-01-04T16:17:53.066+00:00",
		},
		{
			name:     "Fix +0000 to Z",
			input:    "#EXT-X-PROGRAM-DATE-TIME:2026-01-04T16:17:53.066+0000",
			expected: "#EXT-X-PROGRAM-DATE-TIME:2026-01-04T16:17:53.066Z",
		},
		{
			name:     "Fix +HHMM to +HH:MM",
			input:    "#EXT-X-PROGRAM-DATE-TIME:2026-01-04T16:17:53.066+0130",
			expected: "#EXT-X-PROGRAM-DATE-TIME:2026-01-04T16:17:53.066+01:30",
		},
		{
			name:     "Fix -HHMM to -HH:MM",
			input:    "#EXT-X-PROGRAM-DATE-TIME:2026-01-04T16:17:53.066-0500",
			expected: "#EXT-X-PROGRAM-DATE-TIME:2026-01-04T16:17:53.066-05:00",
		},
		{
			name:     "Ignore Non-PDT Lines",
			input:    "#EXTINF:2.000000,",
			expected: "#EXTINF:2.000000,",
		},
		{
			name:     "Ignore Malformed PDT",
			input:    "#EXT-X-PROGRAM-DATE-TIME:invalid-date",
			expected: "#EXT-X-PROGRAM-DATE-TIME:invalid-date",
		},
		{
			name:     "Trims trailing whitespace when normalizing",
			input:    "#EXT-X-PROGRAM-DATE-TIME:2026-01-04T16:17:53.066+0000   ",
			expected: "#EXT-X-PROGRAM-DATE-TIME:2026-01-04T16:17:53.066Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeProgramDateTimeLine(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// MockStore implements model.Store for testing
type MockStore struct {
	Session            *model.SessionRecord
	UpdateSessionCalls int
}

func (m *MockStore) GetSession(ctx context.Context, sessionID string) (*model.SessionRecord, error) {
	if m.Session != nil && m.Session.SessionID == sessionID {
		return m.Session, nil
	}
	return nil, os.ErrNotExist
}

func (m *MockStore) Get(ctx context.Context, sessionID string) (*model.SessionRecord, error) {
	return m.GetSession(ctx, sessionID)
}

func (m *MockStore) List(ctx context.Context) ([]*model.SessionRecord, error) {
	if m.Session != nil {
		return []*model.SessionRecord{m.Session}, nil
	}
	return nil, nil
}

func (m *MockStore) Create(ctx context.Context, rec *model.SessionRecord) error {
	m.Session = rec
	return nil
}

func (m *MockStore) Update(ctx context.Context, rec *model.SessionRecord) error {
	m.Session = rec
	return nil
}

func (m *MockStore) Delete(ctx context.Context, sessionID string) error {
	if m.Session != nil && m.Session.SessionID == sessionID {
		m.Session = nil
	}
	return nil
}

func (m *MockStore) UpdateSession(ctx context.Context, sessionID string, fn func(*model.SessionRecord) error) (*model.SessionRecord, error) {
	if m.Session == nil || m.Session.SessionID != sessionID {
		return nil, os.ErrNotExist
	}
	m.UpdateSessionCalls++
	if err := fn(m.Session); err != nil {
		return nil, err
	}
	return m.Session, nil
}

func TestTouchPlaylistAccessTime_AllowsInitialPlaylistTouchAfterReady(t *testing.T) {
	now := time.Now()
	store := &MockStore{
		Session: &model.SessionRecord{
			SessionID:      "sid-1",
			LastAccessUnix: now.Unix(),
		},
	}
	rec := &model.SessionRecord{
		SessionID:      "sid-1",
		LastAccessUnix: now.Unix(),
	}

	touchPlaylistAccessTime(context.Background(), store, hlsRequest{
		sessionID:  "sid-1",
		filename:   "index.m3u8",
		isPlaylist: true,
	}, rec)

	require.Equal(t, 1, store.UpdateSessionCalls)
	require.False(t, store.Session.LastPlaylistAccessAt.IsZero())
	require.Equal(t, store.Session.LastPlaylistAccessAt.Unix(), store.Session.LastAccessUnix)
}

func TestTouchPlaylistAccessTime_ThrottlesRepeatedPlaylistTouch(t *testing.T) {
	now := time.Now()
	store := &MockStore{
		Session: &model.SessionRecord{
			SessionID:            "sid-2",
			LastAccessUnix:       now.Unix(),
			LastPlaylistAccessAt: now,
		},
	}
	rec := &model.SessionRecord{
		SessionID:            "sid-2",
		LastAccessUnix:       now.Unix(),
		LastPlaylistAccessAt: now,
	}

	touchPlaylistAccessTime(context.Background(), store, hlsRequest{
		sessionID:  "sid-2",
		filename:   "index.m3u8",
		isPlaylist: true,
	}, rec)

	require.Equal(t, 0, store.UpdateSessionCalls)
	require.True(t, store.Session.LastPlaylistAccessAt.Equal(now))
}

func TestTouchPlaylistAccessTime_TracksHLSPlaybackTrace(t *testing.T) {
	now := time.Now()
	store := &MockStore{
		Session: &model.SessionRecord{
			SessionID:            "sid-trace",
			LastAccessUnix:       now.Add(-3 * time.Second).Unix(),
			LastPlaylistAccessAt: now.Add(-3 * time.Second),
			LatestSegmentAt:      now.Add(-2 * time.Second),
		},
	}
	rec := &model.SessionRecord{
		SessionID:            "sid-trace",
		LastAccessUnix:       now.Add(-3 * time.Second).Unix(),
		LastPlaylistAccessAt: now.Add(-3 * time.Second),
		LatestSegmentAt:      now.Add(-2 * time.Second),
	}

	touchPlaylistAccessTime(context.Background(), store, hlsRequest{
		sessionID:  "sid-trace",
		filename:   "index.m3u8",
		isPlaylist: true,
	}, rec)

	require.Equal(t, 1, store.UpdateSessionCalls)
	require.NotNil(t, store.Session.PlaybackTrace)
	require.NotNil(t, store.Session.PlaybackTrace.HLS)
	assert.Equal(t, 1, store.Session.PlaybackTrace.HLS.PlaylistRequestCount)
	assert.NotZero(t, store.Session.PlaybackTrace.HLS.LastPlaylistAtUnix)
	assert.Greater(t, store.Session.PlaybackTrace.HLS.LastPlaylistIntervalMs, 0)
	assert.GreaterOrEqual(t, store.Session.PlaybackTrace.HLS.LatestSegmentLagMs, 0)
	assert.Equal(t, "low", store.Session.PlaybackTrace.HLS.StallRisk)
}

func TestTouchPlaylistAccessTime_DetectsPlaylistOnlyRisk(t *testing.T) {
	now := time.Now()
	store := &MockStore{
		Session: &model.SessionRecord{
			SessionID:            "sid-playlist-only",
			LastAccessUnix:       now.Add(-2 * time.Second).Unix(),
			LastPlaylistAccessAt: now.Add(-2 * time.Second),
			LatestSegmentAt:      now.Add(-1500 * time.Millisecond),
			PlaybackTrace: &model.PlaybackTrace{
				HLS: &model.HLSAccessTrace{
					PlaylistRequestCount: 1,
					LastPlaylistAtUnix:   now.Add(-5 * time.Second).Unix(),
				},
			},
		},
	}
	rec := &model.SessionRecord{
		SessionID:            "sid-playlist-only",
		LastAccessUnix:       now.Add(-2 * time.Second).Unix(),
		LastPlaylistAccessAt: now.Add(-2 * time.Second),
		LatestSegmentAt:      now.Add(-1500 * time.Millisecond),
	}

	touchPlaylistAccessTime(context.Background(), store, hlsRequest{
		sessionID:  "sid-playlist-only",
		filename:   "index.m3u8",
		isPlaylist: true,
	}, rec)

	require.NotNil(t, store.Session.PlaybackTrace)
	require.NotNil(t, store.Session.PlaybackTrace.HLS)
	assert.Equal(t, 2, store.Session.PlaybackTrace.HLS.PlaylistRequestCount)
	assert.Equal(t, "playlist_only", store.Session.PlaybackTrace.HLS.StallRisk)
}

func TestTouchPlaylistAccessTime_DetectsProducerLagRisk(t *testing.T) {
	now := time.Now()
	staleProducerAt := now.Add(-20 * time.Second)
	store := &MockStore{
		Session: &model.SessionRecord{
			SessionID:            "sid-producer-late",
			LastAccessUnix:       now.Add(-2 * time.Second).Unix(),
			LastPlaylistAccessAt: now.Add(-2 * time.Second),
			LatestSegmentAt:      staleProducerAt,
			PlaylistPublishedAt:  staleProducerAt,
		},
	}
	rec := &model.SessionRecord{
		SessionID:            "sid-producer-late",
		LastAccessUnix:       now.Add(-2 * time.Second).Unix(),
		LastPlaylistAccessAt: now.Add(-2 * time.Second),
		LatestSegmentAt:      staleProducerAt,
		PlaylistPublishedAt:  staleProducerAt,
	}

	touchPlaylistAccessTime(context.Background(), store, hlsRequest{
		sessionID:  "sid-producer-late",
		filename:   "index.m3u8",
		isPlaylist: true,
	}, rec)

	require.NotNil(t, store.Session.PlaybackTrace)
	require.NotNil(t, store.Session.PlaybackTrace.HLS)
	assert.Equal(t, "producer_late", store.Session.PlaybackTrace.HLS.StallRisk)
	assert.GreaterOrEqual(t, store.Session.PlaybackTrace.HLS.LatestSegmentLagMs, 15000)
}

func TestTouchSegmentAccessTime_TracksSegmentAccessAndClearsPlaylistOnlyRisk(t *testing.T) {
	now := time.Now()
	store := &MockStore{
		Session: &model.SessionRecord{
			SessionID:            "sid-segment-touch",
			State:                model.SessionReady,
			LastPlaylistAccessAt: now,
			LatestSegmentAt:      now,
			PlaybackTrace: &model.PlaybackTrace{
				HLS: &model.HLSAccessTrace{
					PlaylistRequestCount: 2,
					LastPlaylistAtUnix:   now.Unix(),
					StallRisk:            "playlist_only",
				},
			},
		},
	}

	touchSegmentAccessTime(context.Background(), store, hlsRequest{
		sessionID: "sid-segment-touch",
		filename:  "seg_000123.ts",
		cleanName: "seg_000123.ts",
		isSegment: true,
	}, store.Session)

	require.Equal(t, 1, store.UpdateSessionCalls)
	require.NotNil(t, store.Session.PlaybackTrace)
	require.NotNil(t, store.Session.PlaybackTrace.HLS)
	assert.Equal(t, 1, store.Session.PlaybackTrace.HLS.SegmentRequestCount)
	assert.Equal(t, "seg_000123.ts", store.Session.PlaybackTrace.HLS.LastSegmentName)
	assert.NotZero(t, store.Session.PlaybackTrace.HLS.LastSegmentAtUnix)
	assert.Equal(t, "low", store.Session.PlaybackTrace.HLS.StallRisk)
}

func TestValidateRequest_AllowsOnlyKnownHLSArtifacts(t *testing.T) {
	tests := []struct {
		name      string
		sessionID string
		filename  string
		ok        bool
	}{
		{
			name:      "playlist index",
			sessionID: "safe_session-1",
			filename:  "index.m3u8",
			ok:        true,
		},
		{
			name:      "playlist stream",
			sessionID: "safe_session-1",
			filename:  "stream.m3u8",
			ok:        true,
		},
		{
			name:      "transport stream segment",
			sessionID: "safe_session-1",
			filename:  "seg_000000.ts",
			ok:        true,
		},
		{
			name:      "fmp4 segment",
			sessionID: "safe_session-1",
			filename:  "seg_000001.m4s",
			ok:        true,
		},
		{
			name:      "legacy segment",
			sessionID: "safe_session-1",
			filename:  "stream0.ts",
			ok:        true,
		},
		{
			name:      "init segment",
			sessionID: "safe_session-1",
			filename:  "init.mp4",
			ok:        true,
		},
		{
			name:      "reject invalid session id",
			sessionID: "../unsafe",
			filename:  "index.m3u8",
			ok:        false,
		},
		{
			name:      "reject path traversal filename",
			sessionID: "safe_session-1",
			filename:  "../seg_000000.ts",
			ok:        false,
		},
		{
			name:      "reject unexpected extension",
			sessionID: "safe_session-1",
			filename:  "seg_000000.txt",
			ok:        false,
		},
		{
			name:      "reject legacy wildcard name",
			sessionID: "safe_session-1",
			filename:  "stream../../evil.ts",
			ok:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			_, ok := validateRequest(w, tt.sessionID, tt.filename)
			require.Equal(t, tt.ok, ok)
		})
	}
}

func TestServeHLS_DVRWithStartTag(t *testing.T) {
	// Setup temp directory
	tmpDir := t.TempDir()
	sessionID := "dvr-test-session"
	sessionDir := filepath.Join(tmpDir, "sessions", sessionID)
	require.NoError(t, os.MkdirAll(sessionDir, 0750))

	// Create minimal EVENT playlist WITHOUT EXT-X-START (will be injected)
	rawManifest := `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:2
#EXT-X-MEDIA-SEQUENCE:0
#EXT-X-INDEPENDENT-SEGMENTS
#EXT-X-PROGRAM-DATE-TIME:2026-01-04T16:00:00+0000
#EXTINF:2.000000,
seg_000000.ts
#EXT-X-PROGRAM-DATE-TIME:2026-01-04T16:00:02+0000
#EXTINF:2.000000,
seg_000001.ts
`
	manifestPath := filepath.Join(sessionDir, "index.m3u8")
	require.NoError(t, os.WriteFile(manifestPath, []byte(rawManifest), 0600))

	// Mock store with DVR profile
	store := &MockStore{
		Session: &model.SessionRecord{
			SessionID: sessionID,
			State:     model.SessionReady,
			Profile: model.ProfileSpec{
				Name:           "safari",
				DVRWindowSec:   2700, // 45 minutes
				TranscodeVideo: false,
			},
		},
	}

	// Create HTTP request
	req := httptest.NewRequest("GET", "/index.m3u8", nil)
	w := httptest.NewRecorder()

	// Serve HLS
	ServeHLS(w, req, store, tmpDir, sessionID, "index.m3u8")

	resp := w.Result()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	content := string(body)

	// Assertions
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/vnd.apple.mpegurl", resp.Header.Get("Content-Type"))

	// Black-Box Output Assertions (CTO Gate)
	assert.Contains(t, content, "#EXT-X-PLAYLIST-TYPE:EVENT", "DVR MUST force EVENT type")
	assert.Contains(t, content, "#EXT-X-START:TIME-OFFSET=-8,PRECISE=YES", "DVR MUST inject EXT-X-START with enough live headroom for Safari")
	assert.NotContains(t, content, "#EXT-X-ENDLIST", "DVR (Rolling) MUST NOT contain ENDLIST")
	assert.NotContains(t, content, "#EXT-X-PLAYLIST-TYPE:VOD", "DVR MUST NOT contain VOD tag")

	// Check tag order
	extM3UIdx := strings.Index(content, "#EXTM3U")
	playlistTypeIdx := strings.Index(content, "#EXT-X-PLAYLIST-TYPE")
	startTagIdx := strings.Index(content, "#EXT-X-START")

	assert.True(t, extM3UIdx < playlistTypeIdx && playlistTypeIdx < startTagIdx, "Semantic tags must follow header in order")
}

func TestPreferredLiveStartOffsetSeconds(t *testing.T) {
	t.Run("uses recent segment cadence with conservative headroom", func(t *testing.T) {
		raw := []byte("#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXTINF:6.000000,\nseg_000000.ts\n")
		assert.Equal(t, 12, preferredLiveStartOffsetSeconds(raw))
	})

	t.Run("keeps small targets away from the live edge", func(t *testing.T) {
		raw := []byte("#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXTINF:1.000000,\nseg_000000.ts\n")
		assert.Equal(t, 8, preferredLiveStartOffsetSeconds(raw))
	})

	t.Run("clamps very large targets", func(t *testing.T) {
		raw := []byte("#EXTM3U\n#EXT-X-TARGETDURATION:10\n#EXTINF:10.000000,\nseg_000000.ts\n")
		assert.Equal(t, 12, preferredLiveStartOffsetSeconds(raw))
	})

	t.Run("tracks ORF1 style short segments with extra reserve", func(t *testing.T) {
		raw := []byte("#EXTM3U\n#EXT-X-TARGETDURATION:3\n#EXTINF:2.080000,\nseg_000001.ts\n#EXTINF:1.920000,\nseg_000002.ts\n#EXTINF:2.200000,\nseg_000003.ts\n#EXTINF:1.920000,\nseg_000004.ts\n#EXTINF:2.000000,\nseg_000005.ts\n#EXTINF:2.480000,\nseg_000006.ts\n")
		assert.Equal(t, 9, preferredLiveStartOffsetSeconds(raw))
	})
}

func TestServeHLS_VODNoStartTag(t *testing.T) {
	// Setup temp directory
	tmpDir := t.TempDir()
	sessionID := "vod-test-session"
	sessionDir := filepath.Join(tmpDir, "sessions", sessionID)
	require.NoError(t, os.MkdirAll(sessionDir, 0750))

	// Create VOD playlist
	rawManifest := `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:2
#EXTINF:2.000000,
seg_000000.ts
#EXT-X-ENDLIST
`
	manifestPath := filepath.Join(sessionDir, "index.m3u8")
	require.NoError(t, os.WriteFile(manifestPath, []byte(rawManifest), 0600))

	// Mock store with VOD profile
	store := &MockStore{
		Session: &model.SessionRecord{
			SessionID: sessionID,
			State:     model.SessionReady,
			Profile: model.ProfileSpec{
				Name: "vod",
				VOD:  true,
			},
		},
	}

	req := httptest.NewRequest("GET", "/index.m3u8", nil)
	w := httptest.NewRecorder()

	ServeHLS(w, req, store, tmpDir, sessionID, "index.m3u8")

	resp := w.Result()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	content := string(body)

	// Black-Box Output Assertions (CTO Gate)
	assert.Contains(t, content, "#EXT-X-PLAYLIST-TYPE:VOD", "VOD Profile MUST force VOD tag")
	assert.NotContains(t, content, "#EXT-X-START", "VOD Profile MUST NOT contain START tag (already finite)")
}

func TestServeHLS_LiveNoStartTag(t *testing.T) {
	// Setup temp directory
	tmpDir := t.TempDir()
	sessionID := "live-test-session"
	sessionDir := filepath.Join(tmpDir, "sessions", sessionID)
	require.NoError(t, os.MkdirAll(sessionDir, 0750))

	// Create live playlist
	rawManifest := `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:2
#EXT-X-MEDIA-SEQUENCE:100
#EXTINF:2.000000,
seg_000100.ts
`
	manifestPath := filepath.Join(sessionDir, "index.m3u8")
	require.NoError(t, os.WriteFile(manifestPath, []byte(rawManifest), 0600))

	// Mock store with live profile (DVRWindowSec = 0)
	store := &MockStore{
		Session: &model.SessionRecord{
			SessionID: sessionID,
			State:     model.SessionReady,
			Profile: model.ProfileSpec{
				Name:         "high",
				DVRWindowSec: 0, // Live-only (no DVR)
			},
		},
	}

	req := httptest.NewRequest("GET", "/index.m3u8", nil)
	w := httptest.NewRecorder()

	ServeHLS(w, req, store, tmpDir, sessionID, "index.m3u8")

	resp := w.Result()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	content := string(body)

	// Black-Box Output Assertions (CTO Gate)
	assert.NotContains(t, content, "EXT-X-START", "Live Profile (No DVR) MUST NOT contain START tag")
	assert.NotContains(t, content, "#EXT-X-PLAYLIST-TYPE", "Live Profile MUST NOT force a playlist type (LIVE is default)")
	assert.NotContains(t, content, "#EXT-X-ENDLIST", "Live Profile MUST NOT contain ENDLIST")
}

// TestServeHLS_NegativePreparingJSON ensures that the session endpoint never returns
// the VOD-specific "PREPARING" JSON shape, even during failures/missing files.
// This is a "Hard" Proof of Error Surface Isolation (ADR-ENG-002 Breach Prevention).
func TestServeHLS_NegativePreparingJSON(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "failure-test-session"

	// Mock store with a valid session
	store := &MockStore{
		Session: &model.SessionRecord{
			SessionID: sessionID,
			State:     model.SessionReady,
			Profile: model.ProfileSpec{
				Name: "high",
			},
		},
	}

	// Helper to assert "Hard" isolation
	assertHardIsolation := func(t *testing.T, w *httptest.ResponseRecorder) {
		resp := w.Result()

		// 1. Content-Type Assertion: Must be text/plain for session errors
		contentType := resp.Header.Get("Content-Type")
		assert.Contains(t, contentType, "text/plain", "Session errors MUST be text/plain")
		assert.NotContains(t, contentType, "application/json", "Session errors MUST NOT be JSON")

		// 2. Body Assertion: Must NOT be JSON
		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)

		var js map[string]interface{}
		isJSON := json.Unmarshal(body, &js) == nil
		assert.False(t, isJSON, "Session error body MUST NOT be valid JSON: %s", bodyStr)
		assert.NotContains(t, bodyStr, `{"code":"PREPARING"`, "Session endpoint MUST NOT emit Preparing JSON")
	}

	// Case 1: File Missing (404)
	req := httptest.NewRequest("GET", "/index.m3u8", nil)
	w := httptest.NewRecorder()
	ServeHLS(w, req, store, tmpDir, sessionID, "index.m3u8")
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "playlist_missing", w.Header().Get("X-XG2G-Reason"))
	assertHardIsolation(t, w)

	// Case 2: Session Not Ready (Terminal State - 410 Gone)
	store.Session.State = model.SessionFailed
	w = httptest.NewRecorder()
	ServeHLS(w, req, store, tmpDir, sessionID, "index.m3u8")
	assert.Equal(t, http.StatusGone, w.Code)
	assertHardIsolation(t, w)
}

func TestServeHLS_TerminalTranscodeStalledSetsReasonHeader(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "stall-test-session"

	store := &MockStore{
		Session: &model.SessionRecord{
			SessionID:        sessionID,
			State:            model.SessionFailed,
			Reason:           model.RProcessEnded,
			ReasonDetailCode: model.DTranscodeStalled,
		},
	}

	req := httptest.NewRequest("GET", "/index.m3u8", nil)
	w := httptest.NewRecorder()

	ServeHLS(w, req, store, tmpDir, sessionID, "index.m3u8")

	assert.Equal(t, http.StatusGone, w.Code)
	assert.Equal(t, "transcode_stalled", w.Header().Get("X-XG2G-Reason"))
	assert.Contains(t, w.Body.String(), "stream ended")
}

func TestServeHLS_ActiveMissingSegmentSetsReasonHeader(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "segment-missing-test-session"

	store := &MockStore{
		Session: &model.SessionRecord{
			SessionID: sessionID,
			State:     model.SessionReady,
			Profile: model.ProfileSpec{
				Name: "high",
			},
		},
	}

	req := httptest.NewRequest("GET", "/seg_000000.ts", nil)
	w := httptest.NewRecorder()

	ServeHLS(w, req, store, tmpDir, sessionID, "seg_000000.ts")

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "segment_missing", w.Header().Get("X-XG2G-Reason"))
	assert.Contains(t, w.Body.String(), "file not found")
}

func TestServeHLS_StartingSegmentWaitsForArtifact(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "segment-wait-test-session"
	sessionDir := filepath.Join(tmpDir, "sessions", sessionID)
	require.NoError(t, os.MkdirAll(sessionDir, 0o750))

	store := &MockStore{
		Session: &model.SessionRecord{
			SessionID: sessionID,
			State:     model.SessionStarting,
			Profile: model.ProfileSpec{
				Name: "safari",
			},
		},
	}

	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(sessionDir, "seg_000000.ts"), []byte("segment-data"), 0o600)
	}()

	req := httptest.NewRequest("GET", "/seg_000000.ts", nil)
	w := httptest.NewRecorder()

	ServeHLS(w, req, store, tmpDir, sessionID, "seg_000000.ts")

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "video/mp2t", w.Header().Get("Content-Type"))
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	assert.Equal(t, "identity", w.Header().Get("Content-Encoding"))
	assert.Empty(t, w.Header().Get("X-XG2G-Reason"))
	assert.Equal(t, "segment-data", w.Body.String())
}

func TestServeHLS_SegmentAccessUpdatesPlaybackTrace(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "segment-trace-test-session"
	sessionDir := filepath.Join(tmpDir, "sessions", sessionID)
	require.NoError(t, os.MkdirAll(sessionDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(sessionDir, "seg_000123.ts"), []byte("segment-data"), 0o600))

	now := time.Now()
	store := &MockStore{
		Session: &model.SessionRecord{
			SessionID:            sessionID,
			State:                model.SessionReady,
			LastPlaylistAccessAt: now,
			LatestSegmentAt:      now,
			PlaybackTrace: &model.PlaybackTrace{
				HLS: &model.HLSAccessTrace{
					PlaylistRequestCount: 2,
					LastPlaylistAtUnix:   now.Unix(),
				},
			},
		},
	}

	req := httptest.NewRequest("GET", "/seg_000123.ts", nil)
	w := httptest.NewRecorder()

	ServeHLS(w, req, store, tmpDir, sessionID, "seg_000123.ts")

	assert.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, store.Session.PlaybackTrace)
	require.NotNil(t, store.Session.PlaybackTrace.HLS)
	assert.Equal(t, 1, store.Session.PlaybackTrace.HLS.SegmentRequestCount)
	assert.Equal(t, "seg_000123.ts", store.Session.PlaybackTrace.HLS.LastSegmentName)
	assert.Equal(t, "low", store.Session.PlaybackTrace.HLS.StallRisk)
}

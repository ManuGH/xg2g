package api

import (
	"bytes"
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
	"github.com/rs/zerolog"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/hls/ringbuffer"
	pipelinestore "github.com/ManuGH/xg2g/internal/pipeline/store"
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
#EXTINF:2.000000,
seg_000002.ts
#EXTINF:2.000000,
seg_000003.ts
#EXTINF:2.000000,
seg_000004.ts
#EXTINF:2.000000,
seg_000005.ts
`
	manifestPath := filepath.Join(sessionDir, "index.m3u8")
	require.NoError(t, os.WriteFile(manifestPath, []byte(rawManifest), 0600))
	createMockSafeTSSegment(t, filepath.Join(sessionDir, "seg_000001.ts"))

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
	ServeHLS(w, req, store, nil, tmpDir, sessionID, "index.m3u8")

	resp := w.Result()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	content := string(body)

	// Assertions
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/vnd.apple.mpegurl", resp.Header.Get("Content-Type"))

	// Black-Box Output Assertions (CTO Gate)
	// DVR live MUST stay a standard sliding LIVE playlist: no forced PLAYLIST-TYPE.
	// EXT-X-PLAYLIST-TYPE:EVENT is append-only per spec and breaks the moment
	// delete_segments prunes the window head (~at DVR-window age) -> client hard-stop.
	assert.NotContains(t, content, "#EXT-X-PLAYLIST-TYPE", "DVR live MUST NOT force a playlist type so delete_segments can slide the window without violating an append-only EVENT contract")
	assert.Contains(t, content, "#EXT-X-START:TIME-OFFSET=-8,PRECISE=YES", "DVR MUST inject EXT-X-START with enough live headroom for Safari")
	assert.NotContains(t, content, "#EXT-X-ENDLIST", "DVR (Rolling) MUST NOT contain ENDLIST")

	// Check tag order: the start tag follows the header.
	extM3UIdx := strings.Index(content, "#EXTM3U")
	startTagIdx := strings.Index(content, "#EXT-X-START")

	assert.True(t, extM3UIdx < startTagIdx, "EXT-X-START must follow the #EXTM3U header")
}

// Boundary regression for the 45-min hard stop: once the DVR window fills,
// ffmpeg prunes the playlist head and advances EXT-X-MEDIA-SEQUENCE. The served
// playlist MUST stay a valid sliding LIVE playlist (no append-only EVENT type,
// no ENDLIST) at that point, or clients cut out at exactly the window age.
func TestServeHLS_DVRSlidingPastWindowFill(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "dvr-rolled-session"
	sessionDir := filepath.Join(tmpDir, "sessions", sessionID)
	require.NoError(t, os.MkdirAll(sessionDir, 0750))

	// Window has rolled past fill: high media-sequence, head segments pruned.
	rawManifest := `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:2
#EXT-X-MEDIA-SEQUENCE:1350
#EXT-X-INDEPENDENT-SEGMENTS
#EXT-X-PROGRAM-DATE-TIME:2026-01-04T16:45:00+0000
#EXTINF:2.000000,
seg_001350.ts
#EXT-X-PROGRAM-DATE-TIME:2026-01-04T16:45:02+0000
#EXTINF:2.000000,
seg_001351.ts
`
	manifestPath := filepath.Join(sessionDir, "index.m3u8")
	require.NoError(t, os.WriteFile(manifestPath, []byte(rawManifest), 0600))
	createMockSafeTSSegment(t, filepath.Join(sessionDir, "seg_001350.ts"))

	store := &MockStore{
		Session: &model.SessionRecord{
			SessionID: sessionID,
			State:     model.SessionReady,
			Profile: model.ProfileSpec{
				Name:         "safari",
				DVRWindowSec: 2700,
			},
		},
	}

	req := httptest.NewRequest("GET", "/index.m3u8", nil)
	w := httptest.NewRecorder()
	ServeHLS(w, req, store, nil, tmpDir, sessionID, "index.m3u8")

	resp := w.Result()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	content := string(body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.NotContains(t, content, "#EXT-X-PLAYLIST-TYPE", "rolled DVR window MUST stay a sliding LIVE playlist (no forced type)")
	assert.NotContains(t, content, "#EXT-X-ENDLIST", "rolled DVR window MUST NOT signal end")
	assert.Contains(t, content, "#EXT-X-MEDIA-SEQUENCE:1350", "advanced media-sequence MUST be preserved")
	assert.Contains(t, content, "seg_001350.ts", "retained segments MUST still be served")
	assert.Contains(t, content, "#EXT-X-START:TIME-OFFSET=", "DVR start-headroom tag MUST still be injected after the window rolls")
}

func TestDeriveHLSStartupPolicy(t *testing.T) {
	t.Run("uses recent segment cadence with conservative headroom", func(t *testing.T) {
		raw := []byte("#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXTINF:6.000000,\nseg_000000.ts\n#EXTINF:6.000000,\nseg_000001.ts\n#EXTINF:6.000000,\nseg_000002.ts\n#EXTINF:6.000000,\nseg_000003.ts\n")
		assert.Equal(t, 12, deriveHLSStartupPolicy(nil, raw).StartupHeadroomSec)
	})

	t.Run("keeps small targets away from the live edge", func(t *testing.T) {
		var b strings.Builder
		b.WriteString("#EXTM3U\n#EXT-X-TARGETDURATION:1\n")
		for i := 0; i < 12; i++ {
			b.WriteString("#EXTINF:1.000000,\nseg_00000" + string(rune('0'+i%10)) + ".ts\n")
		}
		assert.Equal(t, 8, deriveHLSStartupPolicy(nil, []byte(b.String())).StartupHeadroomSec)
	})

	t.Run("clamps very large targets", func(t *testing.T) {
		raw := []byte("#EXTM3U\n#EXT-X-TARGETDURATION:10\n#EXTINF:10.000000,\nseg_000000.ts\n#EXTINF:10.000000,\nseg_000001.ts\n#EXTINF:10.000000,\nseg_000002.ts\n")
		assert.Equal(t, 12, deriveHLSStartupPolicy(nil, raw).StartupHeadroomSec)
	})

	t.Run("clamps headroom to available media", func(t *testing.T) {
		// Two 2s segments at startup: only 2s of offsetable media exist, so a
		// double-digit offset would pin native players to the (worst) very
		// first segment.
		raw := []byte("#EXTM3U\n#EXT-X-TARGETDURATION:2\n#EXTINF:2.000000,\nseg_000000.m4s\n#EXTINF:2.000000,\nseg_000001.m4s\n")
		policy := deriveHLSStartupPolicy(nil, raw)
		assert.Equal(t, 2, policy.StartupHeadroomSec)
		assert.Contains(t, policy.Reasons, "available_media_clamp")
	})

	t.Run("suppresses offset when no media is offsetable", func(t *testing.T) {
		raw := []byte("#EXTM3U\n#EXT-X-TARGETDURATION:2\n#EXTINF:2.000000,\nseg_000000.m4s\n")
		assert.Equal(t, 0, deriveHLSStartupPolicy(nil, raw).StartupHeadroomSec)
	})

	t.Run("tracks ORF1 style short segments with extra reserve", func(t *testing.T) {
		raw := []byte("#EXTM3U\n#EXT-X-TARGETDURATION:3\n#EXTINF:2.080000,\nseg_000001.ts\n#EXTINF:1.920000,\nseg_000002.ts\n#EXTINF:2.200000,\nseg_000003.ts\n#EXTINF:1.920000,\nseg_000004.ts\n#EXTINF:2.000000,\nseg_000005.ts\n#EXTINF:2.480000,\nseg_000006.ts\n")
		assert.Equal(t, 9, deriveHLSStartupPolicy(nil, raw).StartupHeadroomSec)
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

	ServeHLS(w, req, store, nil, tmpDir, sessionID, "index.m3u8")

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

	ServeHLS(w, req, store, nil, tmpDir, sessionID, "index.m3u8")

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
	ServeHLS(w, req, store, nil, tmpDir, sessionID, "index.m3u8")
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "playlist_missing", w.Header().Get("X-XG2G-Reason"))
	assertHardIsolation(t, w)

	// Case 2: Session Not Ready (Terminal State - 410 Gone)
	store.Session.State = model.SessionFailed
	w = httptest.NewRecorder()
	ServeHLS(w, req, store, nil, tmpDir, sessionID, "index.m3u8")
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

	ServeHLS(w, req, store, nil, tmpDir, sessionID, "index.m3u8")

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

	ServeHLS(w, req, store, nil, tmpDir, sessionID, "seg_000000.ts")

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

	ServeHLS(w, req, store, nil, tmpDir, sessionID, "seg_000000.ts")

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

	ServeHLS(w, req, store, nil, tmpDir, sessionID, "seg_000123.ts")

	assert.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, store.Session.PlaybackTrace)
	require.NotNil(t, store.Session.PlaybackTrace.HLS)
	assert.Equal(t, 1, store.Session.PlaybackTrace.HLS.SegmentRequestCount)
	assert.Equal(t, "seg_000123.ts", store.Session.PlaybackTrace.HLS.LastSegmentName)
	assert.Equal(t, "low", store.Session.PlaybackTrace.HLS.StallRisk)
}

func TestServeHLS_InMemory_PlaylistAndSegment(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "inmemory-serve-test-session"
	defer ringbuffer.DefaultRegistry.Delete(sessionID)

	buf := ringbuffer.DefaultRegistry.GetOrCreate(sessionID, nil)
	playlistContent := "#EXTM3U\n#EXTINF:2.000000,\nseg_000000.ts\n"
	buf.Put("index.m3u8", []byte(playlistContent))
	buf.Put("seg_000000.ts", []byte("in-memory-ts-data"))

	store := &MockStore{
		Session: &model.SessionRecord{
			SessionID: sessionID,
			State:     model.SessionReady,
		},
	}

	// Request Playlist
	req := httptest.NewRequest("GET", "/index.m3u8", nil)
	w := httptest.NewRecorder()
	ServeHLS(w, req, store, nil, tmpDir, sessionID, "index.m3u8")

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/vnd.apple.mpegurl", w.Header().Get("Content-Type"))
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	assert.Equal(t, playlistContent, w.Body.String())

	// Request Segment
	reqSeg := httptest.NewRequest("GET", "/seg_000000.ts", nil)
	wSeg := httptest.NewRecorder()
	ServeHLS(wSeg, reqSeg, store, nil, tmpDir, sessionID, "seg_000000.ts")

	assert.Equal(t, http.StatusOK, wSeg.Code)
	assert.Equal(t, "video/mp2t", wSeg.Header().Get("Content-Type"))
	assert.Equal(t, "no-store", wSeg.Header().Get("Cache-Control"))
	assert.Equal(t, "identity", wSeg.Header().Get("Content-Encoding"))
	assert.Equal(t, "in-memory-ts-data", wSeg.Body.String())
}

func TestServeHLS_InMemory_Polling(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "inmemory-polling-test-session"
	defer ringbuffer.DefaultRegistry.Delete(sessionID)

	buf := ringbuffer.DefaultRegistry.GetOrCreate(sessionID, nil)

	store := &MockStore{
		Session: &model.SessionRecord{
			SessionID: sessionID,
			State:     model.SessionStarting,
		},
	}

	go func() {
		time.Sleep(150 * time.Millisecond)
		buf.Put("seg_000000.m4s", []byte("in-memory-fmp4-data"))
	}()

	req := httptest.NewRequest("GET", "/seg_000000.m4s", nil)
	w := httptest.NewRecorder()
	ServeHLS(w, req, store, nil, tmpDir, sessionID, "seg_000000.m4s")

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "video/mp4", w.Header().Get("Content-Type"))
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	assert.Equal(t, "in-memory-fmp4-data", w.Body.String())
}

func TestServeHLS_StoreRegistry_RAMDelivery(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "registry-ram-test-session"
	sessionDir := filepath.Join(tmpDir, "sessions", sessionID)
	require.NoError(t, os.MkdirAll(sessionDir, 0o750))

	playlistContent := "#EXTM3U\n#EXTINF:2.000000,\nseg_000001.m4s\n"
	require.NoError(t, os.WriteFile(filepath.Join(sessionDir, "index.m3u8"), []byte(playlistContent), 0o600))

	mockStore := &MockStore{
		Session: &model.SessionRecord{
			SessionID: sessionID,
			State:     model.SessionReady,
		},
	}

	registry := pipelinestore.NewMemoryStoreRegistry()
	ramStore, err := pipelinestore.NewRAMShadowStore(1024*1024, 10)
	require.NoError(t, err)
	err = ramStore.Publish(context.Background(), pipelinestore.StreamID(sessionID), pipelinestore.Object{
		Name: "seg_000001.m4s",
		Data: []byte("registry-ram-segment-data"),
	})
	require.NoError(t, err)
	require.NoError(t, registry.Register(sessionID, ramStore))

	// Request Segment from RAM
	reqSeg := httptest.NewRequest("GET", "/seg_000001.m4s", nil)
	wSeg := httptest.NewRecorder()
	ServeHLS(wSeg, reqSeg, mockStore, registry, tmpDir, sessionID, "seg_000001.m4s")

	assert.Equal(t, http.StatusOK, wSeg.Code)
	assert.Equal(t, "ram", wSeg.Header().Get("X-XG2G-Source"))
	assert.Equal(t, "video/mp4", wSeg.Header().Get("Content-Type"))
	assert.Equal(t, "registry-ram-segment-data", wSeg.Body.String())

	// Request Playlist from Disk
	reqPlay := httptest.NewRequest("GET", "/index.m3u8", nil)
	wPlay := httptest.NewRecorder()
	ServeHLS(wPlay, reqPlay, mockStore, registry, tmpDir, sessionID, "index.m3u8")

	assert.Equal(t, http.StatusOK, wPlay.Code)
	assert.Equal(t, "disk", wPlay.Header().Get("X-XG2G-Source"))
	assert.Equal(t, playlistContent, wPlay.Body.String())
}

func TestRewritePlaylist_MasterPlaylistFMP4(t *testing.T) {
	masterContent := `#EXTM3U
#EXT-X-VERSION:7
#EXT-X-STREAM-INF:BANDWIDTH=5000000,CODECS="av01.0.08M.08"
stream_0.m3u8
`
	rec := &model.SessionRecord{}
	rec.Profile.Container = "fmp4"
	rdr, _, valid, err := rewritePlaylist(strings.NewReader(masterContent), rec, "", zerolog.Logger{})
	assert.NoError(t, err)
	assert.True(t, valid, "master playlist should be valid even without EXT-X-MAP")
	out, _ := io.ReadAll(rdr)
	assert.Contains(t, string(out), "#EXT-X-STREAM-INF")
}

func buildTestTSPacket(pid int, pusi bool, payload []byte) []byte {
	pkt := make([]byte, 188)
	pkt[0] = 0x47
	pkt[1] = byte(pid >> 8)
	if pusi {
		pkt[1] |= 0x40
	}
	pkt[2] = byte(pid & 0xFF)
	pkt[3] = 0x10

	if len(payload) < 184 {
		pkt[3] = 0x30
		padLen := 184 - len(payload) - 1
		pkt[4] = byte(padLen)
		if padLen > 0 {
			pkt[5] = 0x00
			for j := 1; j < padLen; j++ {
				pkt[5+j] = 0xFF
			}
		}
		copy(pkt[5+padLen:], payload)
	} else {
		copy(pkt[4:], payload[:184])
	}
	return pkt
}

func createMockSafeTSSegment(t *testing.T, path string) {
	annexB := []byte{
		0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0xC0, 0x1E,
		0x00, 0x00, 0x00, 0x01, 0x68, 0xCE, 0x3C, 0x80,
		0x00, 0x00, 0x00, 0x01, 0x65, 0x88, 0x84, 0x00,
	}
	videoPID := 0x0100
	var tsData bytes.Buffer
	tsData.Write(buildTestPATPMT(videoPID))
	tsData.Write(buildTestTSPacket(videoPID, true, buildTestPESPacket(annexB)))
	require.NoError(t, os.WriteFile(path, tsData.Bytes(), 0644))
}

func buildTestPATPMT(videoPID int) []byte {
	var buf bytes.Buffer
	patPayload := []byte{
		0x00,
		0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00,
		0x00, 0x01, 0xE0, 0x20,
		0x2C, 0x80, 0xB8, 0x3A,
	}
	buf.Write(buildTestTSPacket(0x0000, true, patPayload))

	pmtPayload := []byte{
		0x00,
		0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00,
		0xE1, 0x00,
		0xF0, 0x00,
		0x1B, byte(videoPID >> 8), byte(videoPID & 0xFF), 0xF0, 0x00,
		0x4E, 0x59, 0x3D, 0x1E,
	}
	buf.Write(buildTestTSPacket(0x0020, true, pmtPayload))
	return buf.Bytes()
}

func buildTestPESPacket(annexBPayload []byte) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00})
	buf.Write([]byte{0x80, 0x80, 0x05})
	buf.Write([]byte{0x21, 0x00, 0x01, 0x00, 0x01})
	buf.Write(annexBPayload)
	return buf.Bytes()
}

func TestRewritePlaylist_FilterRAP(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "session_rap_filter"
	sessionDir := filepath.Join(tmpDir, sessionID)
	require.NoError(t, os.MkdirAll(sessionDir, 0755))

	// Create seg_000000.ts (dummy file, non-IDR/unsafe)
	require.NoError(t, os.WriteFile(filepath.Join(sessionDir, "seg_000000.ts"), []byte{0x47, 0x00, 0x00, 0x10}, 0644))

	// Create seg_000001.ts with valid PAT/PMT + SPS(7), PPS(8), IDR(5) -> safe RAP
	annexB := []byte{
		0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0xC0, 0x1E,
		0x00, 0x00, 0x00, 0x01, 0x68, 0xCE, 0x3C, 0x80,
		0x00, 0x00, 0x00, 0x01, 0x65, 0x88, 0x84, 0x00,
	}
	videoPID := 0x0100
	var tsData bytes.Buffer
	tsData.Write(buildTestPATPMT(videoPID))
	tsData.Write(buildTestTSPacket(videoPID, true, buildTestPESPacket(annexB)))
	require.NoError(t, os.WriteFile(filepath.Join(sessionDir, "seg_000001.ts"), tsData.Bytes(), 0644))

	playlistContent := `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:2
#EXT-X-MEDIA-SEQUENCE:0
#EXTINF:2.000,
seg_000000.ts
#EXTINF:2.000,
seg_000001.ts
`
	rec := &model.SessionRecord{
		State: model.SessionReady,
		Profile: model.ProfileSpec{
			DVRWindowSec: 60,
			Container:    "ts",
		},
	}

	rdr, _, valid, err := rewritePlaylist(strings.NewReader(playlistContent), rec, sessionDir, zerolog.Logger{})
	require.NoError(t, err)
	require.True(t, valid)

	out, _ := io.ReadAll(rdr)
	outStr := string(out)
	assert.Contains(t, outStr, "#EXT-X-MEDIA-SEQUENCE:1")
	assert.NotContains(t, outStr, "seg_000000.ts")
	assert.Contains(t, outStr, "seg_000001.ts")
}

func TestValidateRequest_MasterPlaylistVariants(t *testing.T) {
	w := httptest.NewRecorder()
	req, ok := validateRequest(w, "session123", "stream_0.m3u8")
	assert.True(t, ok)
	assert.True(t, req.isPlaylist)

	req2, ok2 := validateRequest(w, "session123", "init_0.mp4")
	assert.True(t, ok2)
	assert.True(t, req2.isInit)
}

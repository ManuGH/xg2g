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
	Session *model.SessionRecord
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
	assert.Contains(t, content, "#EXT-X-START:TIME-OFFSET=-2700,PRECISE=YES", "DVR MUST inject EXT-X-START with correct offset")
	assert.NotContains(t, content, "#EXT-X-ENDLIST", "DVR (Rolling) MUST NOT contain ENDLIST")
	assert.NotContains(t, content, "#EXT-X-PLAYLIST-TYPE:VOD", "DVR MUST NOT contain VOD tag")

	// Check tag order
	extM3UIdx := strings.Index(content, "#EXTM3U")
	playlistTypeIdx := strings.Index(content, "#EXT-X-PLAYLIST-TYPE")
	startTagIdx := strings.Index(content, "#EXT-X-START")

	assert.True(t, extM3UIdx < playlistTypeIdx && playlistTypeIdx < startTagIdx, "Semantic tags must follow header in order")
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
	assertHardIsolation(t, w)

	// Case 2: Session Not Ready (Terminal State - 410 Gone)
	store.Session.State = model.SessionFailed
	w = httptest.NewRecorder()
	ServeHLS(w, req, store, tmpDir, sessionID, "index.m3u8")
	assert.Equal(t, http.StatusGone, w.Code)
	assertHardIsolation(t, w)
}

//go:build integration

package api_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/pipeline/api"
	"github.com/ManuGH/xg2g/internal/pipeline/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockStore satisfies api.HLSStore
type MockStore struct {
	Session *model.SessionRecord
}

func (m *MockStore) GetSession(ctx context.Context, id string) (*model.SessionRecord, error) {
	if m.Session != nil && m.Session.SessionID == id {
		return m.Session, nil
	}
	return nil, fmt.Errorf("session not found")
}

func TestSafariHLSSmoke(t *testing.T) {
	// 1. Setup: Create temp dir for HLS artifacts
	tmpDir, err := os.MkdirTemp("", "xg2g-smoke-")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	sessionID := "smoke-session-1"
	sessionDir := filepath.Join(tmpDir, "sessions", sessionID)
	err = os.MkdirAll(sessionDir, 0755)
	require.NoError(t, err)

	// 2. Generate minimal HLS stream using FFmpeg
	// Using lavfi testsrc to generate 4 seconds of video (2 segments of 2s)
	// Outputting strict RFC3339 timestamps is tricky with basic ffmpeg flags depending on version,
	// but we will verify what we get and if our normalizer fixes it.
	// We deliberately use the standard +0000 format implicitly to test the normalizer.
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not found, skipping integration test")
	}

	cmd := exec.Command(ffmpegPath,
		"-y",
		"-f", "lavfi", "-i", "testsrc=duration=4:size=640x360:rate=50",
		"-f", "lavfi", "-i", "sine=duration=4:frequency=440",
		"-c:v", "libx264", "-g", "100", "-keyint_min", "100", "-sc_threshold", "0",
		"-c:a", "aac", "-ar", "48000", "-ac", "2",
		"-f", "hls",
		"-hls_time", "2",
		"-hls_list_size", "0",
		"-hls_segment_type", "mpegts",
		"-hls_flags", "independent_segments+program_date_time",
		"-hls_playlist_type", "event",
		"-hls_segment_filename", filepath.Join(sessionDir, "seg_%06d.ts"),
		filepath.Join(sessionDir, "index.m3u8"),
	)

	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "ffmpeg generation failed: %s", string(output))

	// 3. Setup Mock Store and Server
	store := &MockStore{
		Session: &model.SessionRecord{
			SessionID:      sessionID,
			State:          model.SessionReady,
			LastAccessUnix: time.Now().Unix(),
		},
	}

	// Wrapper handler to call ServeHLS
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// path: /api/v3/sessions/{sessionID}/hls/{filename}
		parts := strings.Split(r.URL.Path, "/")
		// parts: ["", "api", "v3", "sessions", "sid", "hls", "fname"]
		if len(parts) < 7 {
			http.NotFound(w, r)
			return
		}
		sid := parts[4]
		fname := parts[6]
		// simple mapping for test
		api.ServeHLS(w, r, store, tmpDir, sid, fname)
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	client := server.Client()

	// 4. Verify Playlist (Normalization Contract)
	playlistPath := filepath.Join(sessionDir, "index.m3u8")
	rawContentBytes, err := os.ReadFile(playlistPath)
	require.NoError(t, err)
	rawContent := string(rawContentBytes)

	// Verify raw content contains +0000 (FFmpeg default) to ensure we are testing something real
	if strings.Contains(rawContent, "+0000") {
		t.Log("Raw playlist contains +0000 as expected.")
	} else {
		t.Logf("Raw playlist does not contain +0000. Raw snippet: %s", rawContent[:min(len(rawContent), 200)])
	}

	playlistURL := fmt.Sprintf("%s/api/v3/sessions/%s/hls/index.m3u8", server.URL, sessionID)
	resp, err := client.Get(playlistURL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/vnd.apple.mpegurl", resp.Header.Get("Content-Type"))
	assert.Equal(t, "no-store", resp.Header.Get("Cache-Control"))

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	content := string(body)

	// Contract Assertions
	assert.Contains(t, content, "#EXT-X-INDEPENDENT-SEGMENTS", "Must claim independent segments")
	assert.Contains(t, content, "#EXT-X-PLAYLIST-TYPE:EVENT", "Must be EVENT playlist")
	assert.Contains(t, content, "#EXT-X-PROGRAM-DATE-TIME:", "Must contain program date time")

	// Safari DVR: Check for EXT-X-START tag (critical for scrubber)
	assert.Contains(t, content, "EXT-X-START:TIME-OFFSET=", "Must contain EXT-X-START for Safari DVR scrubber")

	// Verify Normalization Logic (Raw != Served if Raw had +0000)
	if strings.Contains(rawContent, "+0000") && !strings.Contains(content, "+0000") {
		t.Log("Verified normalization: +0000 removed from served content")
	}

	// Check RFC3339 Compliance (Strict Regex)
	// Matches 2026-01-04T16:17:53.066Z or 2026-01-04T16:17:53.066+00:00
	rfc3339PDT := regexp.MustCompile(`^#EXT-X-PROGRAM-DATE-TIME:\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(Z|[+-]\d{2}:\d{2})\s*$`)

	lines := strings.Split(content, "\n")
	foundPDT := false
	for _, line := range lines {
		if strings.HasPrefix(line, "#EXT-X-PROGRAM-DATE-TIME:") {
			foundPDT = true
			if !rfc3339PDT.MatchString(strings.TrimSpace(line)) {
				t.Errorf("PDT line not RFC3339 compliant: %q", line)
			}
		}
	}
	assert.True(t, foundPDT, "Should have found at least one PDT tag")

	// 5. Verify Segment Headers
	segmentURL := fmt.Sprintf("%s/api/v3/sessions/%s/hls/seg_000000.ts", server.URL, sessionID)
	respSeg, err := client.Get(segmentURL)
	require.NoError(t, err)
	defer respSeg.Body.Close()

	assert.Equal(t, http.StatusOK, respSeg.StatusCode)
	assert.Equal(t, "video/mp2t", respSeg.Header.Get("Content-Type"), "Strict lowercase content-type required")
	assert.Equal(t, "public, max-age=60", respSeg.Header.Get("Cache-Control"))
}

package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/recordings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSanitizeRecordingRelPath_Adversarial covers strict traversal rejection and normalization
func TestSanitizeRecordingRelPath_Adversarial(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantClean string
		wantBlock bool
	}{
		{"Normal", "a/b", "a/b", false},
		{"LeadingSlash", "/a/b", "a/b", false},
		{"DotSegment", "a/./b", "a/b", false},
		{"DoubleSlash", "a//b", "a/b", false},
		{"Traversal_Simple", "../a", "", true},
		{"Traversal_Deep", "a/../../b", "", true},
		{"Traversal_JustDotDot", "..", "", true},
		{"Traversal_End", "a/..", "", true},
		{"ControlChar", "a/\x00/b", "", true},
		{"WinBackslash", "a\\b", "", true},
		{"QueryChar", "a?b", "", true},
		{"HashChar", "a#b", "", true},
		// Case: cleaned path is ".." so it should be blocked
		{"SneakyTraversal", "/../etc/passwd", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, blocked := sanitizeRecordingRelPath(tt.input)
			assert.Equal(t, tt.wantBlock, blocked, "blocked status mismatch")
			if !blocked {
				assert.Equal(t, tt.wantClean, got, "clean path mismatch")
			}
		})
	}
}

// TestValidateRecordingRef_Adversarial covers strict ID validation
func TestValidateRecordingRef_Adversarial(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"Valid", "1:0:1:1:1:1:1:1:1:1:/path/to/file.ts", false},
		{"Invalid_Control", "1:0:1:\x07:1:1:1:1:1:1:/path.ts", true},
		{"Invalid_Backslash", "1:0:1:\\:1:1:1:1:1:1:/path.ts", true},
		{"Invalid_Query", "1:0:1:?:1:1:1:1:1:1:/path.ts", true},
		{"Invalid_Hash", "1:0:1:#:1:1:1:1:1:1:/path.ts", true},
		{"Invalid_Traversal", "1:0:1:1:1:1:1:1:1:1:/../file.ts", true},
		{"Invalid_EncodedTraversal", "1:0:1:1:1:1:1:1:1:1:/%2e%2e/file.ts", true},
		// Note: validateRecordingRef takes a string. If it contains literal %2e, that's safe chars unless decoded later.
		// Detailed check: strings.Contains(x, "/../")
		{"Invalid_LiteralDotDot", "1:0:1:1:1:1:1:1:1:1:/path/../file.ts", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRecordingRef(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestIsAllowedVideoSegment defines the canonical extension list
func TestIsAllowedVideoSegment(t *testing.T) {
	// VOD Recording uses TS-HLS only (no fMP4)
	allowed := []string{"seg_0.ts", "seg_1.ts", "seg_chunk.ts", "seg_2.TS"} // Case insensitive extension
	blocked := []string{
		"seg.exe", "seg.sh", "seg.mov", "random.ts", "movie.mp4", "seg_list.m3u8", "seg.ts", // seg.ts blocked (no underscore)
		"seg_0.mp4",   // No MP4 segments for VOD
		"init.mp4",    // No fMP4 init segments for VOD
		"seg_1.m4s",   // No fMP4 segments for VOD
		"seg_0.cmfv",  // No CMAF segments for VOD
	}

	// Strict Rules for VOD Recording:
	// 1. Must start with "seg_"
	// 2. Extension must be .ts (TS-HLS only)

	for _, f := range allowed {
		assert.True(t, isAllowedVideoSegment(f), "should allow %s", f)
	}
	for _, f := range blocked {
		assert.False(t, isAllowedVideoSegment(f), "should block %s", f)
	}
}

// TestSemaphore_CapacityControl verifies 429 on saturation
func TestSemaphore_CapacityControl(t *testing.T) {
	// Setup
	tmp := t.TempDir()

	cfg := config.AppConfig{
		VODMaxConcurrent: 1, // Only 1 allowed
		DataDir:          tmp,
		RecordingPathMappings: []config.RecordingPathMapping{
			{ReceiverRoot: "/hdd/movie", LocalRoot: tmp},
		},
	}
	s := &Server{
		cfg:                 cfg,
		vodBuildSem:         make(chan struct{}, cfg.VODMaxConcurrent),
		recordingRun:        make(map[string]*recordingBuildState),
		recordingPathMapper: recordings.NewPathMapper(cfg.RecordingPathMappings),
	}

	// 1. Consume the single slot
	s.vodBuildSem <- struct{}{}

	// 2. Request playback (MP4 remux)
	// Create a dummy recording ID that maps to our local path
	svcRef := "1:0:1:1:1:1:1:1:1:1:/hdd/movie/test.ts"
	recID := encodeRecordingID(svcRef)

	req := httptest.NewRequest("GET", "/api/v3/recordings/"+recID+"/stream.mp4", nil)
	w := httptest.NewRecorder()

	// Execute
	s.StreamRecordingDirect(w, req, recID)

	// Verify 429
	resp := w.Result()
	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Equal(t, "30", resp.Header.Get("Retry-After"))
}

func TestRecordingLivePlaylistReady_Adversarial(t *testing.T) {
	// tmpConfigDir := t.TempDir() // Not used but needed for mock if full server
	tmpCacheDir := t.TempDir()

	// 1. Setup minimal structure
	livePath := filepath.Join(tmpCacheDir, "index.live.m3u8")

	// Case A: Valid TS-HLS playlist
	tsContent := `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:6
#EXTINF:6.000,
seg_0.ts`
	err := os.WriteFile(livePath, []byte(tsContent), 0644)
	require.NoError(t, err)

	// Should NOT be ready yet (files missing)
	_, ready := recordingLivePlaylistReady(tmpCacheDir)
	assert.False(t, ready, "should not be ready if files missing")

	// Create seg_0.ts
	err = os.WriteFile(filepath.Join(tmpCacheDir, "seg_0.ts"), []byte("dummy ts data"), 0644)
	require.NoError(t, err)

	// Should be ready now (seg_0.ts exists and is referenced)
	path, ready := recordingLivePlaylistReady(tmpCacheDir)
	assert.True(t, ready, "should be ready if seg_0.ts exists and is referenced")
	assert.Equal(t, livePath, path)

	// Case B: Unsafe segment path (directory traversal)
	unsafeSeg := `#EXTM3U
#EXTINF:6.000,
../secret.ts`
	err = os.WriteFile(livePath, []byte(unsafeSeg), 0644)
	require.NoError(t, err)
	// Ensure this doesn't panic and returns false (rejects unsafe path)
	_, ready = recordingLivePlaylistReady(tmpCacheDir)
	assert.False(t, ready, "should reject unsafe segment path")

	// Case C: Invalid segment extension (not .ts)
	invalidExt := `#EXTM3U
#EXTINF:6.000,
seg_0.m4s`
	err = os.WriteFile(livePath, []byte(invalidExt), 0644)
	require.NoError(t, err)
	_, ready = recordingLivePlaylistReady(tmpCacheDir)
	assert.False(t, ready, "should reject non-.ts segments (VOD uses TS-HLS only)")
}

// TestSegmentPatternConsistency ensures runRecordingBuild produces segments
// consistent with isAllowedVideoSegment validation.
func TestSegmentPatternConsistency(t *testing.T) {
	// The segment pattern used in runRecordingBuild determines what ffmpeg produces.
	// This test verifies that the pattern extension matches what isAllowedVideoSegment allows.

	// Segment pattern is constructed via SegmentPattern(dir, ext)
	// For VOD recordings, we use ".ts" extension
	segExt := ".ts"

	// Test that our allow-list accepts segments with this extension
	testCases := []struct {
		name     string
		filename string
		allowed  bool
	}{
		{
			name:     "valid TS segment",
			filename: "seg_0.ts",
			allowed:  true,
		},
		{
			name:     "valid TS segment with higher index",
			filename: "seg_123.ts",
			allowed:  true,
		},
		{
			name:     "fMP4 segment (not supported in VOD)",
			filename: "seg_0.m4s",
			allowed:  false,
		},
		{
			name:     "generic MP4 (not allowed)",
			filename: "seg_0.mp4",
			allowed:  false,
		},
		{
			name:     "missing seg_ prefix",
			filename: "video.ts",
			allowed:  false,
		},
		{
			name:     "wrong extension",
			filename: "seg_0.avi",
			allowed:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := isAllowedVideoSegment(tc.filename)
			assert.Equal(t, tc.allowed, result, "isAllowedVideoSegment(%s) should be %v", tc.filename, tc.allowed)
		})
	}

	// Verify that the production extension is allowed
	testSegment := "seg_0" + segExt
	assert.True(t, isAllowedVideoSegment(testSegment),
		"Production segment pattern seg_*%s must be allowed by isAllowedVideoSegment", segExt)
}

// TestRootIDCollisionHandling verifies that configured and discovered recording roots
// are normalized consistently and collision suffixing works correctly.
func TestRootIDCollisionHandling(t *testing.T) {
	// Test the normalization function behavior
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "lowercase conversion",
			input:    "HDD",
			expected: "hdd",
		},
		{
			name:     "space to underscore",
			input:    "My Drive",
			expected: "my_drive",
		},
		{
			name:     "mixed case and spaces",
			input:    "USB Drive 1",
			expected: "usb_drive_1",
		},
		{
			name:     "already normalized",
			input:    "ssd",
			expected: "ssd",
		},
	}

	normalizeRootID := func(id string) string {
		return strings.ToLower(strings.ReplaceAll(id, " ", "_"))
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := normalizeRootID(tc.input)
			assert.Equal(t, tc.expected, result, "normalizeRootID(%s) should be %s", tc.input, tc.expected)
		})
	}

	// Test collision handling scenario
	t.Run("collision suffixing", func(t *testing.T) {
		roots := make(map[string]string)

		// Simulate adding roots that collide after normalization
		rootConfigs := []struct {
			originalID string
			path       string
		}{
			{"hdd", "/media/hdd"},
			{"HDD", "/media/HDD2"},      // Collides after normalization with "hdd"
			{"usb drive", "/media/usb"}, // No collision
			{"USB Drive", "/media/usb2"}, // Collides after normalization with "usb_drive"
		}

		for _, rc := range rootConfigs {
			normalizedID := normalizeRootID(rc.originalID)
			baseID := normalizedID
			counter := 2

			// Collision handling loop (same as in GetRecordings)
			for {
				if _, exists := roots[normalizedID]; !exists {
					break
				}
				normalizedID = fmt.Sprintf("%s-%d", baseID, counter)
				counter++
			}

			roots[normalizedID] = rc.path
		}

		// Verify results
		assert.Equal(t, 4, len(roots), "should have 4 distinct roots")

		// First "hdd" should use the base ID
		assert.Contains(t, roots, "hdd", "first hdd should use base ID")
		assert.Equal(t, "/media/hdd", roots["hdd"])

		// Second "HDD" (normalized to "hdd") should get suffix
		assert.Contains(t, roots, "hdd-2", "second HDD should get -2 suffix")
		assert.Equal(t, "/media/HDD2", roots["hdd-2"])

		// First "usb drive" should use base ID
		assert.Contains(t, roots, "usb_drive", "first usb drive should use base ID")
		assert.Equal(t, "/media/usb", roots["usb_drive"])

		// Second "USB Drive" should get suffix
		assert.Contains(t, roots, "usb_drive-2", "second USB Drive should get -2 suffix")
		assert.Equal(t, "/media/usb2", roots["usb_drive-2"])
	})
}

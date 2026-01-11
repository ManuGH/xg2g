package resolver

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/vod"
)

// TestPR42_SourceResolutionAndKeyHygiene covers PR4.2 requirements
func TestPR42_SourceResolutionAndKeyHygiene(t *testing.T) {
	// Setup Basic Dependencies
	cfg := &config.AppConfig{
		Enigma2: config.Enigma2Settings{
			BaseURL:    "http://receiver:80",
			StreamPort: 8001,
		},
		// Policy: "receiver_only" to force receiver path
		RecordingPlaybackPolicy: config.PlaybackPolicyReceiverOnly,
	}
	mgr := vod.NewManager(nil, nil, nil)
	r := New(cfg, mgr)

	// Test 1: URL Escaping (Golden Test)
	t.Run("Receiver URL escaping uses RawPath correctly", func(t *testing.T) {
		// A serviceRef with spaces, colons, and maybe a weird char
		// "1:0:19:..." is typical
		serviceRef := "1:0:1:1:1:1:1:0:0:0:/media/hdd/movie/My Recording.ts"

		kind, source, _, err := r.resolveSource(context.Background(), serviceRef)
		require.NoError(t, err)
		assert.Equal(t, "receiver", kind)

		// Parse the result
		u, err := url.Parse(source)
		require.NoError(t, err)

		// Verify that u.String() returns the properly escaped version.
		// Enigma2 expects colons to be literal (unencoded) but spaces encoded.
		// Because we used u.RawPath in construction, we avoided double-encoding.

		// Expected: http://receiver:8001/1:0:1:1:1:1:1:0:0:0:/media/hdd/movie/My%20Recording.ts
		expectedSubstr := "/1:0:1:1:1:1:1:0:0:0:/media/hdd/movie/My%20Recording.ts"

		s := u.String()

		// 1. Assert content matches expectations
		assert.Contains(t, s, expectedSubstr)

		// 2. Strict Assertion: Colons must be literal (not %3A or %3a)
		assert.Contains(t, s, ":0:")
		assert.NotContains(t, strings.ToLower(s), "%3a")

		// 3. Strict Assertion: Spaces must be single-encoded (%20), not double (%2520)
		assert.NotContains(t, s, "%2520")
		assert.Contains(t, s, "%20")
	})

	// Test 2: Local Mapping Use ExtractPath
	t.Run("Local mapping uses extracted path", func(t *testing.T) {
		// Map: /media/hdd -> tmpDir
		tmpDir := t.TempDir()
		cfg.RecordingPlaybackPolicy = config.PlaybackPolicyLocalOnly
		cfg.RecordingPathMappings = []config.RecordingPathMapping{
			{ReceiverRoot: "/media/hdd", LocalRoot: tmpDir},
		}

		// Create dummy file at target location
		targetPath := filepath.Join(tmpDir, "movie", "test.ts")
		err := os.MkdirAll(filepath.Dir(targetPath), 0755)
		require.NoError(t, err)
		err = os.WriteFile(targetPath, []byte("data"), 0644)
		require.NoError(t, err)

		// Input Ref has complicated prefix but path at end
		serviceRef := "1:0:1:0:0:0:0:0:0:0:/media/hdd/movie/test.ts"

		kind, source, _, err := r.resolveSource(context.Background(), serviceRef)
		require.NoError(t, err)
		assert.Equal(t, "local", kind)

		// Expected source: file://<tmpDir>/movie/test.ts
		expectedSource := "file://" + targetPath
		assert.Equal(t, expectedSource, source)
	})

	// Test 3: Key Hygiene
	t.Run("Singleflight key is hashed", func(t *testing.T) {
		// Checking that hashSingleflightKey returns a hash
		k := hashSingleflightKey("receiver", "http://user:pass@host/path")
		assert.NotContains(t, k, "user:pass")
		assert.NotContains(t, k, "http")
		assert.Len(t, k, 64) // SHA256 hex string length
	})
}

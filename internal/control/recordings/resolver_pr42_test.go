package recordings

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

// TestPR42_SourceResolutionAndKeyHygiene covers PR4.2 requirements.
func TestPR42_SourceResolutionAndKeyHygiene(t *testing.T) {
	cfg := &config.AppConfig{
		Enigma2: config.Enigma2Settings{
			BaseURL:    "http://receiver:80",
			StreamPort: 8001,
		},
		// Policy: "receiver_only" to force receiver path
		RecordingPlaybackPolicy: config.PlaybackPolicyReceiverOnly,
	}
	mgr := vod.NewManager(nil, nil, nil)
	r := NewResolver(cfg, mgr, ResolverOptions{})

	t.Run("Receiver URL escaping uses RawPath correctly", func(t *testing.T) {
		serviceRef := "1:0:1:1:1:1:1:0:0:0:/media/hdd/movie/My Recording.ts"

		kind, source, _, err := r.resolveSource(context.Background(), serviceRef)
		require.NoError(t, err)
		assert.Equal(t, "receiver", kind)

		u, err := url.Parse(source)
		require.NoError(t, err)

		expectedSubstr := "/1:0:1:1:1:1:1:0:0:0:/media/hdd/movie/My%20Recording.ts"
		s := u.String()

		assert.Contains(t, s, expectedSubstr)
		assert.Contains(t, s, ":0:")
		assert.NotContains(t, strings.ToLower(s), "%3a")
		assert.NotContains(t, s, "%2520")
		assert.Contains(t, s, "%20")
	})

	t.Run("Local mapping uses extracted path", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg.RecordingPlaybackPolicy = config.PlaybackPolicyLocalOnly
		cfg.RecordingPathMappings = []config.RecordingPathMapping{
			{ReceiverRoot: "/media/hdd", LocalRoot: tmpDir},
		}

		targetPath := filepath.Join(tmpDir, "movie", "test.ts")
		err := os.MkdirAll(filepath.Dir(targetPath), 0755)
		require.NoError(t, err)
		err = os.WriteFile(targetPath, []byte("data"), 0644)
		require.NoError(t, err)

		serviceRef := "1:0:1:0:0:0:0:0:0:0:/media/hdd/movie/test.ts"

		kind, source, _, err := r.resolveSource(context.Background(), serviceRef)
		require.NoError(t, err)
		assert.Equal(t, "local", kind)

		expectedSource := "file://" + targetPath
		assert.Equal(t, expectedSource, source)
	})

	t.Run("Local mapping escapes spaces in file URL", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg.RecordingPlaybackPolicy = config.PlaybackPolicyLocalOnly
		cfg.RecordingPathMappings = []config.RecordingPathMapping{
			{ReceiverRoot: "/media/hdd", LocalRoot: tmpDir},
		}

		targetPath := filepath.Join(tmpDir, "movie", "My Recording.ts")
		err := os.MkdirAll(filepath.Dir(targetPath), 0755)
		require.NoError(t, err)
		err = os.WriteFile(targetPath, []byte("data"), 0644)
		require.NoError(t, err)

		serviceRef := "1:0:1:0:0:0:0:0:0:0:/media/hdd/movie/My Recording.ts"

		kind, source, _, err := r.resolveSource(context.Background(), serviceRef)
		require.NoError(t, err)
		assert.Equal(t, "local", kind)

		expectedSource := (&url.URL{Scheme: "file", Path: targetPath}).String()
		assert.Equal(t, expectedSource, source)
		assert.NotContains(t, source, " ")
		assert.Contains(t, source, "%20")
	})

	t.Run("Singleflight key is hashed", func(t *testing.T) {
		k := hashSingleflightKey("receiver", "http://user:pass@host/path")
		assert.NotContains(t, k, "user:pass")
		assert.NotContains(t, k, "http")
		assert.Len(t, k, 64)
	})
}

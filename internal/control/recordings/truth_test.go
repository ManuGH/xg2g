package recordings

import (
	"context"
	"errors"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/ManuGH/xg2g/internal/control/vod"
)

// TestPR42_SourceResolutionAndKeyHygiene covers PR4.2 requirements.
// Adapted for TruthProvider.
func TestPR42_SourceResolutionAndKeyHygiene(t *testing.T) {
	cfg := &config.AppConfig{
		Enigma2: config.Enigma2Settings{
			BaseURL:    "http://receiver:80",
			StreamPort: 8001,
		},
		// Policy: "receiver_only" to force receiver path
		RecordingPlaybackPolicy: config.PlaybackPolicyReceiverOnly,
	}
	// Use mocks consistent with package
	mgr, err := vod.NewManager(&dummyRunner{}, &MockProber{}, nil)
	require.NoError(t, err)
	tp, err := NewTruthProvider(cfg, mgr, ResolverOptions{})
	require.NoError(t, err)

	t.Run("Receiver URL escaping uses RawPath correctly", func(t *testing.T) {
		serviceRef := "1:0:1:1:1:1:1:0:0:0:/media/hdd/movie/My Recording.ts"

		kind, source, _, err := tp.resolveSource(context.Background(), serviceRef)
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
		err := os.MkdirAll(filepath.Dir(targetPath), 0750)
		require.NoError(t, err)
		err = os.WriteFile(targetPath, []byte("data"), 0600)
		require.NoError(t, err)

		serviceRef := "1:0:1:0:0:0:0:0:0:0:/media/hdd/movie/test.ts"

		kind, source, _, err := tp.resolveSource(context.Background(), serviceRef)
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
		err := os.MkdirAll(filepath.Dir(targetPath), 0750)
		require.NoError(t, err)
		err = os.WriteFile(targetPath, []byte("data"), 0600)
		require.NoError(t, err)

		serviceRef := "1:0:1:0:0:0:0:0:0:0:/media/hdd/movie/My Recording.ts"

		kind, source, _, err := tp.resolveSource(context.Background(), serviceRef)
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

func TestTruthProvider_ImpossibleProbe_FailsFast(t *testing.T) {
	cfg := &config.AppConfig{
		Enigma2: config.Enigma2Settings{BaseURL: "http://test:80"},
	}
	// Mock manager: empty metadata, no local path
	mgr := &mockManager{
		data: make(map[string]vod.Metadata),
	}

	tests := []struct {
		name        string
		localPath   string
		probe       func(context.Context, string) error
		wantError   error
		wantState   string
		expectProbe bool
	}{
		{
			name:        "Impossible: No Local Path + No Probe Configured",
			localPath:   "",
			probe:       nil,
			wantError:   playback.ErrUpstream,
			expectProbe: false,
		},
		{
			name:      "Impossible: No Local Path + Remote Probe Unsupported",
			localPath: "",
			probe: func(ctx context.Context, s string) error {
				return ErrRemoteProbeUnsupported // Simulate remote not supported
			},
			wantError:   ErrRemoteProbeUnsupported, // Should be passed through
			expectProbe: false,                     // Should fail fast before async
		},
		{
			name:        "Possible: Local Path Exists",
			localPath:   "/tmp/exists",
			probe:       nil, // Mocked existence check in code won't actually check FS in this unit test logic unless we mock PathResolver
			wantError:   nil,
			wantState:   playback.StatePreparing,
			expectProbe: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup PathResolver
			opts := ResolverOptions{ProbeFn: tt.probe}
			if tt.localPath != "" {
				opts.PathResolver = &mockPathResolver{path: tt.localPath}
			}

			tp, err := NewTruthProvider(cfg, mgr, opts)
			require.NoError(t, err)

			// We must ensure the file exists for the "local path" logic to work if it checks FS?
			// The logic in GetMediaTruth is:
			// if t.pathResolver != nil { resolved = ... }
			// fallback to file:// ...
			// The simplified test here assumes pathResolver works.

			got, err := tp.GetMediaTruth(context.Background(), "ref")

			if tt.wantError != nil {
				// We expect an error
				// Check strict equality or error wrapping depending on implementation
				// ErrRemoteProbeUnsupported is specific. ErrUpstream is generic.
				if errors.Is(tt.wantError, ErrRemoteProbeUnsupported) {
					assert.True(t, errors.Is(err, ErrRemoteProbeUnsupported), "expected ErrRemoteProbeUnsupported, got %v", err)
				} else {
					assert.Error(t, err)
					assert.True(t, errors.Is(err, tt.wantError), "expected error %v, got %v", tt.wantError, err)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantState, got.State)
			}
		})
	}
}

func TestTruthProvider_ProbeFailure_PersistsState(t *testing.T) {
	cfg := &config.AppConfig{}

	// Use a channel to synchronize probe execution
	probeCalled := make(chan struct{}, 10)
	probeResultErr := errors.New("probe failed permanently")

	// Seed with existing metadata (ResolvedPath) to check non-destructive update
	initialMeta := vod.Metadata{
		ResolvedPath: "/tmp/existing/path.ts",
		UpdatedAt:    1000,
	}
	mgr := &mockManager{
		data: map[string]vod.Metadata{
			"ref": initialMeta,
		},
		ProbeHook: func(ctx context.Context, path string) (*vod.StreamInfo, error) {
			probeCalled <- struct{}{} // Signal probe attempt
			return nil, probeResultErr
		},
	}

	// We need a path resolver to bypass the "Impossible Probe" gate
	opts := ResolverOptions{
		ProbeFn:      nil, // We rely on local probe via manager
		PathResolver: &mockPathResolver{path: "/tmp/fake"},
	}

	tp, err := NewTruthProvider(cfg, mgr, opts)
	require.NoError(t, err)

	// 1. First Call: Should return Preparing and trigger probe
	got, err := tp.GetMediaTruth(context.Background(), "ref")
	require.NoError(t, err)
	assert.Equal(t, playback.StatePreparing, got.State)

	// Wait for async probe to error out and persist FAILED state.
	// We rely on Eventually because we don't have a callback hook in this simple mock.
	assert.Eventually(t, func() bool {
		m, ok := mgr.GetMetadata("ref")
		return ok && m.State == vod.ArtifactStateFailed
	}, 1*time.Second, 10*time.Millisecond, "metadata state should be FAILED")

	// Verify persistence details
	finalMeta, ok := mgr.GetMetadata("ref")
	require.True(t, ok)
	assert.Equal(t, vod.ArtifactStateFailed, finalMeta.State)
	assert.Equal(t, "probe failed permanently", finalMeta.Error)
	assert.Equal(t, "/tmp/existing/path.ts", finalMeta.ResolvedPath, "Existing ResolvedPath should be preserved (non-destructive update)")
	assert.Greater(t, finalMeta.UpdatedAt, initialMeta.UpdatedAt, "UpdatedAt should satisfy monotonicity")

	// 2. Second Call: Should return Upstream Error and NOT probe
	// Clear the channel
drain:
	for {
		select {
		case <-probeCalled:
		default:
			break drain
		}
	}

	got2, err2 := tp.GetMediaTruth(context.Background(), "ref")
	// Expect Terminal Error
	assert.Error(t, err2)
	assert.True(t, errors.Is(err2, playback.ErrUpstream), "expected ErrUpstream for failed artifact")
	assert.Equal(t, playback.MediaTruth{}, got2)

	// Ensure no probe triggered
	select {
	case <-probeCalled:
		t.Fatal("probe should not have been called again")
	case <-time.After(100 * time.Millisecond):
		// OK
	}
}

// Mocks for TruthProvider Test

type mockPathResolver struct {
	path string
}

func (m *mockPathResolver) ResolveRecordingPath(ref string) (string, string, string, error) {
	if m.path == "" {
		return "", "", "", errors.New("not found")
	}
	return m.path, "root", "rel", nil
}

type mockManager struct {
	mu        sync.Mutex
	data      map[string]vod.Metadata
	ProbeHook func(ctx context.Context, path string) (*vod.StreamInfo, error)
}

func (m *mockManager) Get(ctx context.Context, dir string) (*vod.JobStatus, bool) {
	return nil, false
}
func (m *mockManager) GetMetadata(ref string) (vod.Metadata, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[ref]
	return v, ok
}

// SeedMetadata is for test setup only
func (m *mockManager) SeedMetadata(ref string, meta vod.Metadata) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[ref] = meta
}

func (m *mockManager) MarkFailed(ref string, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	meta, ok := m.data[ref]
	if !ok {
		meta = vod.Metadata{State: vod.ArtifactStateUnknown}
	}
	meta.State = vod.ArtifactStateFailed
	meta.Error = reason
	meta.UpdatedAt = time.Now().UnixNano()
	m.data[ref] = meta
}

func (m *mockManager) MarkFailure(ref string, state vod.ArtifactState, reason string, resolvedPath string, fp *vod.Fingerprint) {
	m.mu.Lock()
	defer m.mu.Unlock()
	meta, ok := m.data[ref]
	if !ok {
		meta = vod.Metadata{State: vod.ArtifactStateUnknown}
	}
	meta.State = state
	meta.Error = reason
	if resolvedPath != "" {
		meta.ResolvedPath = resolvedPath
	}
	meta.UpdatedAt = time.Now().UnixNano()
	m.data[ref] = meta
}

func (m *mockManager) MarkProbed(ref string, resolvedPath string, info *vod.StreamInfo, fp *vod.Fingerprint) {
	m.mu.Lock()
	defer m.mu.Unlock()
	meta, ok := m.data[ref]
	if !ok {
		meta = vod.Metadata{State: vod.ArtifactStateUnknown}
	}
	if resolvedPath != "" {
		meta.ResolvedPath = resolvedPath
	}
	meta.State = vod.ArtifactStateReady
	meta.Error = ""
	if info != nil {
		meta.Duration = int64(math.Round(info.Video.Duration))
		meta.Container = info.Container
		meta.VideoCodec = info.Video.CodecName
		meta.AudioCodec = info.Audio.CodecName
	}
	meta.UpdatedAt = time.Now().UnixNano()
	m.data[ref] = meta
}

func (m *mockManager) Probe(ctx context.Context, path string) (*vod.StreamInfo, error) {
	if m.ProbeHook != nil {
		return m.ProbeHook(ctx, path)
	}
	return nil, errors.New("local probe mock not impl")
}
func (m *mockManager) EnsureSpec(ctx context.Context, workDir, recordingID, source, cacheDir, name, finalPath string, profile vod.Profile) (vod.Spec, error) {
	return vod.Spec{}, nil
}

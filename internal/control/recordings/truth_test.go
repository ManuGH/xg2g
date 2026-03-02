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

	t.Run("Receiver URL keeps allowlisted host for SSRF-like service refs", func(t *testing.T) {
		serviceRef := "1:0:1:1:1:1:1:0:0:0:/media/hdd/movie/http://evil.example/clip.ts"

		kind, source, _, err := tp.resolveSource(context.Background(), serviceRef)
		require.NoError(t, err)
		assert.Equal(t, "receiver", kind)

		u, err := url.Parse(source)
		require.NoError(t, err)
		assert.Equal(t, "receiver:8001", u.Host)
		assert.Equal(t, "http", u.Scheme)
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

		// ResolveLocalExisting resolves symlinks; on macOS temp dirs may be under /var -> /private/var.
		resolvedTargetPath, err := filepath.EvalSymlinks(targetPath)
		require.NoError(t, err)
		expectedSource := "file://" + resolvedTargetPath
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

		resolvedTargetPath, err := filepath.EvalSymlinks(targetPath)
		require.NoError(t, err)
		expectedSource := (&url.URL{Scheme: "file", Path: resolvedTargetPath}).String()
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

func TestTruthProvider_UnknownOrUnprobed_ReturnsPreparing(t *testing.T) {
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
			name:        "No Local Path + No Probe Configured",
			localPath:   "",
			probe:       nil,
			wantError:   nil,
			wantState:   playback.StatePreparing,
			expectProbe: false,
		},
		{
			name:      "No Local Path + Remote Probe Unsupported",
			localPath: "",
			probe: func(ctx context.Context, s string) error {
				return ErrRemoteProbeUnsupported // Simulate remote not supported
			},
			wantError:   nil,
			wantState:   playback.StatePreparing,
			expectProbe: true,
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

			outcome, err := tp.GetMediaTruthOutcome(context.Background(), "ref")

			if tt.wantError != nil {
				assert.Error(t, err)
				assert.True(t, errors.Is(err, tt.wantError), "expected error %v, got %v", tt.wantError, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantState, outcome.Truth.State)
				assert.Equal(t, tt.expectProbe, outcome.NeedsProbe)
			}
		})
	}
}

func TestTruthProvider_GetMediaTruthOutcome_NoSideEffects(t *testing.T) {
	cfg := &config.AppConfig{
		Enigma2: config.Enigma2Settings{
			BaseURL:    "http://receiver:80",
			StreamPort: 8001,
		},
		RecordingPlaybackPolicy: config.PlaybackPolicyReceiverOnly,
	}
	mgr := &mockManager{data: make(map[string]vod.Metadata)}

	probeFnCalls := 0
	tp, err := NewTruthProvider(cfg, mgr, ResolverOptions{
		ProbeFn: func(ctx context.Context, sourceURL string) error {
			probeFnCalls++
			return nil
		},
	})
	require.NoError(t, err)

	outcome, err := tp.GetMediaTruthOutcome(context.Background(), "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/no_side_effect.ts")
	require.NoError(t, err)
	assert.Equal(t, playback.StatePreparing, outcome.Truth.State)
	assert.True(t, outcome.NeedsProbe)
	assert.Equal(t, 0, probeFnCalls, "truth classification must not trigger probe side-effects")
}

func TestTruthProvider_ImpossibleProbe_OptionAContract(t *testing.T) {
	cfg := &config.AppConfig{
		Enigma2: config.Enigma2Settings{
			BaseURL:    "http://receiver:80",
			StreamPort: 8001,
		},
		RecordingPlaybackPolicy: config.PlaybackPolicyReceiverOnly,
	}
	mgr := &mockManager{data: make(map[string]vod.Metadata)}

	disabledTP, err := NewTruthProvider(cfg, mgr, ResolverOptions{})
	require.NoError(t, err)

	disabledOutcome, err := disabledTP.GetMediaTruthOutcome(context.Background(), "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/disabled.ts")
	require.NoError(t, err)
	assert.Equal(t, playback.StatePreparing, disabledOutcome.Truth.State)
	assert.Equal(t, playback.ProbeStateBlocked, disabledOutcome.Truth.ProbeState)
	assert.Equal(t, playback.ProbeBlockedReasonDisabled, disabledOutcome.Truth.ProbeBlockedReason)
	assert.Equal(t, playback.RetryAfterPreparingBlockedDefault, disabledOutcome.Truth.RetryAfterSeconds)
	assert.False(t, disabledOutcome.NeedsProbe, "disabled probe path must not schedule probing")

	enabledTP, err := NewTruthProvider(cfg, mgr, ResolverOptions{
		ProbeFn: func(ctx context.Context, sourceURL string) error { return ErrRemoteProbeUnsupported },
	})
	require.NoError(t, err)

	enabledOutcome, err := enabledTP.GetMediaTruthOutcome(context.Background(), "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/enabled.ts")
	require.NoError(t, err)
	assert.Equal(t, playback.StatePreparing, enabledOutcome.Truth.State)
	assert.True(t, enabledOutcome.NeedsProbe, "configured remote probe path must request scheduling")
	assert.Equal(t, playback.ProbeStateUnknown, enabledOutcome.Truth.ProbeState)
	assert.Equal(t, playback.ProbeBlockedReasonNone, enabledOutcome.Truth.ProbeBlockedReason)
	assert.Equal(t, playback.RetryAfterPreparingDefault, enabledOutcome.Truth.RetryAfterSeconds)
	assert.NotEmpty(t, enabledOutcome.ProbeHint.Source)
}

func TestResolver_ProbeTriggerTTL_RemotePath(t *testing.T) {
	cfg := &config.AppConfig{
		Enigma2: config.Enigma2Settings{
			BaseURL:    "http://receiver:80",
			StreamPort: 8001,
		},
		RecordingPlaybackPolicy: config.PlaybackPolicyReceiverOnly,
	}
	mgr := &mockManager{data: make(map[string]vod.Metadata)}

	var mu sync.Mutex
	probeCalls := 0
	resolver, err := NewResolver(cfg, mgr, ResolverOptions{
		ProbeFn: func(ctx context.Context, sourceURL string) error {
			mu.Lock()
			probeCalls++
			mu.Unlock()
			return ErrRemoteProbeUnsupported // soft-fail: should stay preparing
		},
	})
	require.NoError(t, err)
	r := resolver.(*PlaybackInfoResolver)
	r.probeTTL = 120 * time.Millisecond

	serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/ttl.ts"

	for i := 0; i < 3; i++ {
		got, err := r.GetMediaTruth(context.Background(), serviceRef)
		require.NoError(t, err)
		assert.Equal(t, playback.StatePreparing, got.State)
	}

	assert.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return probeCalls == 1
	}, 500*time.Millisecond, 10*time.Millisecond, "probe should be throttled within TTL")

	time.Sleep(140 * time.Millisecond)
	got, err := r.GetMediaTruth(context.Background(), serviceRef)
	require.NoError(t, err)
	assert.Equal(t, playback.StatePreparing, got.State)

	assert.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return probeCalls >= 2
	}, 500*time.Millisecond, 10*time.Millisecond, "probe should run again after TTL")
}

func TestResolver_RemoteProbeHardFailure_BecomesUpstream(t *testing.T) {
	cfg := &config.AppConfig{
		Enigma2: config.Enigma2Settings{
			BaseURL:    "http://receiver:80",
			StreamPort: 8001,
		},
		RecordingPlaybackPolicy: config.PlaybackPolicyReceiverOnly,
	}
	mgr := &mockManager{data: make(map[string]vod.Metadata)}

	resolver, err := NewResolver(cfg, mgr, ResolverOptions{
		ProbeFn: func(ctx context.Context, sourceURL string) error {
			return errors.New("connection refused")
		},
	})
	require.NoError(t, err)
	r := resolver.(*PlaybackInfoResolver)
	r.probeTTL = 20 * time.Millisecond

	serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/upstream.ts"
	got, err := r.GetMediaTruth(context.Background(), serviceRef)
	require.NoError(t, err)
	assert.Equal(t, playback.StatePreparing, got.State)

	assert.Eventually(t, func() bool {
		m, ok := mgr.GetMetadata(serviceRef)
		return ok && m.State == vod.ArtifactStateFailed && strings.HasPrefix(m.Error, remoteProbeErrorPrefix)
	}, 1*time.Second, 10*time.Millisecond)

	got2, err := r.GetMediaTruth(context.Background(), serviceRef)
	assert.ErrorIs(t, err, playback.ErrUpstream)
	assert.Equal(t, playback.StateFailed, got2.State)
}

func TestResolver_GetMediaTruth_TerminalErrorsKeepTruthPayload(t *testing.T) {
	cfg := &config.AppConfig{}
	mgr := &mockManager{
		data: map[string]vod.Metadata{
			"missing-ref": {
				State:      vod.ArtifactStateMissing,
				Container:  "mp4",
				VideoCodec: "h264",
				AudioCodec: "aac",
				Duration:   120,
				Width:      1920,
				Height:     1080,
			},
			"upstream-ref": {
				State:      vod.ArtifactStateFailed,
				Error:      remoteProbeErrorPrefix + "dial tcp timeout",
				Container:  "ts",
				VideoCodec: "h264",
				AudioCodec: "mp2",
				Duration:   61,
			},
		},
	}

	resolver, err := NewResolver(cfg, mgr, ResolverOptions{})
	require.NoError(t, err)
	r := resolver.(*PlaybackInfoResolver)

	notFoundTruth, notFoundErr := r.GetMediaTruth(context.Background(), "missing-ref")
	require.ErrorIs(t, notFoundErr, playback.ErrNotFound)
	assert.Equal(t, playback.StateFailed, notFoundTruth.State)
	assert.Equal(t, "mp4", notFoundTruth.Container)
	assert.Equal(t, 120.0, notFoundTruth.Duration)
	assert.Equal(t, 1920, notFoundTruth.Width)
	assert.Equal(t, 1080, notFoundTruth.Height)

	upstreamTruth, upstreamErr := r.GetMediaTruth(context.Background(), "upstream-ref")
	require.ErrorIs(t, upstreamErr, playback.ErrUpstream)
	assert.Equal(t, playback.StateFailed, upstreamTruth.State)
	assert.Equal(t, "ts", upstreamTruth.Container)
	assert.Equal(t, "h264", upstreamTruth.VideoCodec)
	assert.Equal(t, "mp2", upstreamTruth.AudioCodec)
	assert.Equal(t, 61.0, upstreamTruth.Duration)
}

func TestResolver_ProbeFailure_PersistsState(t *testing.T) {
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

	resolver, err := NewResolver(cfg, mgr, opts)
	require.NoError(t, err)
	r := resolver.(*PlaybackInfoResolver)

	// 1. First Call: Should return Preparing and trigger probe
	got, err := r.GetMediaTruth(context.Background(), "ref")
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

	got2, err2 := r.GetMediaTruth(context.Background(), "ref")
	// Expect Terminal Error
	assert.Error(t, err2)
	assert.True(t, errors.Is(err2, playback.ErrUpstream), "expected ErrUpstream for failed artifact")
	assert.Equal(t, playback.StateFailed, got2.State)

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

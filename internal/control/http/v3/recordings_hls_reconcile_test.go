package v3

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/config"
	v3recordings "github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/ManuGH/xg2g/internal/recordings"
)

type blockingHandle struct {
	progress chan vod.ProgressEvent
	waitCh   chan struct{}
}

func newBlockingHandle() *blockingHandle {
	return &blockingHandle{
		progress: make(chan vod.ProgressEvent),
		waitCh:   make(chan struct{}),
	}
}

func (h *blockingHandle) Wait() error {
	<-h.waitCh
	return nil
}

func (h *blockingHandle) Stop(grace, kill time.Duration) error {
	select {
	case <-h.waitCh:
	default:
		close(h.waitCh)
	}
	return nil
}

func (h *blockingHandle) Progress() <-chan vod.ProgressEvent {
	return h.progress
}

func (h *blockingHandle) Diagnostics() []string {
	return []string{"blocked"}
}

type countingRunner struct {
	starts  int32
	started chan struct{}
	once    sync.Once
}

func (r *countingRunner) Start(ctx context.Context, spec vod.Spec) (vod.Handle, error) {
	atomic.AddInt32(&r.starts, 1)
	r.once.Do(func() {
		close(r.started)
	})
	// Fix contract violation
	if spec.WorkDir != "" && spec.OutputTemp != "" {
		out := filepath.Join(spec.WorkDir, spec.OutputTemp)
		_ = os.MkdirAll(filepath.Dir(out), 0750)
		_ = os.WriteFile(out, []byte("#EXTM3U"), 0600)
	}
	return newBlockingHandle(), nil
}

type successRunner struct {
	fsRoot string
}

func (r *successRunner) Start(ctx context.Context, spec vod.Spec) (vod.Handle, error) {
	// Create dummy output immediately to satisfy monitor
	if spec.WorkDir != "" && spec.OutputTemp != "" {
		out := filepath.Join(spec.WorkDir, spec.OutputTemp)
		// Ensure dir exists
		_ = os.MkdirAll(filepath.Dir(out), 0750)
		_ = os.WriteFile(out, []byte("#EXTM3U"), 0600)
	}
	return &immediateHandle{}, nil
}

type immediateHandle struct{}

func (h *immediateHandle) Wait() error {
	return nil
}

func (h *immediateHandle) Stop(grace, kill time.Duration) error {
	return nil
}

func (h *immediateHandle) Progress() <-chan vod.ProgressEvent {
	ch := make(chan vod.ProgressEvent)
	close(ch)
	return ch
}

func (h *immediateHandle) Diagnostics() []string {
	return nil
}

type slowProber struct {
	delay time.Duration
}

func (p *slowProber) Probe(ctx context.Context, path string) (*vod.StreamInfo, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(p.delay):
		return &vod.StreamInfo{Video: vod.VideoStreamInfo{Duration: 42}}, nil
	}
}

// signalRunner wraps successRunner and signals when Start has completed its IO.
type signalRunner struct {
	delegate *successRunner
	done     chan struct{}
	once     sync.Once
}

func (r *signalRunner) Start(ctx context.Context, spec vod.Spec) (vod.Handle, error) {
	defer r.once.Do(func() { close(r.done) })
	return r.delegate.Start(ctx, spec)
}

func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func TestGetRecordingHLSPlaylist_FailedPromotesReady(t *testing.T) {
	serviceRef := "1:0:0:0:0:0:0:0:0:/media/test.ts"
	recordingID := recservice.EncodeRecordingID(serviceRef)
	hlsRoot := t.TempDir()
	cfg := config.AppConfig{
		HLS: config.HLSConfig{
			Root: hlsRoot,
		},
	}

	srv := NewServer(cfg, nil, nil)
	// Use successRunner
	vodMgr, err := vod.NewManager(&successRunner{fsRoot: t.TempDir()}, &noopProber{}, nil)
	require.NoError(t, err)
	defer vodMgr.Shutdown()
	srv.SetDependencies(Dependencies{VODManager: vodMgr})

	cacheDir := filepath.Join(hlsRoot, "recordings", v3recordings.RecordingCacheKey(serviceRef))
	require.NoError(t, os.MkdirAll(cacheDir, 0750))

	playlistPath := filepath.Join(cacheDir, "index.m3u8")
	playlist := "#EXTM3U\n#EXT-X-ENDLIST\nseg_0001.ts\n"
	require.NoError(t, os.WriteFile(playlistPath, []byte(playlist), 0600))

	vodMgr.SeedMetadata(serviceRef, vod.Metadata{
		State:        vod.ArtifactStateFailed,
		PlaylistPath: playlistPath,
		Error:        "boom",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+recordingID+"/playlist.m3u8", nil)
	rr := httptest.NewRecorder()
	srv.GetRecordingHLSPlaylist(rr, req, recordingID)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Contains(t, rr.Body.String(), "#EXT-X-PLAYLIST-TYPE:VOD")
}

func TestGetRecordingHLSPlaylist_Failed_Reconcile_BuildCallbackPromotesReady(t *testing.T) {
	serviceRef := "1:0:0:0:0:0:0:0:0:/media/nfs/build.ts"
	recordingID := recservice.EncodeRecordingID(serviceRef)

	cfg := config.AppConfig{
		HLS: config.HLSConfig{
			Root: t.TempDir(),
		},
	}

	localRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(localRoot, "nfs"), 0750))
	require.NoError(t, os.WriteFile(filepath.Join(localRoot, "nfs/build.ts"), []byte("dummy"), 0600))

	mapper := recordings.NewPathMapper([]config.RecordingPathMapping{
		{ReceiverRoot: "/media", LocalRoot: localRoot},
	})

	srv := NewServer(cfg, nil, nil)
	// Use successRunner
	vodMgr, err := vod.NewManager(&successRunner{fsRoot: t.TempDir()}, &noopProber{}, mapper)
	require.NoError(t, err)
	defer vodMgr.Shutdown()
	srv.SetDependencies(Dependencies{VODManager: vodMgr, PathMapper: mapper})

	vodMgr.SeedMetadata(serviceRef, vod.Metadata{
		State: vod.ArtifactStateFailed,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+recordingID+"/playlist.m3u8", nil)
	rr := httptest.NewRecorder()
	srv.GetRecordingHLSPlaylist(rr, req, recordingID)
	require.Equal(t, http.StatusServiceUnavailable, rr.Code)

	require.Eventually(t, func() bool {
		meta, ok := vodMgr.GetMetadata(serviceRef)
		return ok && meta.State == vod.ArtifactStateReady && meta.PlaylistPath != ""
	}, 2*time.Second, 10*time.Millisecond)

	cacheDir, err := v3recordings.RecordingCacheDir(cfg.HLS.Root, serviceRef)
	require.NoError(t, err)

	_ = cacheDir
}

func TestGetRecordingHLSPlaylist_FailedStampedeTriggersSingleBuild(t *testing.T) {
	serviceRef := "1:0:0:0:0:0:0:0:0:/media/nfs/test.ts"
	recordingID := recservice.EncodeRecordingID(serviceRef)
	cfg := config.AppConfig{
		HLS: config.HLSConfig{
			Root: t.TempDir(),
		},
	}

	runner := &countingRunner{
		started: make(chan struct{}),
	}

	localRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(localRoot, "nfs"), 0750))
	require.NoError(t, os.WriteFile(filepath.Join(localRoot, "nfs/test.ts"), []byte("dummy"), 0600))

	mapper := recordings.NewPathMapper([]config.RecordingPathMapping{
		{ReceiverRoot: "/media", LocalRoot: localRoot},
	})

	srv := NewServer(cfg, nil, nil)
	vodMgr, err := vod.NewManager(runner, &noopProber{}, mapper)
	require.NoError(t, err)
	defer vodMgr.Shutdown()
	srv.SetDependencies(Dependencies{PathMapper: mapper, VODManager: vodMgr})

	vodMgr.SeedMetadata(serviceRef, vod.Metadata{
		State: vod.ArtifactStateFailed,
	})

	// Concurrent requests should trigger ONE transition/build
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+recordingID+"/playlist.m3u8", nil)
			rr := httptest.NewRecorder() // Each goroutine needs its own recorder
			srv.GetRecordingHLSPlaylist(rr, req, recordingID)
		}()
	}
	wg.Wait()

	select {
	case <-runner.started:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected build to start")
	}

	require.Equal(t, int32(1), atomic.LoadInt32(&runner.starts))
}

func TestGetRecordingHLSPlaylist_FailedLatencySLO(t *testing.T) {
	serviceRef := "1:0:0:0:0:0:0:0:0:/media/nfs/latency.ts"
	recordingID := recservice.EncodeRecordingID(serviceRef)
	cfg := config.AppConfig{
		HLS: config.HLSConfig{
			Root: t.TempDir(),
		},
	}

	// Use signalRunner to deterministically wait for async IO
	runner := &signalRunner{
		delegate: &successRunner{fsRoot: t.TempDir()},
		done:     make(chan struct{}),
	}
	localRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(localRoot, "nfs"), 0750))
	require.NoError(t, os.WriteFile(filepath.Join(localRoot, "nfs/latency.ts"), []byte("dummy"), 0600))

	mapper := recordings.NewPathMapper([]config.RecordingPathMapping{
		{ReceiverRoot: "/media", LocalRoot: localRoot},
	})

	vodMgr, err := vod.NewManager(runner, &noopProber{}, mapper)
	require.NoError(t, err)
	defer vodMgr.Shutdown()

	srv := NewServer(cfg, nil, nil)
	srv.SetDependencies(Dependencies{VODManager: vodMgr, PathMapper: mapper})

	vodMgr.SeedMetadata(serviceRef, vod.Metadata{
		State: vod.ArtifactStateFailed,
		Error: "MKDIR_FAIL", // Non-recoverable without action
	})

	start := time.Now()
	req := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+recordingID+"/playlist.m3u8", nil)
	rr := httptest.NewRecorder()
	srv.GetRecordingHLSPlaylist(rr, req, recordingID)

	// Should return 503 quickly (reconcile triggered async), not block
	require.Less(t, time.Since(start), 50*time.Millisecond)
	require.Equal(t, http.StatusServiceUnavailable, rr.Code)

	// Wait deterministically for async build start/completion.
	require.Eventually(t, func() bool {
		return isClosed(runner.done)
	}, 2*time.Second, 10*time.Millisecond, "expected async reconcile build to start")

	// Double-check readiness to be absolutely sure all business logic finished.
	// We wait for a terminal state (READY or FAILED) to ensure the VOD manager
	// has released all resources and updated the metadata store.
	require.Eventually(t, func() bool {
		m, ok := vodMgr.GetMetadata(serviceRef)
		return ok && (m.State == vod.ArtifactStateReady || m.State == vod.ArtifactStateFailed)
	}, 2*time.Second, 10*time.Millisecond, "HLS build lifecycle did not reach terminal state (READY/FAILED) within timeout")
}

func TestGetRecordingHLSPlaylist_OpenFailure_ReconcileReady(t *testing.T) {
	serviceRef := "1:0:0:0:0:0:0:0:0:/media/test.ts"
	recordingID := recservice.EncodeRecordingID(serviceRef)
	localRoot := t.TempDir()

	// Create dummy input file so resolveSource doesn't fail
	inputPath := filepath.Join(localRoot, "test.ts")
	require.NoError(t, os.WriteFile(inputPath, []byte("input"), 0600))

	cfg := config.AppConfig{
		HLS: config.HLSConfig{
			Root: t.TempDir(),
		},
	}

	pathMapper := recordings.NewPathMapper([]config.RecordingPathMapping{
		{ReceiverRoot: "/media", LocalRoot: localRoot},
	})

	runner := &signalRunner{
		delegate: &successRunner{fsRoot: t.TempDir()},
		done:     make(chan struct{}),
	}

	srv := NewServer(cfg, nil, nil)
	vodMgr, err := vod.NewManager(runner, &slowProber{delay: 50 * time.Millisecond}, pathMapper)
	require.NoError(t, err)
	defer vodMgr.Shutdown()
	srv.SetDependencies(Dependencies{VODManager: vodMgr, PathMapper: pathMapper})

	cacheDir, err := v3recordings.RecordingCacheDir(cfg.HLS.Root, serviceRef)
	require.NoError(t, err)

	vodMgr.SeedMetadata(serviceRef, vod.Metadata{
		State:        vod.ArtifactStateReady,
		PlaylistPath: filepath.Join(cacheDir, "index.m3u8"),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+recordingID+"/playlist.m3u8", nil)
	rr := httptest.NewRecorder()
	srv.GetRecordingHLSPlaylist(rr, req, recordingID)
	// Open fails (playlist missing), should return PREPARING while reconcile starts.
	require.Equal(t, http.StatusServiceUnavailable, rr.Code)

	require.Eventually(t, func() bool {
		return isClosed(runner.done)
	}, 2*time.Second, 10*time.Millisecond, "expected reconcile build to start")

	require.Eventually(t, func() bool {
		meta, ok := vodMgr.GetMetadata(serviceRef)
		return ok && meta.State == vod.ArtifactStateReady && meta.PlaylistPath != ""
	}, 3*time.Second, 10*time.Millisecond, "expected reconcile to repopulate READY metadata")

	require.Eventually(t, func() bool {
		req2 := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+recordingID+"/playlist.m3u8", nil)
		rr2 := httptest.NewRecorder()
		srv.GetRecordingHLSPlaylist(rr2, req2, recordingID)
		return rr2.Code == http.StatusOK
	}, 3*time.Second, 20*time.Millisecond, "expected playlist to become available after reconcile")
}

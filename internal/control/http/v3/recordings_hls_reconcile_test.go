package v3

import (
	"context"
	"encoding/json"
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
	starts   int32
	started  chan struct{}
	once     sync.Once
	blocking *blockingHandle
}

func newCountingRunner() *countingRunner {
	return &countingRunner{
		started:  make(chan struct{}),
		blocking: newBlockingHandle(),
	}
}

func (r *countingRunner) Start(ctx context.Context, spec vod.Spec) (vod.Handle, error) {
	if err := os.MkdirAll(spec.WorkDir, 0755); err != nil {
		return nil, err
	}
	outputPath := filepath.Join(spec.WorkDir, spec.OutputTemp)
	if err := os.WriteFile(outputPath, []byte("#EXTM3U\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-ENDLIST\nseg_0001.ts\n"), 0600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(spec.WorkDir, "seg_0001.ts"), []byte{}, 0600); err != nil {
		return nil, err
	}
	atomic.AddInt32(&r.starts, 1)
	r.once.Do(func() { close(r.started) })
	return r.blocking, nil
}

type successRunner struct{}

func (r *successRunner) Start(ctx context.Context, spec vod.Spec) (vod.Handle, error) {
	if err := os.MkdirAll(spec.WorkDir, 0755); err != nil {
		return nil, err
	}
	outputPath := filepath.Join(spec.WorkDir, spec.OutputTemp)
	if err := os.WriteFile(outputPath, []byte("#EXTM3U\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-ENDLIST\nseg_0001.ts\n"), 0600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(spec.WorkDir, "seg_0001.ts"), []byte{}, 0600); err != nil {
		return nil, err
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

func TestGetRecordingHLSPlaylist_FailedPromotesReady(t *testing.T) {
	serviceRef := "1:0:0:0:0:0:0:0:0:/media/test.ts"
	recordingID := EncodeRecordingID(serviceRef)
	hlsRoot := t.TempDir()
	cfg := config.AppConfig{
		HLS: config.HLSConfig{
			Root: hlsRoot,
		},
	}

	srv := NewServer(cfg, nil, nil)
	vodMgr := vod.NewManager(nil, nil, nil)
	srv.SetDependencies(nil, nil, nil, nil, nil, nil, nil, nil, vodMgr, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	cacheDir := filepath.Join(hlsRoot, "recordings", RecordingCacheKey(serviceRef))
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

	playlistPath := filepath.Join(cacheDir, "index.m3u8")
	playlist := "#EXTM3U\n#EXT-X-ENDLIST\nseg_0001.ts\n"
	require.NoError(t, os.WriteFile(playlistPath, []byte(playlist), 0600))

	vodMgr.UpdateMetadata(serviceRef, vod.Metadata{
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
	recordingID := EncodeRecordingID(serviceRef)

	cfg := config.AppConfig{
		HLS: config.HLSConfig{
			Root: t.TempDir(),
		},
	}

	srv := NewServer(cfg, nil, nil)
	vodMgr := vod.NewManager(&successRunner{}, nil, nil)
	srv.SetDependencies(nil, nil, nil, nil, nil, nil, nil, nil, vodMgr, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	vodMgr.UpdateMetadata(serviceRef, vod.Metadata{
		State: vod.ArtifactStateFailed,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+recordingID+"/playlist.m3u8", nil)
	rr := httptest.NewRecorder()
	srv.GetRecordingHLSPlaylist(rr, req, recordingID)
	require.Equal(t, http.StatusServiceUnavailable, rr.Code)

	require.Eventually(t, func() bool {
		meta, ok := vodMgr.GetMetadata(serviceRef)
		return ok && meta.State == vod.ArtifactStateReady && meta.PlaylistPath != ""
	}, 500*time.Millisecond, 10*time.Millisecond)

	cacheDir, err := RecordingCacheDir(cfg.HLS.Root, serviceRef)
	require.NoError(t, err)
	require.True(t, RecordingPlaylistReady(cacheDir))

	rr = httptest.NewRecorder()
	srv.GetRecordingHLSPlaylist(rr, req, recordingID)
	require.Equal(t, http.StatusOK, rr.Code)
	require.Contains(t, rr.Body.String(), "#EXT-X-PLAYLIST-TYPE:VOD")
}

func TestGetRecordingHLSPlaylist_FailedStampedeTriggersSingleBuild(t *testing.T) {
	serviceRef := "1:0:0:0:0:0:0:0:0:/media/nfs/test.ts"
	recordingID := EncodeRecordingID(serviceRef)

	cfg := config.AppConfig{
		HLS: config.HLSConfig{
			Root: t.TempDir(),
		},
	}

	srv := NewServer(cfg, nil, nil)
	runner := newCountingRunner()
	vodMgr := vod.NewManager(runner, nil, nil)
	srv.SetDependencies(nil, nil, nil, nil, nil, nil, nil, nil, vodMgr, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	vodMgr.UpdateMetadata(serviceRef, vod.Metadata{
		State: vod.ArtifactStateFailed,
	})

	const concurrency = 20
	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+recordingID+"/playlist.m3u8", nil)
			rr := httptest.NewRecorder()
			srv.GetRecordingHLSPlaylist(rr, req, recordingID)
			require.Equal(t, http.StatusServiceUnavailable, rr.Code)
		}()
	}

	wg.Wait()

	select {
	case <-runner.started:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected build to start")
	}

	require.Equal(t, int32(1), atomic.LoadInt32(&runner.starts))

	meta, ok := vodMgr.GetMetadata(serviceRef)
	require.True(t, ok)
	require.Equal(t, vod.ArtifactStatePreparing, meta.State)

	_ = runner.blocking.Stop(0, 0)

	require.Eventually(t, func() bool {
		updated, ok := vodMgr.GetMetadata(serviceRef)
		return ok && updated.State != vod.ArtifactStatePreparing
	}, 500*time.Millisecond, 10*time.Millisecond)
}

func TestGetRecordingHLSPlaylist_FailedLatencySLO(t *testing.T) {
	serviceRef := "1:0:0:0:0:0:0:0:0:/media/nfs/latency.ts"
	recordingID := EncodeRecordingID(serviceRef)

	cfg := config.AppConfig{
		HLS: config.HLSConfig{
			Root: t.TempDir(),
		},
	}

	srv := NewServer(cfg, nil, nil)
	runner := newCountingRunner()
	vodMgr := vod.NewManager(runner, nil, nil)
	srv.SetDependencies(nil, nil, nil, nil, nil, nil, nil, nil, vodMgr, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	vodMgr.UpdateMetadata(serviceRef, vod.Metadata{
		State: vod.ArtifactStateFailed,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+recordingID+"/playlist.m3u8", nil)
	rr := httptest.NewRecorder()

	start := time.Now()
	srv.GetRecordingHLSPlaylist(rr, req, recordingID)
	duration := time.Since(start)

	require.Less(t, duration, 200*time.Millisecond, "GetRecordingHLSPlaylist too slow in FAILED reconcile")
	require.Equal(t, http.StatusServiceUnavailable, rr.Code)

	_ = runner.blocking.Stop(0, 0)
}

func TestGetRecordingHLSPlaylist_OpenFailure_ReconcileReady(t *testing.T) {
	serviceRef := "1:0:0:0:0:0:0:0:0:/media/test.ts"
	recordingID := EncodeRecordingID(serviceRef)
	localRoot := t.TempDir()
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

	srv := NewServer(cfg, nil, nil)
	vodMgr := vod.NewManager(&successRunner{}, &slowProber{delay: 50 * time.Millisecond}, pathMapper)
	srv.SetDependencies(nil, nil, nil, nil, pathMapper, nil, nil, nil, vodMgr, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	cacheDir, err := RecordingCacheDir(cfg.HLS.Root, serviceRef)
	require.NoError(t, err)

	vodMgr.UpdateMetadata(serviceRef, vod.Metadata{
		State:        vod.ArtifactStateReady,
		PlaylistPath: filepath.Join(cacheDir, "index.m3u8"),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+recordingID+"/playlist.m3u8", nil)
	rr := httptest.NewRecorder()
	srv.GetRecordingHLSPlaylist(rr, req, recordingID)
	require.Equal(t, http.StatusServiceUnavailable, rr.Code)

	require.Eventually(t, func() bool {
		meta, ok := vodMgr.GetMetadata(serviceRef)
		return ok && meta.State == vod.ArtifactStatePreparing
	}, 100*time.Millisecond, 5*time.Millisecond)

	require.Eventually(t, func() bool {
		meta, ok := vodMgr.GetMetadata(serviceRef)
		return ok && meta.State == vod.ArtifactStateReady && meta.ResolvedPath != "" && meta.Duration > 0
	}, 500*time.Millisecond, 10*time.Millisecond)

	rr = httptest.NewRecorder()
	srv.GetRecordingHLSPlaylist(rr, req, recordingID)
	require.Equal(t, http.StatusServiceUnavailable, rr.Code)
	require.Contains(t, rr.Header().Get("Content-Type"), "application/problem+json")
	var prob map[string]interface{}
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&prob))
	codeVal, ok := prob["code"].(string)
	require.True(t, ok)
	require.Equal(t, "PREPARING", codeVal)
	typeVal, ok := prob["type"].(string)
	require.True(t, ok)
	require.Equal(t, "recordings/preparing", typeVal)
	statusVal, ok := prob["status"].(float64)
	require.True(t, ok)
	require.Equal(t, float64(http.StatusServiceUnavailable), statusVal)
	instanceVal, ok := prob["instance"].(string)
	require.True(t, ok)
	require.Equal(t, req.URL.EscapedPath(), instanceVal)

	require.Eventually(t, func() bool {
		meta, ok := vodMgr.GetMetadata(serviceRef)
		return ok && meta.State == vod.ArtifactStateReady && meta.HasPlaylist()
	}, 500*time.Millisecond, 10*time.Millisecond)

	rr = httptest.NewRecorder()
	srv.GetRecordingHLSPlaylist(rr, req, recordingID)
	require.Equal(t, http.StatusOK, rr.Code)
	require.Contains(t, rr.Body.String(), "#EXT-X-PLAYLIST-TYPE:VOD")
}

package v3

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/control/http/v3/recordings/artifacts"
	"github.com/ManuGH/xg2g/internal/control/http/v3/recordings/resolver"
	"github.com/ManuGH/xg2g/internal/control/vod"
)

// TestUIHotPathMechanicalGate_NoSyncIO ensures that the UI hot path handlers (listing/status)
// do not contain calls to forbidden I/O operations (os.Stat, ffprobe, etc.).
// NOTE: Streaming endpoints (StreamRecordingDirect, GetRecordingHLSPlaylist) ARE allowed
// to use os.Open/ServeContent but MUST NOT perform synchronous preflight Stat or probing.
func TestUIHotPathMechanicalGate_NoSyncIO(t *testing.T) {
	forbidden := []string{
		"os.Stat", "os.Open", "filepath.EvalSymlinks", "exec.Command",
		"ffprobe", "ProbeDuration", "ResolveLocalExisting",
	}

	content, err := os.ReadFile("recordings.go")
	require.NoError(t, err)

	lines := strings.Split(string(content), "\n")

	inHotPath := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "func ") {
			inHotPath = false
			if strings.HasPrefix(trimmed, "func (s *Server) GetRecordings") ||
				strings.HasPrefix(trimmed, "func (s *Server) GetRecordingPlaybackInfo") ||
				strings.HasPrefix(trimmed, "func (s *Server) resolveRecordingPlaybackSource") {
				inHotPath = true
			}
		}

		if inHotPath {
			for _, f := range forbidden {
				if strings.Contains(line, f) && !strings.Contains(line, "//") {
					t.Errorf("Forbidden call %q found in hot path at recordings.go:%d", f, i+1)
				}
			}
		}
	}
}

// blockingProber is a mock prober that blocks until the context is canceled.
type blockingProber struct{}

func (p *blockingProber) Probe(ctx context.Context, path string) (*vod.StreamInfo, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

type noopRunner struct{}

func (n *noopRunner) Start(ctx context.Context, spec vod.Spec) (vod.Handle, error) {
	if spec.WorkDir != "" && spec.OutputTemp != "" {
		out := filepath.Join(spec.WorkDir, spec.OutputTemp)
		_ = os.MkdirAll(filepath.Dir(out), 0755)
		_ = os.WriteFile(out, []byte("#EXTM3U"), 0644)
	}
	// Return a handle that satisfies the contract immediately
	return &noopHandle{progress: make(chan vod.ProgressEvent)}, nil
}

type noopHandle struct {
	progress chan vod.ProgressEvent
}

func (h *noopHandle) Wait() error { return nil }
func (h *noopHandle) Stop(grace, kill time.Duration) error {
	select {
	case <-h.progress:
	default:
		close(h.progress)
	}
	return nil
}
func (h *noopHandle) Progress() <-chan vod.ProgressEvent { return h.progress }
func (h *noopHandle) Diagnostics() []string              { return nil }

// TestHotPathLatencySLO verifies that hot endpoints stay responsive even if the backend stalls.
func TestHotPathLatencySLO(t *testing.T) {
	s, _ := newV3TestServer(t, t.TempDir())
	t.Cleanup(func() {
		if s.vodManager != nil {
			s.vodManager.CancelAll()
		}
	})

	// Inject a VOD Manager with a blocking prober
	prober := &blockingProber{}
	mgr := vod.NewManager(&noopRunner{}, prober, nil)
	s.vodManager = mgr
	res := resolver.New(&s.cfg, mgr)
	res.Probe = func(ctx context.Context, sourceURL string) error {
		<-ctx.Done()
		return ctx.Err()
	}
	s.resolver = res
	s.artifacts = artifacts.New(&s.cfg, mgr, nil)

	t.Run("GetRecordings_Under_500ms", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v3/recordings", nil)
		rr := httptest.NewRecorder()

		start := time.Now()
		s.GetRecordings(rr, req, GetRecordingsParams{})
		duration := time.Since(start)

		require.Less(t, duration, 500*time.Millisecond, "GetRecordings too slow")
	})

	t.Run("GetRecordingPlaybackInfo_Under_200ms_StallingProber", func(t *testing.T) {
		serviceRef := "1:0:0:0:0:0:0:0:0:/media/nfs/test.ts"
		recordingID := EncodeRecordingID(serviceRef)
		req := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+recordingID+"/stream-info", nil)
		rr := httptest.NewRecorder()

		start := time.Now()
		s.GetRecordingPlaybackInfo(rr, req, recordingID)
		duration := time.Since(start)

		require.Less(t, duration, 200*time.Millisecond, "GetRecordingPlaybackInfo too slow")
		require.Equal(t, http.StatusServiceUnavailable, rr.Code)

		var prob map[string]interface{}
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &prob))
		require.Equal(t, "PREPARING", prob["code"])
	})

	t.Run("GetRecordingHLSPlaylist_Under_200ms_StallingProber", func(t *testing.T) {
		serviceRef := "1:0:0:0:0:0:0:0:0:/media/nfs/test_hls.ts"
		recordingID := EncodeRecordingID(serviceRef)
		req := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+recordingID+"/playlist.m3u8", nil)
		rr := httptest.NewRecorder()

		start := time.Now()
		s.GetRecordingHLSPlaylist(rr, req, recordingID)
		duration := time.Since(start)

		require.Less(t, duration, 200*time.Millisecond, "GetRecordingHLSPlaylist too slow")
		require.Equal(t, http.StatusServiceUnavailable, rr.Code)
	})

	// Removed Extra Brace

	t.Run("StreamRecordingDirect_Under_200ms_StallingProber", func(t *testing.T) {
		serviceRef := "1:0:0:0:0:0:0:0:0:/media/nfs/test_direct.ts"
		recordingID := EncodeRecordingID(serviceRef)
		req := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+recordingID+"/stream.mp4", nil)
		rr := httptest.NewRecorder()

		start := time.Now()
		s.StreamRecordingDirect(rr, req, recordingID)
		duration := time.Since(start)

		require.Less(t, duration, 200*time.Millisecond, "StreamRecordingDirect too slow")
		require.Equal(t, http.StatusServiceUnavailable, rr.Code)
	})
}

func TestReadyOpenFailureReconcile(t *testing.T) {
	s, _ := newV3TestServer(t, t.TempDir())

	// Inject VOD Manager
	prober := &blockingProber{} // Just needs to exist
	mgr := vod.NewManager(nil, prober, nil)
	s.vodManager = mgr

	serviceRef := "1:0:0:0:0:0:0:0:0:/media/nfs/missing.ts"
	recordingID := EncodeRecordingID(serviceRef)

	// Manually inject READY state but pointing to a non-existent file
	mgr.UpdateMetadata(serviceRef, vod.Metadata{
		State:        vod.ArtifactStateReady,
		ArtifactPath: "/tmp/non-existent-artifact.mp4",
		ResolvedPath: serviceRef, // Needed for probe input
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+recordingID+"/stream.mp4", nil)
	rr := httptest.NewRecorder()

	// Handle request
	s.StreamRecordingDirect(rr, req, recordingID)

	// Assert 503 Service Unavailable
	require.Equal(t, http.StatusServiceUnavailable, rr.Code, "Should return 503 on open failure")

	// Assert State Demotion
	meta, exists := mgr.GetMetadata(serviceRef)
	require.True(t, exists)
	require.Equal(t, vod.ArtifactStatePreparing, meta.State, "State should be demoted to PREPARING synchronously")
	require.Contains(t, meta.Error, "reconcile: open failed", "Error message should reflect reconciliation")
}

func TestTriggerProbe_QueueFull(t *testing.T) {
	// Setup with small buffer
	mgr := vod.NewManager(nil, nil, nil) // Prober nil is fine, we care about queue

	// Fill queue via TriggerProbe with unique IDs
	for i := 0; i < vod.ProbeQueueSize; i++ {
		mgr.TriggerProbe(fmt.Sprintf("filler-%d", i), "")
	}

	// Now try one more
	targetID := "overflow-item"
	mgr.TriggerProbe(targetID, "")

	// Assert state did NOT stick in PREPARING
	meta, exists := mgr.GetMetadata(targetID)
	// If queue full, we expect it to revert to UNKNOWN (or not exist if it was new)
	if exists {
		// If it exists, it should NOT be PREPARING if we successfully reverted.
		// However, our logic says if it was new, we might have initialized it.
		// If revert logic works, it should be UNKNOWN (passed as previousState default).
		require.NotEqual(t, vod.ArtifactStatePreparing, meta.State, "State should not be PREPARING if queue full")
	}
}

func TestSafeMetadataMutation(t *testing.T) {
	mgr := vod.NewManager(nil, nil, nil)
	id := "test-mutation"

	// Initial State with critical fields
	mgr.UpdateMetadata(id, vod.Metadata{
		State:        vod.ArtifactStateReady,
		ArtifactPath: "/keep/this/path",
		Duration:     123,
	})

	// Call MarkUnknown
	mgr.MarkUnknown(id)

	// Verify preservation
	meta, _ := mgr.GetMetadata(id)
	require.Equal(t, vod.ArtifactStateUnknown, meta.State)
	require.Equal(t, "/keep/this/path", meta.ArtifactPath, "ArtifactPath must be preserved")
	require.Equal(t, int64(123), meta.Duration, "Duration must be preserved")
}

// TestTriggerProbe_RapidFireRevert has been moved to internal/control/vod/manager_race_test.go
// to allow for deterministic, white-box testing of the race guard logic.

func TestHotPath_StampedePrevention(t *testing.T) {
	s, _ := newV3TestServer(t, t.TempDir())

	// Create a real temporary file to satisfy os.Stat in background worker
	tmpFile, err := os.CreateTemp("", "stampede-*.ts")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	tmpPath := tmpFile.Name()

	// Inject a prober that counts calls
	var count int
	var mu sync.Mutex

	// Mock PathMapper that always succeeds
	pm := &mockPathMapper{}

	mgr := vod.NewManager(&noopRunner{}, nil, pm)
	s.vodManager = mgr
	res := resolver.New(&s.cfg, mgr)
	res.Probe = func(ctx context.Context, sourceURL string) error {
		mu.Lock()
		count++
		mu.Unlock()
		select {
		case <-time.After(250 * time.Millisecond):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.resolver = res
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.vodManager.StartProberPool(ctx)

	serviceRef := "1:0:0:0:0:0:0:0:0:" + tmpPath
	recordingID := EncodeRecordingID(serviceRef)

	// Launch multiple concurrent requests
	const concurrency = 20
	var wg sync.WaitGroup
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+recordingID+"/stream-info", nil)
			rr := httptest.NewRecorder()
			s.GetRecordingPlaybackInfo(rr, req, recordingID)
			// All should get 503 initially as it's UNKNOWN or PREPARING
			if rr.Code != http.StatusServiceUnavailable {
				t.Errorf("Expected 503, got %d", rr.Code)
			}
		}()
	}

	wg.Wait()

	// Wait for background worker to catch up
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, count, "Multiple probes triggered for the same ID (Stampede!)")
}

type countProber struct {
	probeFn func(ctx context.Context, path string) (*vod.StreamInfo, error)
}

func (p *countProber) Probe(ctx context.Context, path string) (*vod.StreamInfo, error) {
	return p.probeFn(ctx, path)
}

type mockPathMapper struct{}

func (m *mockPathMapper) ResolveLocalExisting(receiverPath string) (string, bool) {
	return receiverPath, true
}

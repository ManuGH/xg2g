package api

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordingConcurrencyLimit(t *testing.T) {
	// Setup
	cfg := config.AppConfig{
		VODMaxConcurrent: 1, // Limit to 1
		HLSRoot:          t.TempDir(),
	}
	s := &Server{
		cfg:          cfg,
		recordingRun: make(map[string]*recordingBuildState),
		vodBuildSem:  make(chan struct{}, 1),
	}

	// Case 1: Acquire Semaphore (Simulate external build)
	select {
	case s.vodBuildSem <- struct{}{}:
		t.Log("Case 1: Semaphore acquired manually")
	default:
		t.Fatal("failed to acquire semaphore")
	}

	// Case 2: Attempt new build (Should Fail with ErrTooManyBuilds)
	// We call scheduleRecordingBuild, which checks semaphore BEFORE spawning.

	cacheDir := filepath.Join(cfg.HLSRoot, "rec1")
	t.Logf("Case 2: Scheduling build for %s", cacheDir)
	err := s.scheduleRecordingBuild(cacheDir, "ref1:test", "http", "http://test")

	require.Error(t, err)
	require.True(t, errors.Is(err, errTooManyBuilds) || errors.Is(err, ErrConcurrentBuildsExceeded), "expected limit error, got %v", err)

	s.recordingMu.Lock()
	_, exists := s.recordingRun[cacheDir]
	s.recordingMu.Unlock()
	require.False(t, exists, "recordingRun should be empty after rejection")

	// Case 3: Release Semaphore and use Blocking Mock logic for deterministic test
	t.Log("Case 3: Releasing semaphore")
	<-s.vodBuildSem

	// Inject blocking mock
	block := make(chan struct{})
	s.preflightCheck = func(ctx context.Context, src string) error {
		<-block                     // Block until test allows
		return ErrSourceUnavailable // Fail fast to exit goroutine without ffmpeg
	}

	// Schedule the build (should start async)
	t.Log("Case 3: Scheduling build again")
	err = s.scheduleRecordingBuild(cacheDir, "ref1:test", "http", "http://test")

	// Expectation: Async start
	require.Error(t, err)
	require.True(t, errors.Is(err, errRecordingNotReady), "expected recording started (not ready), got %v", err)

	// Verify semaphore is HELD while blocked
	select {
	case s.vodBuildSem <- struct{}{}:
		t.Fatal("semaphore should be held by blocked build")
	default:
		// success
	}

	// Unblock
	close(block)

	// Verify semaphore is RELEASED after goroutine exits
	released := false
	for i := 0; i < 20; i++ {
		select {
		case s.vodBuildSem <- struct{}{}:
			released = true
			goto Done
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
Done:
	require.True(t, released, "semaphore should be released after build exits")
}

func TestRecordingCacheEviction(t *testing.T) {
	tmp := t.TempDir()
	recordingsDir := filepath.Join(tmp, "recordings")
	err := os.MkdirAll(recordingsDir, 0755)
	require.NoError(t, err)

	cfg := config.AppConfig{
		HLSRoot:     tmp,
		VODCacheTTL: 100 * time.Millisecond,
	}
	s := &Server{
		cfg:          cfg,
		recordingRun: make(map[string]*recordingBuildState),
	}

	// Create "old" cache
	oldDir := filepath.Join(recordingsDir, "old_cache")
	err = os.Mkdir(oldDir, 0755)
	require.NoError(t, err)

	// Set ModTime to past
	oldTime := time.Now().Add(-1 * time.Hour)
	err = os.Chtimes(oldDir, oldTime, oldTime)
	require.NoError(t, err)

	// Create "new" cache
	newDir := filepath.Join(recordingsDir, "new_cache")
	err = os.Mkdir(newDir, 0755)
	require.NoError(t, err) // ModTime is Now()

	s.evictRecordingCaches(cfg.VODCacheTTL)

	// Check results
	_, err = os.Stat(oldDir)
	assert.True(t, os.IsNotExist(err), "old cache should be evicted")

	_, err = os.Stat(newDir)
	assert.NoError(t, err, "new cache should exist")
}

func TestStaleCancellation(t *testing.T) {
	// Setup
	cfg := config.AppConfig{
		VODMaxConcurrent: 1,
		HLSRoot:          t.TempDir(),
	}
	s := &Server{
		cfg:          cfg,
		recordingRun: make(map[string]*recordingBuildState),
		vodBuildSem:  make(chan struct{}, 1),
	}

	// 1. Start a "stuck" build
	// We manually inject state to simulate a build that holds the semaphore but isn't progressing
	cacheDir := "/tmp/stale"

	// Acquire sem
	s.vodBuildSem <- struct{}{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	state := &recordingBuildState{
		status:         recordingBuildRunning,
		updatedAt:      time.Now().Add(-5 * time.Hour), // Old update
		lastProgressAt: time.Now().Add(-5 * time.Hour), // Old progress (Stale)
		cancel:         cancel,
	}
	s.recordingRun[cacheDir] = state

	// Launch a goroutine that waits for cancel, then releases sem
	semReleased := make(chan bool)
	go func() {
		<-ctx.Done()    // Wait for cancel
		<-s.vodBuildSem // Release sem
		semReleased <- true
	}()

	// 2. Run Cleanup
	// Should detect stale -> call cancel -> goroutine wakes -> releases sem
	s.cleanupRecordingBuilds(time.Now())

	// 3. Verify
	select {
	case <-semReleased:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("stale build validation failed: semaphore not released")
	}

	// Check state is marked failed
	s.recordingMu.Lock()
	st := s.recordingRun[cacheDir]
	s.recordingMu.Unlock()

	require.NotNil(t, st)
	assert.Equal(t, recordingBuildFailed, st.status)
	assert.Contains(t, st.error, "stale")
}

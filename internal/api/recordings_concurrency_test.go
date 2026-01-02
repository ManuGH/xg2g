package api

import (
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

	// Mock "recordingBuildTimeout" to be short for test if needed, references global
	// We can't easily mock the goroutine duration without dependency injection,
	// but we can test the semaphore acquisition logic by manually acquiring first.

	// Case 1: Acquire Semaphore (Simulate external build)
	select {
	case s.vodBuildSem <- struct{}{}:
		t.Log("Case 1: Semaphore acquired manually")
	default:
		t.Fatal("failed to acquire semaphore")
	}

	// Case 2: Attempt new build (Should Fail with ErrTooManyBuilds)
	// We need to call scheduleRecordingBuild.
	// Note: scheduleRecordingBuild spawns a goroutine that releases the semaphore on exit.
	// But it fails BEFORE spawning if semaphore is full.

	cacheDir := filepath.Join(cfg.HLSRoot, "rec1")
	t.Logf("Case 2: Scheduling build for %s", cacheDir)
	err := s.scheduleRecordingBuild(cacheDir, "ref1:test", "http", "http://test")

	t.Logf("Case 2 Result: %v", err)
	require.Error(t, err)
	require.True(t, errors.Is(err, errTooManyBuilds) || errors.Is(err, ErrConcurrentBuildsExceeded), "expected limit error, got %v", err)

	s.recordingMu.Lock()
	_, exists := s.recordingRun[cacheDir]
	s.recordingMu.Unlock()
	require.False(t, exists, "recordingRun should be empty after rejection")

	// Case 3: Release Semaphore and Retry
	t.Log("Case 3: Releasing semaphore")
	<-s.vodBuildSem

	// Now it should succeed
	t.Log("Case 3: Scheduling build again")
	err = s.scheduleRecordingBuild(cacheDir, "ref1:test", "http", "http://test")
	require.NoError(t, err)

	// Wait for goroutine to start/finish?
	// scheduleRecordingBuild starts a goroutine that eventually exits (and releases sem).
	// Since we didn't mock resolve/ffmpeg, it might fail fast or hang?
	// Actually scheduleRecordingBuild calls runRecordingBuild which calls checkSourceAvailability.
	// We didn't mock those. But `runRecordingBuild` runs async.
	// The function `scheduleRecordingBuild` returns nil immediately if async started.
	// So sem is held.

	// Verify sem is held
	select {
	case s.vodBuildSem <- struct{}{}:
		t.Fatal("semaphore should be held by running build")
	default:
		// success, it's full
	}
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

	// Run Evicter (Manually call internal function if possible, or wait for wrapper)
	// We exposed StartRecordingCacheEvicter but that runs loop.
	// We made `evictRecordingCaches` unexported.
	// Ideally we'd test `evictRecordingCaches` directly.
	// Since I cannot change visibility easily without another edit, I will call the loop
	// with a very short ticker or context cancel?
	// Or I can just trust `StartRecordingCacheEvicter` loop if I wait > TTL.

	// Let's rely on `evictRecordingCaches` being unexported but I can access it if test is in same package `api`.
	// Yes, `package api`.

	s.evictRecordingCaches(cfg.VODCacheTTL)

	// Check results
	_, err = os.Stat(oldDir)
	assert.True(t, os.IsNotExist(err), "old cache should be evicted")

	_, err = os.Stat(newDir)
	assert.NoError(t, err, "new cache should exist")
}

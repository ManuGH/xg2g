package vod

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEvictRecordingCache_TTL(t *testing.T) {
	root := t.TempDir()
	cacheRoot := filepath.Join(root, "recordings")
	require.NoError(t, os.MkdirAll(cacheRoot, 0750))

	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	clock := NewMockClock(now)

	oldDir := createCacheDir(t, cacheRoot, "old", now.Add(-3*time.Hour))
	newDir := createCacheDir(t, cacheRoot, "new", now.Add(-30*time.Minute))

	res, err := EvictRecordingCache(root, 2*time.Hour, 10, clock)
	require.NoError(t, err)
	require.Equal(t, 1, res.EvictedTTL)
	require.Equal(t, 0, res.EvictedMaxEntries)
	require.Equal(t, 1, res.Entries)

	_, err = os.Stat(oldDir)
	require.Error(t, err)
	require.True(t, os.IsNotExist(err))

	_, err = os.Stat(newDir)
	require.NoError(t, err)
}

func TestEvictRecordingCache_MaxEntries(t *testing.T) {
	root := t.TempDir()
	cacheRoot := filepath.Join(root, "recordings")
	require.NoError(t, os.MkdirAll(cacheRoot, 0750))

	now := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	clock := NewMockClock(now)

	oldest := createCacheDir(t, cacheRoot, "a", now.Add(-4*time.Hour))
	_ = createCacheDir(t, cacheRoot, "b", now.Add(-3*time.Hour))
	_ = createCacheDir(t, cacheRoot, "c", now.Add(-2*time.Hour))

	res, err := EvictRecordingCache(root, 0, 2, clock)
	require.NoError(t, err)
	require.Equal(t, 0, res.EvictedTTL)
	require.Equal(t, 1, res.EvictedMaxEntries)
	require.Equal(t, 2, res.Entries)

	_, err = os.Stat(oldest)
	require.Error(t, err)
	require.True(t, os.IsNotExist(err))
}

func TestEvictRecordingCache_LongSequence(t *testing.T) {
	root := t.TempDir()
	cacheRoot := filepath.Join(root, "recordings")
	require.NoError(t, os.MkdirAll(cacheRoot, 0750))

	now := time.Date(2026, 6, 7, 8, 9, 10, 0, time.UTC)
	clock := NewMockClock(now)
	maxEntries := 50
	ttl := 2 * time.Hour

	for i := 0; i < 200; i++ {
		_ = createCacheDir(t, cacheRoot, fmt.Sprintf("seq-%03d", i), clock.Now())
		if i%5 == 0 {
			clock.Advance(10 * time.Minute)
		}
		if i%10 == 0 {
			_, err := EvictRecordingCache(root, ttl, maxEntries, clock)
			require.NoError(t, err)
			require.LessOrEqual(t, countCacheDirs(t, cacheRoot), maxEntries)
		}
	}

	clock.Advance(3 * time.Hour)
	res, err := EvictRecordingCache(root, ttl, maxEntries, clock)
	require.NoError(t, err)
	require.Greater(t, res.EvictedTTL, 0)
	require.LessOrEqual(t, countCacheDirs(t, cacheRoot), maxEntries)
}

func TestEvictRecordingCache_Concurrent(t *testing.T) {
	root := t.TempDir()
	cacheRoot := filepath.Join(root, "recordings")
	require.NoError(t, os.MkdirAll(cacheRoot, 0750))

	now := time.Date(2026, 7, 8, 9, 10, 11, 0, time.UTC)
	clock := NewMockClock(now)
	maxEntries := 20
	ttl := 1 * time.Hour

	for i := 0; i < 120; i++ {
		createCacheDir(t, cacheRoot, fmt.Sprintf("seed-%03d", i), now.Add(-time.Duration(i)*time.Minute))
	}

	start := make(chan struct{})
	errs := make(chan error, 1)
	var wg sync.WaitGroup

	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < 30; i++ {
				if _, err := EvictRecordingCache(root, ttl, maxEntries, clock); err != nil {
					select {
					case errs <- err:
					default:
					}
					return
				}
			}
		}()
	}

	close(start)
	wg.Wait()

	select {
	case err := <-errs:
		require.NoError(t, err)
	default:
	}

	_, err := EvictRecordingCache(root, ttl, maxEntries, clock)
	require.NoError(t, err)
	require.LessOrEqual(t, countCacheDirs(t, cacheRoot), maxEntries)
}

func TestEvictRecordingCache_ConcurrentCreateAndEvict(t *testing.T) {
	root := t.TempDir()
	cacheRoot := filepath.Join(root, "recordings")
	require.NoError(t, os.MkdirAll(cacheRoot, 0750))

	now := time.Date(2026, 9, 10, 11, 12, 13, 0, time.UTC)
	maxEntries := 25

	for i := 0; i < 80; i++ {
		createCacheDir(t, cacheRoot, fmt.Sprintf("seed-%03d", i), now.Add(-time.Duration(i)*time.Minute))
	}

	start := make(chan struct{})
	errs := make(chan error, 1)

	var creatorsWG sync.WaitGroup
	for g := 0; g < 4; g++ {
		creatorID := g
		creatorsWG.Add(1)
		go func() {
			defer creatorsWG.Done()
			<-start
			for i := 0; i < 120; i++ {
				name := fmt.Sprintf("live-%d-%03d", creatorID, i)
				dir := filepath.Join(cacheRoot, name)

				if err := os.MkdirAll(dir, 0750); err != nil {
					select {
					case errs <- fmt.Errorf("creator mkdir %s: %w", name, err):
					default:
					}
					return
				}
				if err := os.WriteFile(filepath.Join(dir, "index.m3u8"), []byte("#EXTM3U"), 0600); err != nil && !os.IsNotExist(err) {
					select {
					case errs <- fmt.Errorf("creator write %s: %w", name, err):
					default:
					}
					return
				}
				if i%20 == 0 {
					time.Sleep(1 * time.Millisecond)
				}
			}
		}()
	}

	var evictWG sync.WaitGroup
	for g := 0; g < 6; g++ {
		evictWG.Add(1)
		go func() {
			defer evictWG.Done()
			<-start
			for i := 0; i < 40; i++ {
				if _, err := EvictRecordingCache(root, 0, maxEntries, RealClock{}); err != nil {
					select {
					case errs <- fmt.Errorf("evict run %d: %w", i, err):
					default:
					}
					return
				}
			}
		}()
	}

	close(start)
	evictWG.Wait()
	creatorsWG.Wait()

	select {
	case err := <-errs:
		require.NoError(t, err)
	default:
	}

	_, err := EvictRecordingCache(root, 0, maxEntries, RealClock{})
	require.NoError(t, err)
	require.LessOrEqual(t, countCacheDirs(t, cacheRoot), maxEntries)
}

func TestEvictRecordingCache_ExcludesActiveBuildDirs(t *testing.T) {
	root := t.TempDir()
	cacheRoot := filepath.Join(root, "recordings")
	require.NoError(t, os.MkdirAll(cacheRoot, 0750))

	now := time.Date(2026, 8, 9, 10, 11, 12, 0, time.UTC)
	clock := NewMockClock(now)

	activeDir := createCacheDir(t, cacheRoot, "active", now.Add(-4*time.Hour))
	staleDir := createCacheDir(t, cacheRoot, "stale", now.Add(-3*time.Hour))
	freshDir := createCacheDir(t, cacheRoot, "fresh", now.Add(-10*time.Minute))

	res, err := EvictRecordingCacheWithExclusions(
		root,
		2*time.Hour,
		1,
		clock,
		map[string]struct{}{activeDir: {}},
	)
	require.NoError(t, err)
	require.Equal(t, 1, res.EvictedTTL)
	require.Equal(t, 0, res.EvictedMaxEntries)
	require.Equal(t, 1, res.Entries)

	_, err = os.Stat(activeDir)
	require.NoError(t, err)
	_, err = os.Stat(staleDir)
	require.True(t, os.IsNotExist(err))
	_, err = os.Stat(freshDir)
	require.NoError(t, err)
}

func createCacheDir(t *testing.T, cacheRoot, name string, modTime time.Time) string {
	t.Helper()
	dir := filepath.Join(cacheRoot, name)
	require.NoError(t, os.MkdirAll(dir, 0750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.m3u8"), []byte("#EXTM3U"), 0600))
	require.NoError(t, os.Chtimes(dir, modTime, modTime))
	return dir
}

func countCacheDirs(t *testing.T, cacheRoot string) int {
	t.Helper()
	entries, err := os.ReadDir(cacheRoot)
	require.NoError(t, err)
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			count++
		}
	}
	return count
}

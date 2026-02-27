package vod

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// CacheEvictionResult captures a single eviction pass outcome.
type CacheEvictionResult struct {
	Entries           int
	EvictedTTL        int
	EvictedMaxEntries int
	Errors            int
}

const (
	CacheEvictReasonTTL        = "ttl"
	CacheEvictReasonMaxEntries = "max_entries"
)

type cacheEntry struct {
	name    string
	path    string
	modTime time.Time
}

var recordingCacheEvictionLocks sync.Map // map[string]*sync.Mutex

// EvictRecordingCache removes stale recording cache directories under the HLS root.
// Policy: TTL eviction first, then oldest-first to enforce maxEntries.
// Oldest is defined by directory modTime (filesystem metadata), not access time.
func EvictRecordingCache(hlsRoot string, ttl time.Duration, maxEntries int, clock Clock) (CacheEvictionResult, error) {
	return evictRecordingCache(hlsRoot, ttl, maxEntries, clock, nil)
}

// EvictRecordingCacheWithExclusions removes stale cache directories while skipping excluded paths.
// Exclusions are absolute cache directory paths (e.g. active build workdirs).
func EvictRecordingCacheWithExclusions(
	hlsRoot string,
	ttl time.Duration,
	maxEntries int,
	clock Clock,
	excludedPaths map[string]struct{},
) (CacheEvictionResult, error) {
	return evictRecordingCache(hlsRoot, ttl, maxEntries, clock, excludedPaths)
}

func evictRecordingCache(
	hlsRoot string,
	ttl time.Duration,
	maxEntries int,
	clock Clock,
	excludedPaths map[string]struct{},
) (CacheEvictionResult, error) {
	var res CacheEvictionResult

	if strings.TrimSpace(hlsRoot) == "" {
		return res, nil
	}
	if maxEntries <= 0 {
		return res, fmt.Errorf("recording cache eviction disabled: maxEntries must be > 0")
	}
	if clock == nil {
		clock = RealClock{}
	}

	cacheRoot := filepath.Join(hlsRoot, "recordings")
	evictionLock(cacheRoot).Lock()
	defer evictionLock(cacheRoot).Unlock()

	entries, err := os.ReadDir(cacheRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return res, nil
		}
		return res, err
	}

	dirs := make([]cacheEntry, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirPath := filepath.Join(cacheRoot, entry.Name())
		if _, excluded := excludedPaths[dirPath]; excluded {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			if os.IsNotExist(infoErr) {
				continue
			}
			res.Errors++
			continue
		}
		dirs = append(dirs, cacheEntry{
			name:    entry.Name(),
			path:    dirPath,
			modTime: info.ModTime(),
		})
	}

	if len(dirs) == 0 {
		return res, nil
	}

	sort.Slice(dirs, func(i, j int) bool {
		if dirs[i].modTime.Equal(dirs[j].modTime) {
			return dirs[i].name < dirs[j].name
		}
		return dirs[i].modTime.Before(dirs[j].modTime)
	})

	toDelete := make(map[string]struct{})
	reasons := make(map[string]string)
	if ttl > 0 {
		cutoff := clock.Now().Add(-ttl)
		for _, dir := range dirs {
			if dir.modTime.Before(cutoff) {
				toDelete[dir.path] = struct{}{}
				reasons[dir.path] = CacheEvictReasonTTL
			}
		}
	}

	remaining := make([]cacheEntry, 0, len(dirs))
	for _, dir := range dirs {
		if _, marked := toDelete[dir.path]; !marked {
			remaining = append(remaining, dir)
		}
	}

	if len(remaining) > maxEntries {
		overflow := len(remaining) - maxEntries
		for i := 0; i < overflow; i++ {
			toDelete[remaining[i].path] = struct{}{}
			reasons[remaining[i].path] = CacheEvictReasonMaxEntries
		}
	}

	for _, dir := range dirs {
		if _, marked := toDelete[dir.path]; !marked {
			continue
		}
		if err := os.RemoveAll(dir.path); err != nil {
			res.Errors++
			continue
		}
		switch reasons[dir.path] {
		case CacheEvictReasonTTL:
			res.EvictedTTL++
		case CacheEvictReasonMaxEntries:
			res.EvictedMaxEntries++
		}
	}

	res.Entries = len(dirs) - res.EvictedTTL - res.EvictedMaxEntries
	if res.Entries < 0 {
		res.Entries = 0
	}
	return res, nil
}

func evictionLock(cacheRoot string) *sync.Mutex {
	key := filepath.Clean(cacheRoot)
	if key == "." {
		key = cacheRoot
	}
	if existing, ok := recordingCacheEvictionLocks.Load(key); ok {
		return existing.(*sync.Mutex)
	}
	mu := &sync.Mutex{}
	actual, _ := recordingCacheEvictionLocks.LoadOrStore(key, mu)
	return actual.(*sync.Mutex)
}

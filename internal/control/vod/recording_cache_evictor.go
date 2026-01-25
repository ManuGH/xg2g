package vod

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

// EvictRecordingCache removes stale recording cache directories under the HLS root.
// Policy: TTL eviction first, then oldest-first to enforce maxEntries.
// Oldest is defined by directory modTime (filesystem metadata), not access time.
func EvictRecordingCache(hlsRoot string, ttl time.Duration, maxEntries int, clock Clock) (CacheEvictionResult, error) {
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
		info, infoErr := entry.Info()
		modTime := time.Time{}
		if infoErr != nil {
			res.Errors++
		} else {
			modTime = info.ModTime()
		}
		dirs = append(dirs, cacheEntry{
			name:    entry.Name(),
			path:    filepath.Join(cacheRoot, entry.Name()),
			modTime: modTime,
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

package vod

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestManager_PruneMetadata_TTLAndLimit(t *testing.T) {
	mgr, err := NewManager(&mockRunner{}, &mockProber{}, nil)
	require.NoError(t, err)

	now := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	mgr.SeedMetadata("old", Metadata{UpdatedAt: now.Add(-3 * time.Hour).UnixNano()})
	mgr.SeedMetadata("mid", Metadata{UpdatedAt: now.Add(-90 * time.Minute).UnixNano()})
	mgr.SeedMetadata("new", Metadata{UpdatedAt: now.Add(-30 * time.Minute).UnixNano()})

	res := mgr.PruneMetadata(now, 2*time.Hour, 2)
	require.Equal(t, 1, res.RemovedTTL)
	require.Equal(t, 0, res.RemovedMaxEntries)
	require.Equal(t, 2, res.Remaining)

	_, ok := mgr.GetMetadata("old")
	require.False(t, ok)
}

func TestManager_PruneMetadata_MaxEntries(t *testing.T) {
	mgr, err := NewManager(&mockRunner{}, &mockProber{}, nil)
	require.NoError(t, err)

	now := time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC)
	for i := 0; i < 4; i++ {
		id := fmt.Sprintf("id-%d", i)
		mgr.SeedMetadata(id, Metadata{UpdatedAt: now.Add(time.Duration(i) * time.Minute).UnixNano()})
	}

	res := mgr.PruneMetadata(now, 0, 2)
	require.Equal(t, 0, res.RemovedTTL)
	require.Equal(t, 2, res.RemovedMaxEntries)
	require.Equal(t, 2, res.Remaining)
}

func TestManager_PruneMetadata_Concurrent(t *testing.T) {
	mgr, err := NewManager(&mockRunner{}, &mockProber{}, nil)
	require.NoError(t, err)

	now := time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)
	maxEntries := 50

	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("id-%d", i)
			mgr.SeedMetadata(id, Metadata{UpdatedAt: now.UnixNano()})
		}(i)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			_ = mgr.PruneMetadata(now, 0, maxEntries)
		}
	}()

	wg.Wait()
	final := mgr.PruneMetadata(now, 0, maxEntries)
	require.LessOrEqual(t, final.Remaining, maxEntries)
}

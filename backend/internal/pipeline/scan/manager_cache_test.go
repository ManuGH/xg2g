package scan

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManager_RunBackground_SkipsWarmCache(t *testing.T) {
	store := NewMemoryStore()
	now := time.Now().UTC()
	serviceRef := "1:0:1:ABC"
	store.Update(Capability{
		ServiceRef:  serviceRef,
		Resolution:  "1920x1080",
		Codec:       "h264",
		State:       CapabilityStateOK,
		LastScan:    now,
		LastAttempt: now,
		LastSuccess: now,
		NextRetryAt: now.Add(12 * time.Hour),
	})

	playlistPath := filepath.Join(t.TempDir(), "playlist.m3u")
	require.NoError(t, os.WriteFile(playlistPath, []byte("#EXTM3U\n#EXTINF:-1,Test\nhttp://receiver.example/"+serviceRef+"\n"), 0o600))

	manager := NewManager(store, playlistPath, nil)
	require.False(t, manager.RunBackground(), "warm cache should prevent startup/background scan")
}

func TestManager_RunBackgroundForce_StartsWithWarmCache(t *testing.T) {
	store := NewMemoryStore()
	now := time.Now().UTC()
	serviceRef := "1:0:1:ABC"
	store.Update(Capability{
		ServiceRef:  serviceRef,
		Resolution:  "1920x1080",
		Codec:       "h264",
		State:       CapabilityStateOK,
		LastScan:    now,
		LastAttempt: now,
		LastSuccess: now,
		NextRetryAt: now.Add(12 * time.Hour),
	})

	manager := NewManager(store, filepath.Join(t.TempDir(), "playlist.m3u"), nil)
	started := make(chan struct{}, 1)
	manager.scanFn = func(ctx context.Context) error {
		started <- struct{}{}
		return ctx.Err()
	}

	require.True(t, manager.RunBackgroundForce(), "manual refresh should bypass warm-cache skip")
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("expected forced background scan to start")
	}
	manager.Stop()
}

func TestManager_ShouldProbeService_UsesPersistentRetryState(t *testing.T) {
	store := NewMemoryStore()
	manager := NewManager(store, filepath.Join(t.TempDir(), "playlist.m3u"), nil)
	now := time.Now().UTC()
	serviceRef := "1:0:1:ABC"

	store.Update(Capability{
		ServiceRef:    serviceRef,
		State:         CapabilityStateFailed,
		LastAttempt:   now,
		FailureReason: "no_lock",
		NextRetryAt:   now.Add(2 * time.Hour),
	})

	assert.False(t, manager.shouldProbeService(serviceRef, now.Add(time.Hour)))
	assert.True(t, manager.shouldProbeService(serviceRef, now.Add(3*time.Hour)))
}

func TestManager_GetCapability_HidesFailedEntries(t *testing.T) {
	store := NewMemoryStore()
	now := time.Now().UTC()
	store.Update(Capability{
		ServiceRef:    "1:0:1:FAILED",
		State:         CapabilityStateFailed,
		LastAttempt:   now,
		FailureReason: "probe_failed",
		NextRetryAt:   now.Add(time.Hour),
	})

	manager := NewManager(store, filepath.Join(t.TempDir(), "playlist.m3u"), nil)
	_, found := manager.GetCapability("1:0:1:FAILED")
	assert.False(t, found)
}

func TestManager_GetCapability_NormalizesTrailingColonServiceRef(t *testing.T) {
	store := NewMemoryStore()
	now := time.Now().UTC()
	store.Update(Capability{
		ServiceRef:  "1:0:19:132F:3EF:1:C00000:0:0:0:",
		Resolution:  "1280x720",
		Codec:       "h264",
		State:       CapabilityStateOK,
		LastScan:    now,
		LastAttempt: now,
		LastSuccess: now,
		NextRetryAt: now.Add(24 * time.Hour),
	})

	manager := NewManager(store, filepath.Join(t.TempDir(), "playlist.m3u"), nil)
	got, found := manager.GetCapability("1:0:19:132F:3EF:1:C00000:0:0:0")
	assert.True(t, found)
	assert.Equal(t, "1:0:19:132F:3EF:1:C00000:0:0:0", got.ServiceRef)
	assert.Equal(t, "h264", got.Codec)
	assert.False(t, got.Interlaced)
}

package household

import (
	"testing"
	"time"
)

// L11: expired unlock entries are only reclaimed when their exact ID is queried again, so an
// expired session never revisited leaks forever. CreateUnlock (the sole growth path) must
// sweep expired entries to keep the map bounded.
func TestInMemoryUnlockStore_PurgesExpiredOnCreate(t *testing.T) {
	s := NewInMemoryUnlockStore()

	// Seed an already-expired entry that no IsUnlocked call will ever reclaim.
	s.mu.Lock()
	s.entries["stale"] = unlockEntry{expiresAt: time.Now().Add(-time.Hour)}
	s.mu.Unlock()

	if _, err := s.CreateUnlock(time.Hour); err != nil {
		t.Fatalf("create unlock: %v", err)
	}

	s.mu.RLock()
	_, staleStillThere := s.entries["stale"]
	n := len(s.entries)
	s.mu.RUnlock()

	if staleStillThere {
		t.Fatal("expired entry must be purged on CreateUnlock")
	}
	if n != 1 {
		t.Fatalf("expected only the 1 live entry after sweep, got %d", n)
	}
}

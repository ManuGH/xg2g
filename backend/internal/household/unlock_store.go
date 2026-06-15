package household

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

var ErrInvalidUnlockTTL = errors.New("invalid household unlock ttl")

type UnlockStore interface {
	CreateUnlock(ttl time.Duration) (string, error)
	IsUnlocked(sessionID string) bool
	InvalidateUnlock(sessionID string)
}

type unlockEntry struct {
	expiresAt time.Time
}

type InMemoryUnlockStore struct {
	mu      sync.RWMutex
	entries map[string]unlockEntry
}

func NewInMemoryUnlockStore() *InMemoryUnlockStore {
	return &InMemoryUnlockStore{
		entries: make(map[string]unlockEntry),
	}
}

func (s *InMemoryUnlockStore) CreateUnlock(ttl time.Duration) (string, error) {
	if ttl <= 0 {
		return "", ErrInvalidUnlockTTL
	}

	sessionID, err := newUnlockSessionID()
	if err != nil {
		return "", err
	}

	now := time.Now()
	s.mu.Lock()
	// Opportunistic sweep: IsUnlocked only purges an entry when its exact ID is queried
	// again, so an expired session that is never revisited leaks forever. Purging here (the
	// sole growth path) bounds the map to roughly the live unlock count without needing a
	// background sweeper goroutine and its lifecycle.
	s.purgeExpiredLocked(now)
	s.entries[sessionID] = unlockEntry{
		expiresAt: now.Add(ttl),
	}
	s.mu.Unlock()

	return sessionID, nil
}

// purgeExpiredLocked removes all expired entries. Caller must hold s.mu.
func (s *InMemoryUnlockStore) purgeExpiredLocked(now time.Time) {
	for id, entry := range s.entries {
		if now.After(entry.expiresAt) {
			delete(s.entries, id)
		}
	}
}

func (s *InMemoryUnlockStore) IsUnlocked(sessionID string) bool {
	if sessionID == "" {
		return false
	}

	s.mu.RLock()
	entry, ok := s.entries[sessionID]
	s.mu.RUnlock()
	if !ok {
		return false
	}

	if time.Now().After(entry.expiresAt) {
		s.InvalidateUnlock(sessionID)
		return false
	}

	return true
}

func (s *InMemoryUnlockStore) InvalidateUnlock(sessionID string) {
	if sessionID == "" {
		return
	}
	s.mu.Lock()
	delete(s.entries, sessionID)
	s.mu.Unlock()
}

func newUnlockSessionID() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

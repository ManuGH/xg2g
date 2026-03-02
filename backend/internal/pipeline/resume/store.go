package resume

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
)

const (
	_ = "resume.db"
)

// NewStore creates a resume store based on the backend.
// Per ADR-021: Only sqlite and memory backends are supported in production.
func NewStore(backend, dir string) (Store, error) {
	if backend == "" {
		backend = "sqlite" // Default: SQLite is Single Durable Truth (ADR-020, ADR-021)
	}

	switch backend {
	case "sqlite":
		if dir == "" {
			return NewMemoryStore(), nil
		}
		return NewSqliteStore(filepath.Join(dir, "resume.sqlite"))
	case "memory":
		return NewMemoryStore(), nil
	case "bolt", "badger": // ADR-021 removed
		// ADR-021: BoltDB/BadgerDB are DEPRECATED and removed.
		// See docs/ops/BACKUP_RESTORE.md for SQLite-only operations.
		return nil, fmt.Errorf("DEPRECATED: %s backend removed (ADR-021). Only SQLite is supported in production", backend)
	default:
		return nil, fmt.Errorf("unknown resume store backend: %s (supported: sqlite, memory)", backend)
	}
}

// MemoryStore implements Store using a map (thread-safe).
type MemoryStore struct {
	mu   sync.RWMutex
	data map[string]*State
}

// NewMemoryStore creates an in-memory resume store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		data: make(map[string]*State),
	}
}

func (s *MemoryStore) Put(ctx context.Context, principalID, recordingID string, state *State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := compositeKey(principalID, recordingID)
	// Copy to avoid race if caller modifies state later
	clone := *state
	s.data[key] = &clone
	return nil
}

func (s *MemoryStore) Get(ctx context.Context, principalID, recordingID string) (*State, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := compositeKey(principalID, recordingID)
	if val, ok := s.data[key]; ok {
		clone := *val
		return &clone, nil
	}
	return nil, nil
}

func (s *MemoryStore) Delete(ctx context.Context, principalID, recordingID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, compositeKey(principalID, recordingID))
	return nil
}

func (s *MemoryStore) Close() error {
	s.mu.Lock()
	s.data = nil
	s.mu.Unlock()
	return nil
}

func compositeKey(principal, recording string) string {
	return principal + "\x00" + recording
}

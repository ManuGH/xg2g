package resume

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
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

func (s *MemoryStore) Put(ctx context.Context, principalID, recordingKey string, state *State) error {
	_ = ctx
	if state == nil {
		return ErrNilState
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		// Close() nils the map; assigning to a nil map would panic.
		return ErrStoreClosed
	}
	key := compositeKey(principalID, recordingKey)
	s.data[key] = cloneState(state)
	return nil
}

func (s *MemoryStore) Get(ctx context.Context, principalID, recordingKey string) (*State, error) {
	_ = ctx

	s.mu.RLock()
	defer s.mu.RUnlock()
	key := compositeKey(principalID, recordingKey)
	if val, ok := s.data[key]; ok {
		return cloneState(val), nil
	}
	return nil, nil
}

// ListRecent returns the principal's unfinished resume entries with a
// position > 0, most recently updated first.
func (s *MemoryStore) ListRecent(ctx context.Context, principalID string, limit int) ([]RecentEntry, error) {
	_ = ctx
	limit = clampListRecentLimit(limit)
	if limit == 0 {
		return nil, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	prefix := principalID + "\x00"
	entries := make([]RecentEntry, 0, limit)
	for key, state := range s.data {
		if !strings.HasPrefix(key, prefix) || state == nil {
			continue
		}
		if state.Finished || state.PosSeconds <= 0 {
			continue
		}
		entries = append(entries, RecentEntry{
			RecordingKey: strings.TrimPrefix(key, prefix),
			State:        *cloneState(state),
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].State.UpdatedAt.After(entries[j].State.UpdatedAt)
	})
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

func (s *MemoryStore) Delete(ctx context.Context, principalID, recordingKey string) error {
	_ = ctx

	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, compositeKey(principalID, recordingKey))
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

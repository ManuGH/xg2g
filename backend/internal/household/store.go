package household

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
)

type Store interface {
	// List returns the canonical profile enumeration used by snapshots and cross-store
	// consumers: the default profile first, then remaining profiles by case-insensitive
	// display name ASC, then normalized ID ASC.
	List(ctx context.Context) ([]Profile, error)
	Get(ctx context.Context, id string) (Profile, bool, error)
	Upsert(ctx context.Context, profile Profile) error
	Delete(ctx context.Context, id string) error
	Close() error
}

func NewStore(backend, storagePath string) (Store, error) {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "", "sqlite":
		return NewSqliteStore(filepath.Join(storagePath, "household.sqlite"))
	case "memory":
		return NewMemoryStore(), nil
	case "bolt", "badger":
		return nil, fmt.Errorf("DEPRECATED: %s backend removed (ADR-021). Only SQLite is supported in production", backend)
	default:
		return nil, fmt.Errorf("unknown household store backend: %s (supported: sqlite, memory)", backend)
	}
}

type MemoryStore struct {
	mu   sync.RWMutex
	data map[string]Profile
}

func NewMemoryStore() *MemoryStore {
	defaultProfile := CreateDefaultProfile()
	return &MemoryStore{
		data: map[string]Profile{
			defaultProfile.ID: defaultProfile,
		},
	}
}

func (s *MemoryStore) List(ctx context.Context) ([]Profile, error) {
	_ = ctx

	s.mu.RLock()
	defer s.mu.RUnlock()

	profiles := make([]Profile, 0, len(s.data))
	for _, profile := range s.data {
		profiles = append(profiles, CloneProfile(profile))
	}
	sortProfiles(profiles)
	return cloneProfiles(profiles), nil
}

func (s *MemoryStore) Get(ctx context.Context, id string) (Profile, bool, error) {
	_ = ctx

	s.mu.RLock()
	defer s.mu.RUnlock()

	profile, ok := s.data[normalizeIdentifier(id)]
	if !ok {
		return Profile{}, false, nil
	}
	return CloneProfile(profile), true, nil
}

func (s *MemoryStore) Upsert(ctx context.Context, profile Profile) error {
	_ = ctx

	normalized, err := PrepareProfile(profile)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[normalized.ID] = normalized
	return nil
}

func (s *MemoryStore) Delete(ctx context.Context, id string) error {
	_ = ctx

	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, normalizeIdentifier(id))
	return nil
}

func (s *MemoryStore) Close() error {
	s.mu.Lock()
	s.data = nil
	s.mu.Unlock()
	return nil
}

package entitlements

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	SourceAdminOverride  = "admin_override"
	SourceGooglePlay     = "google_play"
	SourceAmazonAppstore = "amazon_appstore"
)

type Grant struct {
	PrincipalID string
	Scope       string
	Source      string
	GrantedAt   time.Time
	ExpiresAt   *time.Time
}

type Store interface {
	ListByPrincipal(ctx context.Context, principalID string) ([]Grant, error)
	Upsert(ctx context.Context, grant Grant) error
	Delete(ctx context.Context, principalID, scope, source string) error
	Close() error
}

func NewStore(backend, storagePath string) (Store, error) {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "", "sqlite":
		return NewSqliteStore(filepath.Join(storagePath, "entitlements.sqlite"))
	case "memory":
		return NewMemoryStore(), nil
	case "bolt", "badger":
		return nil, fmt.Errorf("DEPRECATED: %s backend removed (ADR-021). Only SQLite is supported in production", backend)
	default:
		return nil, fmt.Errorf("unknown entitlement store backend: %s (supported: sqlite, memory)", backend)
	}
}

type MemoryStore struct {
	mu   sync.RWMutex
	data map[string]Grant
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: make(map[string]Grant)}
}

func (s *MemoryStore) ListByPrincipal(ctx context.Context, principalID string) ([]Grant, error) {
	_ = ctx

	s.mu.RLock()
	defer s.mu.RUnlock()

	normalizedPrincipalID := normalizePrincipalID(principalID)
	grants := make([]Grant, 0)
	for _, grant := range s.data {
		if grant.PrincipalID != normalizedPrincipalID {
			continue
		}
		grants = append(grants, cloneGrant(grant))
	}
	sortGrants(grants)
	return grants, nil
}

func (s *MemoryStore) Upsert(ctx context.Context, grant Grant) error {
	_ = ctx

	normalized, err := normalizeGrant(grant)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[grantKey(normalized.PrincipalID, normalized.Scope, normalized.Source)] = normalized
	return nil
}

func (s *MemoryStore) Delete(ctx context.Context, principalID, scope, source string) error {
	_ = ctx

	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, grantKey(normalizePrincipalID(principalID), normalizeScope(scope), normalizeSource(source)))
	return nil
}

func (s *MemoryStore) Close() error {
	s.mu.Lock()
	s.data = nil
	s.mu.Unlock()
	return nil
}

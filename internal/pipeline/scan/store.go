package scan

import (
	"fmt"
	"path/filepath"
	"sync"
	"time"
)

type Capability struct {
	ServiceRef string    `json:"service_ref"`
	Interlaced bool      `json:"interlaced"`
	LastScan   time.Time `json:"last_scan"`
	Resolution string    `json:"resolution"`
	Codec      string    `json:"codec"`
}

// CapabilityStore defines the interface for hardware metadata persistence.
type CapabilityStore interface {
	Update(cap Capability)
	Get(serviceRef string) (Capability, bool)
	Close() error
}

// NewStore creates a CapabilityStore based on the backend.
// Per ADR-021: Only sqlite backend is supported in production.
func NewStore(backend, storagePath string) (CapabilityStore, error) {
	if backend == "" {
		backend = "sqlite" // Default: SQLite is Single Durable Truth (ADR-020, ADR-021)
	}

	switch backend {
	case "sqlite":
		return NewSqliteStore(filepath.Join(storagePath, "capabilities.sqlite"))
	case "memory":
		return NewMemoryStore(), nil
	case "json": // ADR-021 removed
		// ADR-021: JSON file backend is DEPRECATED and removed.
		// See docs/ops/BACKUP_RESTORE.md for SQLite-only operations.
		return nil, fmt.Errorf("DEPRECATED: json backend removed (ADR-021). Only SQLite is supported in production")
	default:
		return nil, fmt.Errorf("unknown capability store backend: %s (supported: sqlite, memory)", backend)
	}
}

// MemoryStore implements an in-memory CapabilityStore (ephemeral).
type MemoryStore struct {
	mu   sync.RWMutex
	data map[string]Capability
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		data: make(map[string]Capability),
	}
}

func (s *MemoryStore) Update(cap Capability) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[cap.ServiceRef] = cap
}

func (s *MemoryStore) Get(serviceRef string) (Capability, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cap, ok := s.data[serviceRef]
	return cap, ok
}

func (s *MemoryStore) Close() error {
	return nil
}

package scan

import (
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/normalize"
)

type Capability struct {
	ServiceRef    string          `json:"service_ref"`
	Interlaced    bool            `json:"interlaced"`
	LastScan      time.Time       `json:"last_scan"`
	LastAttempt   time.Time       `json:"last_attempt,omitempty"`
	LastSuccess   time.Time       `json:"last_success,omitempty"`
	State         CapabilityState `json:"state,omitempty"`
	FailureReason string          `json:"failure_reason,omitempty"`
	NextRetryAt   time.Time       `json:"next_retry_at,omitempty"`
	Resolution    string          `json:"resolution"`
	Codec         string          `json:"codec"`
}

type CapabilityState string

const (
	CapabilityStateOK      CapabilityState = "ok"
	CapabilityStatePartial CapabilityState = "partial"
	CapabilityStateFailed  CapabilityState = "failed"
)

const (
	successRetryWindow = 30 * 24 * time.Hour
	partialRetryWindow = 6 * time.Hour
	failureRetryWindow = 24 * time.Hour
)

func (c Capability) Normalized() Capability {
	c.ServiceRef = normalize.ServiceRef(c.ServiceRef)

	state := c.State
	switch state {
	case CapabilityStateOK, CapabilityStatePartial, CapabilityStateFailed:
	default:
		state = inferCapabilityState(c.Resolution, c.Codec)
	}
	c.State = state

	if c.LastAttempt.IsZero() && !c.LastScan.IsZero() {
		c.LastAttempt = c.LastScan
	}
	if c.LastSuccess.IsZero() && state != CapabilityStateFailed && !c.LastScan.IsZero() {
		c.LastSuccess = c.LastScan
	}
	if c.LastScan.IsZero() && !c.LastSuccess.IsZero() {
		c.LastScan = c.LastSuccess
	}
	if c.NextRetryAt.IsZero() {
		anchor := c.LastAttempt
		if anchor.IsZero() {
			anchor = c.LastSuccess
		}
		if anchor.IsZero() {
			anchor = c.LastScan
		}
		if !anchor.IsZero() {
			c.NextRetryAt = anchor.Add(defaultRetryDelay(state))
		}
	}

	return c
}

func (c Capability) RetryDue(now time.Time) bool {
	normalized := c.Normalized()
	if normalized.NextRetryAt.IsZero() {
		return true
	}
	return !now.Before(normalized.NextRetryAt)
}

func (c Capability) Usable() bool {
	switch c.Normalized().State {
	case CapabilityStateOK, CapabilityStatePartial:
		return true
	default:
		return false
	}
}

func inferCapabilityState(resolution, codec string) CapabilityState {
	hasResolution := resolution != ""
	hasCodec := codec != ""
	switch {
	case hasResolution && hasCodec:
		return CapabilityStateOK
	case hasResolution || hasCodec:
		return CapabilityStatePartial
	default:
		return CapabilityStateFailed
	}
}

func defaultRetryDelay(state CapabilityState) time.Duration {
	switch state {
	case CapabilityStatePartial:
		return partialRetryWindow
	case CapabilityStateFailed:
		return failureRetryWindow
	default:
		return successRetryWindow
	}
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
	cap = cap.Normalized()
	s.data[cap.ServiceRef] = cap
}

func (s *MemoryStore) Get(serviceRef string) (Capability, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cap, ok := s.data[normalize.ServiceRef(serviceRef)]
	if !ok {
		return Capability{}, false
	}
	return cap.Normalized(), true
}

func (s *MemoryStore) Close() error {
	return nil
}

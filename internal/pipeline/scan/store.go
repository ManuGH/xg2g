package scan

import (
	"fmt"
	"path/filepath"
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
	case "json": // ADR-021 removed
		// ADR-021: JSON file backend is DEPRECATED and removed.
		// See docs/ops/BACKUP_RESTORE.md for SQLite-only operations.
		return nil, fmt.Errorf("DEPRECATED: json backend removed (ADR-021). Only SQLite is supported in production")
	default:
		return nil, fmt.Errorf("unknown capability store backend: %s (supported: sqlite)", backend)
	}
}

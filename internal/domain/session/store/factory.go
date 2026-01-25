// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package store

import (
	"fmt"
)

// OpenStateStore creates a StateStore based on the backend configuration.
// Per ADR-021: Only sqlite and memory backends are supported in production.
func OpenStateStore(backend, path string) (StateStore, error) {
	if backend == "" {
		backend = "sqlite" // Default: SQLite is Single Durable Truth (ADR-020, ADR-021)
	}

	switch backend {
	case "sqlite":
		return NewSqliteStore(path)
	case "memory":
		return NewMemoryStore(), nil // Ephemeral only (testing/dev)
	case "bolt", "badger": // ADR-021 removed
		// ADR-021: BoltDB/BadgerDB are DEPRECATED and removed.
		// See docs/ops/BACKUP_RESTORE.md for SQLite-only operations.
		return nil, fmt.Errorf("DEPRECATED: %s backend removed (ADR-021). Only SQLite is supported in production", backend)
	default:
		return nil, fmt.Errorf("unknown store backend: %s (supported: sqlite, memory)", backend)
	}
}

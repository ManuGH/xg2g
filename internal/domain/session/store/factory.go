// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package store

import (
	"fmt"
)

// OpenStateStore creates a StateStore based on the backend configuration.
func OpenStateStore(backend, path string) (StateStore, error) {
	if backend == "" {
		backend = "sqlite" // Default for Phase 2.3
	}

	switch backend {
	case "memory":
		return NewMemoryStore(), nil
	case "bolt":
		return OpenBoltStore(path)
	case "badger":
		return nil, fmt.Errorf("badger backend not implemented yet")
	case "sqlite":
		return NewSqliteStore(path)
	default:
		return nil, fmt.Errorf("unknown store backend: %s", backend)
	}
}

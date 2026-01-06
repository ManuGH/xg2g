// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package store

import (
	"fmt"
)

// OpenStateStore creates a StateStore based on the backend configuration.
func OpenStateStore(backend, path string) (StateStore, error) {
	switch backend {
	case "memory":
		return NewMemoryStore(), nil
	case "bolt":
		return OpenBoltStore(path)
	case "badger":
		return nil, fmt.Errorf("badger backend not implemented yet")
	default:
		// Fallback to memory if empty (MVP) or error?
		// Better to be explicit.
		if backend == "" {
			return NewMemoryStore(), nil
		}
		return nil, fmt.Errorf("unknown store backend: %s", backend)
	}
}

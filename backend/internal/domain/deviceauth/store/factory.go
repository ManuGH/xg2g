// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package store

import "fmt"

func OpenStateStore(backend, path string) (StateStore, error) {
	if backend == "" {
		backend = "sqlite"
	}

	switch backend {
	case "sqlite":
		return NewSqliteStore(path)
	case "memory":
		return NewMemoryStateStore(), nil
	default:
		return nil, fmt.Errorf("unknown device auth store backend: %s (supported: sqlite, memory)", backend)
	}
}

// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package library

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestNewStorePragmasApplied guards that the DSN pragmas actually take effect.
// The modernc.org/sqlite driver only honors the _pragma=name(value) syntax; the
// mattn-style params previously used were silently ignored, leaving the DB on the
// defaults (rollback journal, busy_timeout 0).
func TestNewStorePragmasApplied(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "library.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer func() { _ = store.Close() }()

	var journalMode string
	if err := store.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		t.Errorf("journal_mode: got %q, want WAL (DSN pragma ignored)", journalMode)
	}

	var busyTimeout int
	if err := store.db.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatalf("query busy_timeout: %v", err)
	}
	if busyTimeout != 5000 {
		t.Errorf("busy_timeout: got %d, want 5000 (DSN pragma ignored)", busyTimeout)
	}
}

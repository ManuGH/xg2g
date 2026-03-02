package store

import (
	"strings"
	"testing"
)

// TestOpenStateStore_RejectsBoltBadger verifies ADR-021 enforcement:
// Factory MUST reject bolt/badger backends with clear error message.
func TestOpenStateStore_RejectsBoltBadger(t *testing.T) {
	tests := []struct {
		backend string
		wantErr string
	}{
		{
			backend: "bolt",
			wantErr: "DEPRECATED: bolt backend removed (ADR-021)",
		},
		{
			backend: "badger",
			wantErr: "DEPRECATED: badger backend removed (ADR-021)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.backend, func(t *testing.T) {
			store, err := OpenStateStore(tt.backend, "/tmp/test.db")
			if err == nil {
				t.Fatalf("Expected error for backend %q, got nil", tt.backend)
			}
			if store != nil {
				t.Fatalf("Expected nil store for backend %q, got %v", tt.backend, store)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Error message for backend %q:\n  got:  %q\n  want substring: %q",
					tt.backend, err.Error(), tt.wantErr)
			}
		})
	}
}

// TestOpenStateStore_DefaultsToSqlite verifies ADR-020/ADR-021:
// Empty backend string MUST default to sqlite (Single Durable Truth).
func TestOpenStateStore_DefaultsToSqlite(t *testing.T) {
	// Use in-memory sqlite for test
	store, err := OpenStateStore("", ":memory:")
	if err != nil {
		t.Fatalf("Default backend (sqlite) failed: %v", err)
	}
	if store == nil {
		t.Fatal("Expected non-nil store for default backend")
	}

	// Verify it's actually a SqliteStore
	sqliteStore, ok := store.(*SqliteStore)
	if !ok {
		t.Errorf("Expected *SqliteStore, got %T", store)
	}
	if sqliteStore != nil {
		defer sqliteStore.Close()
	}
}

// TestOpenStateStore_AllowsMemory verifies ephemeral backend is allowed.
func TestOpenStateStore_AllowsMemory(t *testing.T) {
	store, err := OpenStateStore("memory", "")
	if err != nil {
		t.Fatalf("Memory backend failed: %v", err)
	}
	if store == nil {
		t.Fatal("Expected non-nil store for memory backend")
	}

	// Verify it's actually a MemoryStore
	if _, ok := store.(*MemoryStore); !ok {
		t.Errorf("Expected *MemoryStore, got %T", store)
	}
}

// TestOpenStateStore_RejectsUnknown verifies fail-closed behavior.
func TestOpenStateStore_RejectsUnknown(t *testing.T) {
	store, err := OpenStateStore("redis", "/tmp/test.db")
	if err == nil {
		t.Fatal("Expected error for unknown backend, got nil")
	}
	if store != nil {
		t.Fatalf("Expected nil store for unknown backend, got %v", store)
	}
	wantSubstr := "unknown store backend"
	if !strings.Contains(err.Error(), wantSubstr) {
		t.Errorf("Error message:\n  got:  %q\n  want substring: %q",
			err.Error(), wantSubstr)
	}
}

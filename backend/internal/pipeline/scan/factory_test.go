package scan

import (
	"strings"
	"testing"
)

// TestNewStore_RejectsJson verifies ADR-021 enforcement:
// Factory MUST reject json backend with clear error message.
func TestNewStore_RejectsJson(t *testing.T) {
	store, err := NewStore("json", "/tmp/test-capabilities")
	if err == nil {
		t.Fatal("Expected error for json backend, got nil")
	}
	if store != nil {
		t.Fatalf("Expected nil store for json backend, got %v", store)
	}
	wantSubstr := "DEPRECATED: json backend removed (ADR-021)"
	if !strings.Contains(err.Error(), wantSubstr) {
		t.Errorf("Error message:\n  got:  %q\n  want substring: %q",
			err.Error(), wantSubstr)
	}
}

// TestNewStore_DefaultsToSqlite verifies ADR-020/ADR-021:
// Empty backend string MUST default to sqlite (Single Durable Truth).
func TestNewStore_DefaultsToSqlite(t *testing.T) {
	store, err := NewStore("", t.TempDir())
	if err != nil {
		t.Fatalf("Default backend (sqlite) failed: %v", err)
	}
	if store == nil {
		t.Fatal("Expected non-nil store for default backend")
	}

	// Verify it's a SqliteStore
	sqliteStore, ok := store.(*SqliteStore)
	if !ok {
		t.Errorf("Expected *SqliteStore, got %T", store)
	}
	if sqliteStore != nil {
		defer sqliteStore.Close()
	}
}

// TestNewStore_AllowsSqlite verifies sqlite backend is explicitly supported.
func TestNewStore_AllowsSqlite(t *testing.T) {
	store, err := NewStore("sqlite", t.TempDir())
	if err != nil {
		t.Fatalf("Sqlite backend failed: %v", err)
	}
	if store == nil {
		t.Fatal("Expected non-nil store for sqlite backend")
	}

	// Verify it's a SqliteStore
	sqliteStore, ok := store.(*SqliteStore)
	if !ok {
		t.Errorf("Expected *SqliteStore, got %T", store)
	}
	if sqliteStore != nil {
		defer sqliteStore.Close()
	}
}

// TestNewStore_RejectsUnknown verifies fail-closed behavior.
func TestNewStore_RejectsUnknown(t *testing.T) {
	store, err := NewStore("redis", "/tmp/test-capabilities")
	if err == nil {
		t.Fatal("Expected error for unknown backend, got nil")
	}
	if store != nil {
		t.Fatalf("Expected nil store for unknown backend, got %v", store)
	}
	wantSubstr := "unknown capability store backend"
	if !strings.Contains(err.Error(), wantSubstr) {
		t.Errorf("Error message:\n  got:  %q\n  want substring: %q",
			err.Error(), wantSubstr)
	}
}

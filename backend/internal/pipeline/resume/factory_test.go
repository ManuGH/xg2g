package resume

import (
	"strings"
	"testing"
)

// TestNewStore_RejectsBolt verifies ADR-021 enforcement:
// Factory MUST reject bolt backend with clear error message.
func TestNewStore_RejectsBolt(t *testing.T) {
	store, err := NewStore("bolt", "/tmp/test-resume")
	if err == nil {
		t.Fatal("Expected error for bolt backend, got nil")
	}
	if store != nil {
		t.Fatalf("Expected nil store for bolt backend, got %v", store)
	}
	wantSubstr := "DEPRECATED: bolt backend removed (ADR-021)"
	if !strings.Contains(err.Error(), wantSubstr) {
		t.Errorf("Error message:\n  got:  %q\n  want substring: %q",
			err.Error(), wantSubstr)
	}
}

// TestNewStore_RejectsBadger verifies ADR-021 enforcement:
// Factory MUST reject badger backend with clear error message.
func TestNewStore_RejectsBadger(t *testing.T) {
	store, err := NewStore("badger", "/tmp/test-resume")
	if err == nil {
		t.Fatal("Expected error for badger backend, got nil")
	}
	if store != nil {
		t.Fatalf("Expected nil store for badger backend, got %v", store)
	}
	wantSubstr := "DEPRECATED: badger backend removed (ADR-021)"
	if !strings.Contains(err.Error(), wantSubstr) {
		t.Errorf("Error message:\n  got:  %q\n  want substring: %q",
			err.Error(), wantSubstr)
	}
}

// TestNewStore_DefaultsToSqlite verifies ADR-020/ADR-021:
// Empty backend string MUST default to sqlite (Single Durable Truth).
func TestNewStore_DefaultsToSqlite(t *testing.T) {
	// Empty dir → memory
	store, err := NewStore("", "")
	if err != nil {
		t.Fatalf("Default backend (sqlite) failed: %v", err)
	}
	if store == nil {
		t.Fatal("Expected non-nil store for default backend")
	}

	// Verify it's a MemoryStore (since dir is empty)
	if _, ok := store.(*MemoryStore); !ok {
		t.Errorf("Expected *MemoryStore for empty dir, got %T", store)
	}

	// With dir → SqliteStore
	store2, err := NewStore("", t.TempDir())
	if err != nil {
		t.Fatalf("Default backend with dir failed: %v", err)
	}
	if store2 == nil {
		t.Fatal("Expected non-nil store for default backend with dir")
	}

	// Verify it's a SqliteStore
	sqliteStore, ok := store2.(*SqliteStore)
	if !ok {
		t.Errorf("Expected *SqliteStore, got %T", store2)
	}
	if sqliteStore != nil {
		defer sqliteStore.Close()
	}
}

// TestNewStore_AllowsMemory verifies ephemeral backend is allowed.
func TestNewStore_AllowsMemory(t *testing.T) {
	store, err := NewStore("memory", "")
	if err != nil {
		t.Fatalf("Memory backend failed: %v", err)
	}
	if store == nil {
		t.Fatal("Expected non-nil store for memory backend")
	}

	// Verify it's a MemoryStore
	if _, ok := store.(*MemoryStore); !ok {
		t.Errorf("Expected *MemoryStore, got %T", store)
	}
}

// TestNewStore_RejectsUnknown verifies fail-closed behavior.
func TestNewStore_RejectsUnknown(t *testing.T) {
	store, err := NewStore("redis", "/tmp/test-resume")
	if err == nil {
		t.Fatal("Expected error for unknown backend, got nil")
	}
	if store != nil {
		t.Fatalf("Expected nil store for unknown backend, got %v", store)
	}
	wantSubstr := "unknown resume store backend"
	if !strings.Contains(err.Error(), wantSubstr) {
		t.Errorf("Error message:\n  got:  %q\n  want substring: %q",
			err.Error(), wantSubstr)
	}
}

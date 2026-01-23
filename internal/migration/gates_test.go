package migration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/store"
	_ "modernc.org/sqlite"
)

func TestGate5_NoDualDurable(t *testing.T) {
	// 1. Setup
	tmpDir := t.TempDir()
	boltPath := filepath.Join(tmpDir, "state.db")
	_ = os.WriteFile(boltPath, []byte("fake-bolt"), 0600)

	// 2. Set SQLite as Truth
	t.Setenv("XG2G_STORAGE", "sqlite")
	// Migration mode NOT set

	// 3. Try to open Bolt
	_, err := store.OpenBoltStore(boltPath)
	if err == nil {
		t.Fatal("Gate 5 Failed: Bolt opened while SQLite is the configured Truth")
	}
	t.Logf("✅ Gate 5 Passed: Received expected error: %v", err)
}

func TestAdversarialGate5_FactoryBypass(t *testing.T) {
	// 1. Setup
	tmpDir := t.TempDir()

	// 2. Set SQLite as Truth
	t.Setenv("XG2G_STORAGE", "sqlite")

	// 3. Attempt to use the factory to open Bolt
	_, err := store.OpenStateStore("bolt", filepath.Join(tmpDir, "st.db"))
	if err == nil {
		t.Fatal("Adversarial Gate 5 Failed: Factory allowed opening Bolt while SQLite is Truth")
	}
	t.Logf("✅ Adversarial Gate 5 Passed: Factory blocked Bolt with error: %v", err)
}

func TestGate4_SchemaVersion(t *testing.T) {
	tmpDir := t.TempDir()
	sqlitePath := filepath.Join(tmpDir, "sessions.sqlite")

	// 1. Initialize SQLite Store
	s, err := store.NewSqliteStore(sqlitePath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// 2. Check user_version
	var v int
	err = s.DB.QueryRow("PRAGMA user_version").Scan(&v)
	if err != nil {
		t.Fatal(err)
	}

	if v != 3 {
		t.Errorf("Gate 4 Failed: Expected user_version 3, got %d", v)
	}
	t.Logf("✅ Gate 4 Passed: Schema version is %d", v)
}

func TestGate1_Idempotence(t *testing.T) {
	tmpDir := t.TempDir()
	sqlitePath := filepath.Join(tmpDir, "sessions.sqlite")

	s, _ := store.NewSqliteStore(sqlitePath)
	defer s.Close()

	// IsMigrated should be false
	done, _ := IsMigrated(s.DB, ModuleSessions)
	if done {
		t.Fatal("Gate 1 Failed: Module already marked as migrated")
	}

	// Record migration
	err := RecordMigration(s.DB, HistoryRecord{
		Module:      ModuleSessions,
		SourceType:  "bolt",
		SourcePath:  "fake",
		RecordCount: 10,
	})
	if err != nil {
		t.Fatal(err)
	}

	// IsMigrated should be true
	done, _ = IsMigrated(s.DB, ModuleSessions)
	if !done {
		t.Fatal("Gate 1 Failed: Module not marked as migrated")
	}
	t.Log("✅ Gate 1 Passed: Idempotence marker verified")
}

package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestRunStorageVerifyAllChecksExpandedStorageInventory(t *testing.T) {
	dataDir := t.TempDir()
	storeDir := filepath.Join(dataDir, "store")
	if err := os.MkdirAll(storeDir, 0o750); err != nil {
		t.Fatalf("mkdir store dir: %v", err)
	}

	for _, dbName := range []string{
		"sessions.sqlite",
		"resume.sqlite",
		"capabilities.sqlite",
		"decision_audit.sqlite",
		"capability_registry.sqlite",
		"entitlements.sqlite",
		"household.sqlite",
	} {
		createSQLiteFile(t, filepath.Join(storeDir, dbName))
	}

	writeJSONFile(t, filepath.Join(storeDir, "last_sweep.json"), `{"version":1}`)
	writeJSONFile(t, filepath.Join(dataDir, "channels.json"), `["channel-1"]`)
	writeJSONFile(t, filepath.Join(dataDir, "series_rules.json"), `[]`)
	writeJSONFile(t, filepath.Join(dataDir, "drift_state.json"), `{"version":1,"lastCheck":"2026-04-09T00:00:00Z"}`)

	t.Setenv("XG2G_DATA_DIR", dataDir)
	t.Setenv("XG2G_STORE_PATH", storeDir)
	t.Setenv("XG2G_STORE_BACKEND", "sqlite")

	if code := runStorageVerify([]string{"--all", "--mode", "quick"}); code != 0 {
		t.Fatalf("runStorageVerify(--all) = %d, want 0", code)
	}
}

func TestRunStorageVerifyAllFailsOnInvalidJSONState(t *testing.T) {
	dataDir := t.TempDir()
	writeJSONFile(t, filepath.Join(dataDir, "channels.json"), `{invalid`)

	t.Setenv("XG2G_DATA_DIR", dataDir)
	t.Setenv("XG2G_STORE_BACKEND", "memory")

	if code := runStorageVerify([]string{"--all", "--mode", "quick"}); code != 1 {
		t.Fatalf("runStorageVerify(--all) = %d, want 1 for invalid json", code)
	}
}

func TestRunStorageVerifyAllIncludesConfiguredLibraryDB(t *testing.T) {
	dataDir := t.TempDir()
	libraryDBPath := filepath.Join(dataDir, "library.db")
	createSQLiteFile(t, libraryDBPath)

	configYAML := "library:\n  enabled: true\n  db_path: " + libraryDBPath + "\n"
	if err := os.WriteFile(filepath.Join(dataDir, "config.yaml"), []byte(configYAML), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	t.Setenv("XG2G_DATA_DIR", dataDir)
	t.Setenv("XG2G_STORE_BACKEND", "memory")

	if code := runStorageVerify([]string{"--all", "--mode", "quick"}); code != 0 {
		t.Fatalf("runStorageVerify(--all) = %d, want 0 with configured library db", code)
	}
}

func createSQLiteFile(t *testing.T, path string) {
	t.Helper()

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite %s: %v", path, err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS test (id INTEGER PRIMARY KEY);`); err != nil {
		t.Fatalf("init sqlite %s: %v", path, err)
	}
}

func writeJSONFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

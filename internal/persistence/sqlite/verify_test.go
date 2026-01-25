package sqlite

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
)

// INV-SQLITE-012: VerifyIntegrity detects deterministic corruption.
func TestVerifyIntegrity_Corruption_INV_SQLITE_012(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "corruptible.sqlite")

	// 1. Create a valid database
	cfg := DefaultConfig()
	db, err := Open(dbPath, cfg)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}

	// Create some schema to ensure there are pages to corrupt
	_, err = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, data TEXT);")
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}
	for i := 0; i < 100; i++ {
		_, _ = db.Exec("INSERT INTO test (data) VALUES (REPLICATE('A', 100));")
	}
	db.Close()

	// 2. Initial verification (should pass)
	issues, err := VerifyIntegrity(dbPath, "quick")
	if err != nil {
		t.Fatalf("Initial verification failed with system error: %v", err)
	}
	if issues != nil {
		t.Fatalf("Initial verification failed: %v", issues)
	}

	// 3. Simulate corruption: Overwrite 100 bytes at offset 4096 (usually second page)
	f, err := os.OpenFile(dbPath, os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("Failed to open file for corruption: %v", err)
	}

	corruptData := make([]byte, 100)
	rand.Read(corruptData)

	_, err = f.WriteAt(corruptData, 4096)
	f.Close()
	if err != nil {
		t.Fatalf("Failed to write corrupt data: %v", err)
	}

	// 4. Verify detection (should fail)
	// We use "full" mode for deterministic detection of page-level corruption
	issues, err = VerifyIntegrity(dbPath, "full")
	if err != nil {
		t.Fatalf("Verification after corruption failed with system error: %v", err)
	}

	if issues == nil {
		t.Error("Verification PASSED but should have FAILED (INV-SQLITE-012 failure)")
	} else {
		t.Logf("Detected expected corruption issues: %v", issues)
	}
}

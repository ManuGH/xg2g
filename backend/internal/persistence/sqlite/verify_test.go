package sqlite

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
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
	payload := strings.Repeat("A", 100)
	for i := 0; i < 100; i++ {
		if _, err := db.Exec("INSERT INTO test (data) VALUES (?);", payload); err != nil {
			t.Fatalf("Failed to seed row %d: %v", i, err)
		}
	}

	var pageSize int64
	if err := db.QueryRow("PRAGMA page_size;").Scan(&pageSize); err != nil {
		t.Fatalf("Failed to query page_size: %v", err)
	}
	var pageCount int64
	if err := db.QueryRow("PRAGMA page_count;").Scan(&pageCount); err != nil {
		t.Fatalf("Failed to query page_count: %v", err)
	}
	if pageCount < 2 {
		t.Fatalf("Expected at least two pages before corruption, got %d", pageCount)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Failed to close seeded database: %v", err)
	}

	// 2. Initial verification (should pass)
	issues, err := VerifyIntegrity(dbPath, "quick")
	if err != nil {
		t.Fatalf("Initial verification failed with system error: %v", err)
	}
	if issues != nil {
		t.Fatalf("Initial verification failed: %v", issues)
	}

	// 3. Simulate corruption: overwrite bytes at the start of the second page.
	f, err := os.OpenFile(dbPath, os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("Failed to open file for corruption: %v", err)
	}

	corruptData := make([]byte, 100)
	if _, err := rand.Read(corruptData); err != nil {
		t.Fatalf("Failed to generate corruption payload: %v", err)
	}

	_, err = f.WriteAt(corruptData, pageSize)
	if closeErr := f.Close(); closeErr != nil {
		t.Fatalf("Failed to close corrupted file: %v", closeErr)
	}
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

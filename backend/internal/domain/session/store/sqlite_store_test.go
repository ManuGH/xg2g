package store

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

func TestSqliteStore_Pragmas(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_pragmas.db")
	store, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// 1. Check Journal Mode
	var mode string
	err = store.DB.QueryRow("PRAGMA journal_mode").Scan(&mode)
	if err != nil || mode != "wal" {
		t.Errorf("expected WAL mode, got %s (err: %v)", mode, err)
	}

	// 2. Check Synchronous
	var sync int
	err = store.DB.QueryRow("PRAGMA synchronous").Scan(&sync)
	if err != nil || sync != 1 { // 1 = NORMAL
		t.Errorf("expected synchronous=NORMAL (1), got %d", sync)
	}

	// 3. Check Busy Timeout
	var timeout int
	err = store.DB.QueryRow("PRAGMA busy_timeout").Scan(&timeout)
	if err != nil || timeout != 5000 {
		t.Errorf("expected busy_timeout=5000, got %d", timeout)
	}

	// 4. Check Foreign Keys
	var fk int
	err = store.DB.QueryRow("PRAGMA foreign_keys").Scan(&fk)
	if err != nil || fk != 1 {
		t.Errorf("expected foreign_keys=ON (1), got %d", fk)
	}
}

func TestSqliteStore_CrashSafeReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_crash.db")

	// Write data
	s1, _ := NewSqliteStore(dbPath)
	ctx := context.Background()
	rec := &model.SessionRecord{
		SessionID:     "sess-crash",
		State:         model.SessionNew,
		CreatedAtUnix: time.Now().Unix(),
	}
	if err := s1.PutSession(ctx, rec); err != nil {
		t.Fatal(err)
	}
	s1.Close()

	// Reopen and Verify
	s2, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	got, err := s2.GetSession(ctx, "sess-crash")
	if err != nil || got == nil || got.SessionID != "sess-crash" {
		t.Errorf("recovery failed: %v", err)
	}
}

func TestSqliteStore_Concurrency_WAL(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_concurrency.db")
	store, _ := NewSqliteStore(dbPath)
	defer store.Close()

	ctx := context.Background()

	// Start a writer that takes some time in a transaction
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		tx, _ := store.DB.Begin()
		_, _ = tx.Exec("INSERT INTO sessions (session_id, service_ref, profile_json, state, pipeline_state, reason, correlation_id, created_at_unix, updated_at_unix, expires_at_unix, lease_expires_at_unix, heartbeat_interval) VALUES (?, 'svc', '{}', 'NEW', 'INIT', 'NONE', 'id', 0, 0, 0, 0, 0)", "concurrent-1")
		time.Sleep(100 * time.Millisecond) // Simulate slow write
		_ = tx.Commit()
	}()

	// Readers should not be blocked by the writer (WAL behavior)
	time.Sleep(20 * time.Millisecond) // Ensure writer started
	start := time.Now()
	_, err := store.GetSession(ctx, "non-existent")
	duration := time.Since(start)

	if err != nil {
		t.Errorf("reader failed: %v", err)
	}
	if duration > 50*time.Millisecond {
		t.Errorf("reader was likely blocked by writer, took %v", duration)
	}

	wg.Wait()
}

func TestSqliteStore_Idempotency_monotonic(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_idem.db")
	store, _ := NewSqliteStore(dbPath)
	defer store.Close()

	ctx := context.Background()
	rec := &model.SessionRecord{
		SessionID: "s1",
		State:     model.SessionNew,
	}

	// 1. Put
	sid1, exists1, err := store.PutSessionWithIdempotency(ctx, rec, "key1", time.Hour)
	if err != nil || exists1 || sid1 != "s1" {
		t.Errorf("first put failed: %v, %v", err, exists1)
	}

	// 2. Replay same key, different session ID (should return s1)
	rec2 := &model.SessionRecord{SessionID: "s2", State: model.SessionNew}
	sid2, exists2, err := store.PutSessionWithIdempotency(ctx, rec2, "key1", time.Hour)
	if err != nil || !exists2 || sid2 != "s1" {
		t.Errorf("replay failed: %v, %v, got %s", err, exists2, sid2)
	}
}

func TestSqliteStore_Lease_Contention(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_lease.db")
	store, _ := NewSqliteStore(dbPath)
	defer store.Close()

	ctx := context.Background()

	// 1. Acquire
	_, ok, _ := store.TryAcquireLease(ctx, "res", "owner1", 100*time.Millisecond)
	if !ok {
		t.Fatal("acquire failed")
	}

	// 2. Contention (Different owner, not expired)
	_, ok, _ = store.TryAcquireLease(ctx, "res", "owner2", 100*time.Millisecond)
	if ok {
		t.Error("expected contention fail for owner2")
	}

	// 3. Renew (Same owner)
	_, ok, _ = store.RenewLease(ctx, "res", "owner1", 200*time.Millisecond)
	if !ok {
		t.Error("renew failed for owner1")
	}

	// 4. Takeover (Takeover after expiry)
	time.Sleep(250 * time.Millisecond)
	_, ok, _ = store.TryAcquireLease(ctx, "res", "owner2", 100*time.Millisecond)
	if !ok {
		t.Error("takeover failed after expiry")
	}
}

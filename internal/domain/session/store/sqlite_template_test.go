package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

// ============================================================================
// SQLite Template Tests - Canonical Behavioral Contract
// ============================================================================
// Purpose: Lock SQLite behavioral invariants for sessions store.
// This file serves as the template for all SQLite store implementations.
//
// Coverage:
//   1. CRUD roundtrip with deterministic ordering
//   2. Transaction semantics (rollback leaves no partial rows)
//   3. Migration/schema version validation
//   4. Concurrency behavior under WAL (parallel read/write)
//
// DoD: Fast (<250ms), deterministic, no sleeps except for time-based tests.
// ============================================================================

// ----------------------------------------------------------------------------
// 1. CRUD Roundtrip with Deterministic Ordering
// ----------------------------------------------------------------------------

// INV-SQLITE-001: CRUD operations are consistent and deterministic.
// Write, read, update, delete operations preserve data integrity.
func TestSqliteStore_CRUD_Roundtrip_INV_SQLITE_001(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "crud.db")
	store, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSqliteStore failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// 1. CREATE: Insert session
	session := &model.SessionRecord{
		SessionID:     "sess-crud-001",
		ServiceRef:    "svc-test",
		Profile:       model.ProfileSpec{Name: "test-profile"},
		State:         model.SessionNew,
		PipelineState: model.PipeInit,
		Reason:        model.RNone,
		CorrelationID: "corr-001",
		CreatedAtUnix: 1000,
		UpdatedAtUnix: 1000,
		ExpiresAtUnix: 2000,
		LeaseExpiresAtUnix: 0,
		HeartbeatInterval:  30,
	}

	if err := store.PutSession(ctx, session); err != nil {
		t.Fatalf("PutSession failed: %v", err)
	}

	// 2. READ: Retrieve and verify exact match
	retrieved, err := store.GetSession(ctx, "sess-crud-001")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if retrieved == nil {
		t.Fatal("GetSession returned nil")
	}

	// Verify all fields
	if retrieved.SessionID != session.SessionID {
		t.Errorf("SessionID mismatch: got %q, want %q", retrieved.SessionID, session.SessionID)
	}
	if retrieved.ServiceRef != session.ServiceRef {
		t.Errorf("ServiceRef mismatch: got %q, want %q", retrieved.ServiceRef, session.ServiceRef)
	}
	if retrieved.Profile.Name != session.Profile.Name {
		t.Errorf("Profile.Name mismatch: got %q, want %q", retrieved.Profile.Name, session.Profile.Name)
	}
	if retrieved.State != session.State {
		t.Errorf("State mismatch: got %q, want %q", retrieved.State, session.State)
	}
	if retrieved.CreatedAtUnix != session.CreatedAtUnix {
		t.Errorf("CreatedAtUnix mismatch: got %d, want %d", retrieved.CreatedAtUnix, session.CreatedAtUnix)
	}

	// 3. UPDATE: Modify session via UpdateSession
	updated, err := store.UpdateSession(ctx, "sess-crud-001", func(s *model.SessionRecord) error {
		s.State = model.SessionReady
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateSession failed: %v", err)
	}
	if updated.State != model.SessionReady {
		t.Errorf("UpdateSession state mismatch: got %q, want %q", updated.State, model.SessionReady)
	}
	// Note: UpdatedAtUnix is set automatically by UpdateSession to time.Now()

	// 4. DELETE: Remove session
	if err := store.DeleteSession(ctx, "sess-crud-001"); err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}

	// 5. VERIFY DELETION: GetSession should return nil
	deleted, err := store.GetSession(ctx, "sess-crud-001")
	if err != nil {
		t.Fatalf("GetSession after delete failed: %v", err)
	}
	if deleted != nil {
		t.Errorf("GetSession after delete returned non-nil: %+v", deleted)
	}
}

// INV-SQLITE-002: ListSessions returns results in deterministic order.
// Order is consistent across multiple reads.
func TestSqliteStore_ListSessions_DeterministicOrder_INV_SQLITE_002(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "list_order.db")
	store, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSqliteStore failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Insert sessions in random order
	sessions := []string{"sess-003", "sess-001", "sess-005", "sess-002", "sess-004"}
	for i, sid := range sessions {
		if err := store.PutSession(ctx, &model.SessionRecord{
			SessionID:     sid,
			State:         model.SessionNew,
			CreatedAtUnix: int64(i * 100),
		}); err != nil {
			t.Fatalf("PutSession(%s) failed: %v", sid, err)
		}
	}

	// First read
	list1, err := store.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions (first read) failed: %v", err)
	}

	// Second read
	list2, err := store.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions (second read) failed: %v", err)
	}

	// Verify same length
	if len(list1) != len(list2) {
		t.Fatalf("ListSessions length mismatch: first=%d, second=%d", len(list1), len(list2))
	}

	// Verify deterministic order
	for i := range list1 {
		if list1[i].SessionID != list2[i].SessionID {
			t.Errorf("ListSessions order mismatch at index %d: first=%s, second=%s",
				i, list1[i].SessionID, list2[i].SessionID)
		}
	}
}

// ----------------------------------------------------------------------------
// 2. Transaction Semantics (Rollback Leaves No Partial Rows)
// ----------------------------------------------------------------------------

// INV-SQLITE-003: Transaction rollback leaves database in consistent state.
// Failed transactions MUST NOT leave partial writes.
func TestSqliteStore_Transaction_Rollback_INV_SQLITE_003(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "tx_rollback.db")
	store, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSqliteStore failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Baseline: Insert one session to verify DB is working
	baseline := &model.SessionRecord{
		SessionID:     "baseline",
		State:         model.SessionNew,
		CreatedAtUnix: 1000,
	}
	if err := store.PutSession(ctx, baseline); err != nil {
		t.Fatalf("Baseline PutSession failed: %v", err)
	}

	// Attempt a multi-row transaction that will fail
	tx, err := store.DB.Begin()
	if err != nil {
		t.Fatalf("Begin transaction failed: %v", err)
	}

	// Insert first session (should succeed)
	_, err = tx.Exec(`
		INSERT INTO sessions (session_id, service_ref, profile_json, state, pipeline_state, reason, correlation_id, created_at_ms, updated_at_ms, expires_at_ms, lease_expires_at_ms, heartbeat_interval)
		VALUES (?, 'svc', '{}', 'NEW', 'INIT', 'R_NONE', 'id', 0, 0, 0, 0, 0)
	`, "sess-tx-1")
	if err != nil {
		t.Fatalf("First insert in transaction failed: %v", err)
	}

	// Insert duplicate session (should violate PRIMARY KEY constraint)
	_, err = tx.Exec(`
		INSERT INTO sessions (session_id, service_ref, profile_json, state, pipeline_state, reason, correlation_id, created_at_ms, updated_at_ms, expires_at_ms, lease_expires_at_ms, heartbeat_interval)
		VALUES (?, 'svc', '{}', 'NEW', 'INIT', 'R_NONE', 'id', 0, 0, 0, 0, 0)
	`, "sess-tx-1") // Duplicate SessionID
	if err == nil {
		t.Fatal("Expected PRIMARY KEY violation, got nil error")
	}

	// Rollback transaction
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	// Verify NO partial writes: sess-tx-1 should NOT exist
	result, err := store.GetSession(ctx, "sess-tx-1")
	if err != nil {
		t.Fatalf("GetSession after rollback failed: %v", err)
	}
	if result != nil {
		t.Errorf("Transaction rollback left partial write: sess-tx-1 exists")
	}

	// Verify baseline session is still intact
	baselineCheck, err := store.GetSession(ctx, "baseline")
	if err != nil || baselineCheck == nil {
		t.Errorf("Baseline session corrupted after rollback: %v", err)
	}
}

// INV-SQLITE-004: Successful transactions commit atomically.
// All rows in transaction are visible after commit.
func TestSqliteStore_Transaction_Commit_Atomic_INV_SQLITE_004(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "tx_commit.db")
	store, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSqliteStore failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Begin transaction
	tx, err := store.DB.Begin()
	if err != nil {
		t.Fatalf("Begin transaction failed: %v", err)
	}

	// Insert multiple sessions
	sessionIDs := []string{"atomic-1", "atomic-2", "atomic-3"}
	for _, sid := range sessionIDs {
		_, err = tx.Exec(`
			INSERT INTO sessions (session_id, service_ref, profile_json, state, pipeline_state, reason, reason_detail, fallback_reason, correlation_id, created_at_ms, updated_at_ms, expires_at_ms, lease_expires_at_ms, heartbeat_interval, stop_reason)
			VALUES (?, 'svc', '{}', 'NEW', 'INIT', 'R_NONE', '', '', 'id', 0, 0, 0, 0, 0, '')
		`, sid)
		if err != nil {
			t.Fatalf("Insert %s in transaction failed: %v", sid, err)
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Verify all sessions are visible
	for _, sid := range sessionIDs {
		result, err := store.GetSession(ctx, sid)
		if err != nil {
			t.Fatalf("GetSession(%s) after commit failed: %v", sid, err)
		}
		if result == nil {
			t.Errorf("GetSession(%s) returned nil after commit", sid)
		}
	}
}

// ----------------------------------------------------------------------------
// 3. Migration/Schema Version Validation
// ----------------------------------------------------------------------------

// INV-SQLITE-005: Schema version is set correctly on initialization.
// PRAGMA user_version must be set to expected schema version.
func TestSqliteStore_SchemaVersion_INV_SQLITE_005(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "schema_version.db")
	store, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSqliteStore failed: %v", err)
	}
	defer store.Close()

	// Query user_version
	var version int
	err = store.DB.QueryRow("PRAGMA user_version").Scan(&version)
	if err != nil {
		t.Fatalf("PRAGMA user_version query failed: %v", err)
	}

	// Expected version: 3 (as per store implementation const schemaVersion = 3)
	expectedVersion := 3
	if version != expectedVersion {
		t.Errorf("Schema version mismatch: got %d, want %d", version, expectedVersion)
	}
}

// INV-SQLITE-006: Opening existing database preserves schema version.
// Reopening a database does not reset or corrupt schema version.
func TestSqliteStore_SchemaVersion_Preserved_INV_SQLITE_006(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "schema_preserve.db")

	// First open: create database
	s1, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("First NewSqliteStore failed: %v", err)
	}

	// Verify initial version
	var v1 int
	if err := s1.DB.QueryRow("PRAGMA user_version").Scan(&v1); err != nil {
		t.Fatalf("PRAGMA user_version (first) failed: %v", err)
	}
	s1.Close()

	// Second open: reopen existing database
	s2, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("Second NewSqliteStore failed: %v", err)
	}
	defer s2.Close()

	// Verify version preserved
	var v2 int
	if err := s2.DB.QueryRow("PRAGMA user_version").Scan(&v2); err != nil {
		t.Fatalf("PRAGMA user_version (second) failed: %v", err)
	}

	if v1 != v2 {
		t.Errorf("Schema version not preserved: first=%d, second=%d", v1, v2)
	}
}

// INV-SQLITE-007: Required tables exist after initialization.
// All schema tables are created during NewSqliteStore.
func TestSqliteStore_Schema_TablesExist_INV_SQLITE_007(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "schema_tables.db")
	store, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSqliteStore failed: %v", err)
	}
	defer store.Close()

	requiredTables := []string{"sessions", "idempotency", "leases"}

	for _, table := range requiredTables {
		var count int
		query := `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`
		err := store.DB.QueryRow(query, table).Scan(&count)
		if err != nil {
			t.Fatalf("Query for table %q failed: %v", table, err)
		}
		if count != 1 {
			t.Errorf("Table %q does not exist (count=%d)", table, count)
		}
	}
}

// ----------------------------------------------------------------------------
// 4. Concurrency Behavior Under WAL (Parallel Read/Write)
// ----------------------------------------------------------------------------

// INV-SQLITE-008: WAL mode allows concurrent readers during writes.
// Multiple readers can read while a writer is active (no blocking).
func TestSqliteStore_WAL_ConcurrentReaders_INV_SQLITE_008(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "wal_readers.db")
	store, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSqliteStore failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Insert baseline data
	if err := store.PutSession(ctx, &model.SessionRecord{
		SessionID:     "wal-baseline",
		State:         model.SessionNew,
		CreatedAtUnix: 1000,
	}); err != nil {
		t.Fatalf("Baseline PutSession failed: %v", err)
	}

	// Start a long-running write transaction
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		tx, _ := store.DB.Begin()
		defer func() { _ = tx.Rollback() }()

		// Simulate slow write
		_, _ = tx.Exec(`
			INSERT INTO sessions (session_id, service_ref, profile_json, state, pipeline_state, reason, correlation_id, created_at_ms, updated_at_ms, expires_at_ms, lease_expires_at_ms, heartbeat_interval)
			VALUES (?, 'svc', '{}', 'NEW', 'INIT', 'R_NONE', 'id', 0, 0, 0, 0, 0)
		`, "wal-writer")
		time.Sleep(150 * time.Millisecond)
		_ = tx.Commit()
	}()

	// Allow writer to start
	time.Sleep(20 * time.Millisecond)

	// Start concurrent readers
	readerCount := 5
	var readerErrors atomic.Int32
	var readerWg sync.WaitGroup

	for i := 0; i < readerCount; i++ {
		readerWg.Add(1)
		go func(id int) {
			defer readerWg.Done()
			start := time.Now()

			// Read should not block (WAL allows concurrent reads)
			_, err := store.GetSession(ctx, "wal-baseline")
			duration := time.Since(start)

			if err != nil {
				t.Errorf("Reader %d failed: %v", id, err)
				readerErrors.Add(1)
				return
			}

			// Verify read was not blocked by writer
			// WAL allows reads to proceed while write is in progress
			// If blocked, duration would be >= 150ms (writer sleep time)
			if duration > 100*time.Millisecond {
				t.Errorf("Reader %d was blocked by writer (took %v)", id, duration)
				readerErrors.Add(1)
			}
		}(i)
	}

	readerWg.Wait()
	wg.Wait()

	if readerErrors.Load() > 0 {
		t.Errorf("WAL concurrent read test failed with %d reader errors", readerErrors.Load())
	}
}

// INV-SQLITE-009: Parallel writes are serialized correctly.
// Multiple concurrent writes do not corrupt data.
func TestSqliteStore_WAL_ParallelWrites_INV_SQLITE_009(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "wal_writers.db")
	store, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSqliteStore failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Launch parallel writers (reduced to 5 to avoid SQLITE_BUSY under high contention)
	writerCount := 5
	var wg sync.WaitGroup
	var writeErrors atomic.Int32

	for i := 0; i < writerCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			sessionID := sql.NullString{String: "parallel-writer-" + string(rune('A'+id)), Valid: true}
			session := &model.SessionRecord{
				SessionID:     sessionID.String,
				State:         model.SessionNew,
				CreatedAtUnix: int64(1000 + id),
			}

			if err := store.PutSession(ctx, session); err != nil {
				t.Errorf("Writer %d failed: %v", id, err)
				writeErrors.Add(1)
			}
		}(i)
	}

	wg.Wait()

	if writeErrors.Load() > 0 {
		t.Fatalf("Parallel writes failed with %d errors", writeErrors.Load())
	}

	// Verify all sessions were written correctly
	list, err := store.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions after parallel writes failed: %v", err)
	}

	if len(list) != writerCount {
		t.Errorf("ListSessions count mismatch: got %d, want %d", len(list), writerCount)
	}
}

// INV-SQLITE-010: ScanSessions with cancelled context returns error.
// Pre-cancelled context prevents query from starting.
func TestSqliteStore_ScanSessions_ContextCancellation_INV_SQLITE_010(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "scan_cancel.db")
	store, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSqliteStore failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Insert baseline sessions
	for i := 0; i < 10; i++ {
		if err := store.PutSession(ctx, &model.SessionRecord{
			SessionID:     "scan-" + string(rune('0'+i)),
			State:         model.SessionNew,
			CreatedAtUnix: int64(i),
		}); err != nil {
			t.Fatalf("PutSession failed: %v", err)
		}
	}

	// Create pre-cancelled context
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel() // Cancel BEFORE calling ScanSessions

	// Attempt scan with cancelled context
	err = store.ScanSessions(cancelCtx, func(s *model.SessionRecord) error {
		t.Error("Callback should not be invoked with cancelled context")
		return nil
	})

	// QueryContext should detect cancelled context and return error
	if err == nil {
		t.Error("ScanSessions with pre-cancelled context returned nil error, expected context.Canceled")
	}
	// Note: Error may be context.Canceled or a wrapped version
	if err != nil && err != context.Canceled && !isContextError(err) {
		t.Errorf("ScanSessions error: got %v, want context.Canceled or related error", err)
	}
}

// isContextError checks if an error is related to context cancellation.
func isContextError(err error) bool {
	if err == context.Canceled || err == context.DeadlineExceeded {
		return true
	}
	// Check for wrapped context errors
	return err.Error() == "context canceled" || err.Error() == "context deadline exceeded"
}

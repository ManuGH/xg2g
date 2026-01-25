package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

// INV-SQLITE-011: UpdateSession persists updates
// This test ensures that UpdateSession actually writes back to the database
// and that changes are durable across store re-openings.
func TestInvariant_UpdateSessionPersists(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "sessions.sqlite")

	// 1. Initial Setup: Create a session
	s, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	sessionID := "test-session-inv"
	rec := &model.SessionRecord{
		SessionID:  sessionID,
		ServiceRef: "ref-1",
		State:      model.SessionNew,
	}

	if err := s.PutSession(ctx, rec); err != nil {
		t.Fatalf("Failed to put session: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Failed to close store: %v", err)
	}

	// 2. Perform Update
	s, err = NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to re-open store: %v", err)
	}

	updatedRec, err := s.UpdateSession(ctx, sessionID, func(r *model.SessionRecord) error {
		r.State = model.SessionReady
		r.Reason = "invariant-test"
		return nil
	})

	if err != nil {
		t.Fatalf("UpdateSession failed: %v", err)
	}

	// Assertions on returned record
	if updatedRec.State != model.SessionReady {
		t.Errorf("Expected state model.SessionReady, got %s", updatedRec.State)
	}
	if updatedRec.Reason != "invariant-test" {
		t.Errorf("Expected reason 'invariant-test', got %q", updatedRec.Reason)
	}
	if updatedRec.UpdatedAtUnix == 0 {
		t.Error("UpdatedAtUnix was not set (zero)")
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Failed to close store: %v", err)
	}

	// 3. Verification: Proving durable persistence
	s, err = NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to re-open store for verification: %v", err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			t.Errorf("Failed to close store: %v", err)
		}
	}()

	persisted, err := s.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("Failed to get session: %v", err)
	}

	if persisted == nil {
		t.Fatal("Session not found after update")
	}

	// Durable state/reason proof (CORE of INV-SQLITE-011)
	if persisted.State != model.SessionReady {
		t.Errorf("Durable state mismatch: expected READY, got %s", persisted.State)
	}

	if persisted.Reason != "invariant-test" {
		t.Errorf("Durable reason mismatch: expected 'invariant-test', got %q", persisted.Reason)
	}

	// Consistency proof: DB truth matches returned record
	if persisted.UpdatedAtUnix != updatedRec.UpdatedAtUnix {
		t.Errorf("Durable UpdatedAt mismatch: expected %d, got %d", updatedRec.UpdatedAtUnix, persisted.UpdatedAtUnix)
	}
}

// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/vod/fsm"
	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory sqlite db: %v", err)
	}
	return db
}

func TestArtifactStore_InitAndUpsert(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	ctx := context.Background()
	store := NewArtifactStore(db)

	if err := store.InitSchema(ctx); err != nil {
		t.Fatalf("InitSchema failed: %v", err)
	}

	now := time.Now().Truncate(time.Millisecond)
	art, err := fsm.NewArtifact("art-1", "rec-100", "var-h264", "/tmp/manifest.m3u8", "/tmp/seg-*.ts", now)
	if err != nil {
		t.Fatalf("NewArtifact failed: %v", err)
	}

	// Insert
	if err := store.UpsertArtifact(ctx, art); err != nil {
		t.Fatalf("UpsertArtifact failed: %v", err)
	}

	// Retrieve
	fetched, err := store.GetArtifact(ctx, "rec-100", "var-h264")
	if err != nil {
		t.Fatalf("GetArtifact failed: %v", err)
	}

	if fetched.ID != "art-1" {
		t.Errorf("ID = %q, want art-1", fetched.ID)
	}
	if fetched.State != fsm.StatePreparing {
		t.Errorf("State = %s, want PREPARING", fetched.State)
	}

	// Update transition to READY
	_ = fsm.CompleteBuild(art, now.Add(time.Second))
	if err := store.UpsertArtifact(ctx, art); err != nil {
		t.Fatalf("UpsertArtifact update failed: %v", err)
	}

	updated, err := store.GetArtifact(ctx, "rec-100", "var-h264")
	if err != nil {
		t.Fatalf("GetArtifact failed after update: %v", err)
	}
	if updated.State != fsm.StateReady {
		t.Errorf("State = %s, want READY", updated.State)
	}
}

func TestArtifactStore_NotFound(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	ctx := context.Background()
	store := NewArtifactStore(db)
	_ = store.InitSchema(ctx)

	_, err := store.GetArtifact(ctx, "nonexistent", "var")
	if !errors.Is(err, fsm.ErrArtifactNotFound) {
		t.Errorf("expected ErrArtifactNotFound, got %v", err)
	}
}

func TestArtifactStore_ListAndDelete(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	ctx := context.Background()
	store := NewArtifactStore(db)
	_ = store.InitSchema(ctx)

	now := time.Now().Truncate(time.Millisecond)
	art1, _ := fsm.NewArtifact("art-1", "rec-200", "var-720p", "/m1.m3u8", "/s1-*.ts", now)
	art2, _ := fsm.NewArtifact("art-2", "rec-200", "var-1080p", "/m2.m3u8", "/s2-*.ts", now.Add(time.Second))

	_ = store.UpsertArtifact(ctx, art1)
	_ = store.UpsertArtifact(ctx, art2)

	list, err := store.ListByRecordingRef(ctx, "rec-200")
	if err != nil {
		t.Fatalf("ListByRecordingRef failed: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list length = %d, want 2", len(list))
	}

	// Delete art1
	if err := store.DeleteArtifact(ctx, "rec-200", "var-720p"); err != nil {
		t.Fatalf("DeleteArtifact failed: %v", err)
	}

	remaining, _ := store.ListByRecordingRef(ctx, "rec-200")
	if len(remaining) != 1 {
		t.Errorf("remaining length = %d, want 1", len(remaining))
	}
}

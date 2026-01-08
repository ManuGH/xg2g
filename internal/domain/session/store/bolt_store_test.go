// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

func TestBoltStore_OpenClose(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "bolt_test_basic")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// OpenBoltStore expects explicit path or directory.

	store, err := OpenBoltStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("failed to close store: %v", err)
	}

	// Reopen
	store2, err := OpenBoltStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to reopen store: %v", err)
	}
	_ = store2.Close()
}

func TestBoltStore_SessionRoundTrip(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "bolt_test_session")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := OpenBoltStore(tmpDir)
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	rec := &model.SessionRecord{
		SessionID: "sess-1",
		State:     model.SessionNew,
	}

	if err := store.PutSession(ctx, rec); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetSession(ctx, "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionID != "sess-1" || got.State != model.SessionNew {
		t.Errorf("mismatch: %+v", got)
	}
}

func TestBoltStore_PutSessionWithIdempotency(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "bolt_test_atomic")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := OpenBoltStore(tmpDir)
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	rec := &model.SessionRecord{
		SessionID: "sess-atomic-1",
		State:     model.SessionNew,
	}

	// First time: Success
	_, exists, err := store.PutSessionWithIdempotency(ctx, rec, "key-1", 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Error("expected exists=false for first write")
	}

	// Verify Idempotency exists
	sid, ok, err := store.GetIdempotency(ctx, "key-1")
	if err != nil || !ok || sid != "sess-atomic-1" {
		t.Errorf("expected found=true, sid=sess-atomic-1, got %v, %v", ok, sid)
	}

	// Verify Replay Detection (Atomic Check-And-Set)
	// Must not return error, but exists=true and existingID
	rec2 := &model.SessionRecord{SessionID: "sess-atomic-2"} // Different ID
	existingID, exists, err := store.PutSessionWithIdempotency(ctx, rec2, "key-1", 100*time.Millisecond)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if !exists {
		t.Error("expected exists=true for replay")
	}
	if existingID != "sess-atomic-1" {
		t.Errorf("expected existingID=sess-atomic-1, got %s", existingID)
	}

	// Verify Expiry
	time.Sleep(150 * time.Millisecond)
	_, ok, err = store.GetIdempotency(ctx, "key-1")
	if err != nil {
		t.Error(err)
	}
	if ok {
		t.Error("expected expiry, got found")
	}
}

func TestBoltStore_ScanSessions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "bolt_test_scan")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	store, _ := OpenBoltStore(tmpDir)
	defer func() { _ = store.Close() }()

	_ = store.PutSession(context.Background(), &model.SessionRecord{SessionID: "s1"})
	_ = store.PutSession(context.Background(), &model.SessionRecord{SessionID: "s2"})
	_ = store.PutSession(context.Background(), &model.SessionRecord{SessionID: "s3"})

	count := 0
	err = store.ScanSessions(context.Background(), func(s *model.SessionRecord) error {
		count++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Errorf("expected 3 sessions, got %d", count)
	}
}

func TestBoltStore_Lease(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "bolt_test_lease")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	store, _ := OpenBoltStore(tmpDir)
	defer func() { _ = store.Close() }()
	ctx := context.Background()

	// 1. Acquire
	lease, ok, err := store.TryAcquireLease(ctx, "res1", "worker1", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	if !ok {
		t.Fatalf("acquire ok=false")
	}
	if lease.Owner() != "worker1" {
		t.Error("wrong owner")
	}

	// 2. Contention (Fail)
	_, ok, err = store.TryAcquireLease(ctx, "res1", "worker2", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("acquire 2 failed: %v", err)
	}
	if ok {
		t.Error("expected contention failure")
	}

	// 3. Renew (Success)
	_, ok, err = store.RenewLease(ctx, "res1", "worker1", 200*time.Millisecond)
	if err != nil {
		t.Fatalf("renew failed: %v", err)
	}
	if !ok {
		t.Error("renew failed ok=false")
	}

	// 4. Must-Fix Check: Renew on Expired Lease must fail
	time.Sleep(250 * time.Millisecond)
	_, ok, err = store.RenewLease(ctx, "res1", "worker1", 100*time.Millisecond)
	if err != nil {
		t.Logf("renew expired error (expected): %v", err)
	}
	if ok {
		t.Error("expected renew to fail on expired lease (force recovery)")
	}

	// 5. Expiry Takeover
	_, ok, err = store.TryAcquireLease(ctx, "res1", "worker2", 100*time.Millisecond)
	// Check err
	if err != nil {
		t.Error(err)
	}
	if !ok {
		t.Error("takeover failed after expiry")
	}
}

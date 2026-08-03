// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package receiverusage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/store"
)

type failStore struct {
	store.LeaseStore
	err error
}

func (f *failStore) TryAcquireLease(ctx context.Context, key, owner string, ttl time.Duration) (store.Lease, bool, error) {
	return nil, false, f.err
}

// Test 1: First Acquire receives Slot 0.
func TestController_Acquire_FirstAcquire_Slot0(t *testing.T) {
	st := store.NewMemoryStore()
	ctrl := NewRestrictedAccessController(st)

	handle, err := ctrl.Acquire(context.Background(), "rec-1", "owner-1", 1, 10*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handle.Slot != 0 {
		t.Fatalf("expected slot 0, got %d", handle.Slot)
	}
	wantScope := "receiver:rec-1:restricted-access:0"
	if handle.Scope != wantScope {
		t.Fatalf("expected scope %q, got %q", wantScope, handle.Scope)
	}
}

// Test 2: Second Acquire receives Slot 1 when capacity is 2.
func TestController_Acquire_SecondAcquire_Slot1(t *testing.T) {
	st := store.NewMemoryStore()
	ctrl := NewRestrictedAccessController(st)

	h1, err := ctrl.Acquire(context.Background(), "rec-1", "owner-1", 2, 10*time.Second)
	if err != nil {
		t.Fatalf("unexpected error h1: %v", err)
	}
	if h1.Slot != 0 {
		t.Fatalf("expected h1 slot 0, got %d", h1.Slot)
	}

	h2, err := ctrl.Acquire(context.Background(), "rec-1", "owner-2", 2, 10*time.Second)
	if err != nil {
		t.Fatalf("unexpected error h2: %v", err)
	}
	if h2.Slot != 1 {
		t.Fatalf("expected h2 slot 1, got %d", h2.Slot)
	}
}

// Test 3: Full pool returns typed capacity conflict.
func TestController_Acquire_FullPool_TypedConflict(t *testing.T) {
	st := store.NewMemoryStore()
	ctrl := NewRestrictedAccessController(st)

	_, err := ctrl.Acquire(context.Background(), "rec-1", "owner-1", 1, 10*time.Second)
	if err != nil {
		t.Fatalf("unexpected error first acquire: %v", err)
	}

	_, err = ctrl.Acquire(context.Background(), "rec-1", "owner-2", 1, 10*time.Second)
	if err == nil {
		t.Fatalf("expected error on full pool acquire, got nil")
	}
	if !errors.Is(err, ErrRestrictedAccessLimitExceeded) {
		t.Fatalf("expected ErrRestrictedAccessLimitExceeded, got %v", err)
	}
}

// Test 4: Store error remains technical error unchanged.
func TestController_Acquire_StoreError_TechnicalError(t *testing.T) {
	techErr := errors.New("database connection failed")
	st := &failStore{err: techErr}
	ctrl := NewRestrictedAccessController(st)

	_, err := ctrl.Acquire(context.Background(), "rec-1", "owner-1", 1, 10*time.Second)
	if err == nil {
		t.Fatalf("expected technical store error, got nil")
	}
	if !errors.Is(err, techErr) {
		t.Fatalf("expected technical error %v, got %v", techErr, err)
	}
}

// Test 5: Renew extends exclusively its own lease.
func TestController_Renew_ExtendsOwnLease(t *testing.T) {
	st := store.NewMemoryStore()
	ctrl := NewRestrictedAccessController(st)

	h1, err := ctrl.Acquire(context.Background(), "rec-1", "owner-1", 1, 10*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := ctrl.Renew(context.Background(), h1, 20*time.Second); err != nil {
		t.Fatalf("expected successful renew, got %v", err)
	}

	fakeHandle := h1
	fakeHandle.Owner = "owner-fake"
	if err := ctrl.Renew(context.Background(), fakeHandle, 20*time.Second); err == nil {
		t.Fatalf("expected renew error for wrong owner, got nil")
	}
}

// Test 6: Release releases exclusively its own lease.
func TestController_Release_ReleasesOwnLease(t *testing.T) {
	st := store.NewMemoryStore()
	ctrl := NewRestrictedAccessController(st)

	h1, err := ctrl.Acquire(context.Background(), "rec-1", "owner-1", 1, 10*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := ctrl.Release(context.Background(), h1); err != nil {
		t.Fatalf("expected successful release, got %v", err)
	}

	// Pool should now be free
	h2, err := ctrl.Acquire(context.Background(), "rec-1", "owner-2", 1, 10*time.Second)
	if err != nil {
		t.Fatalf("expected successful acquire after release, got %v", err)
	}
	if h2.Slot != 0 {
		t.Fatalf("expected slot 0 after release, got %d", h2.Slot)
	}
}

// Test 7: Outdated handle cannot release a new lease acquired by another owner.
func TestController_Release_OutdatedHandle_CannotReleaseNewLease(t *testing.T) {
	st := store.NewMemoryStore()
	ctrl := NewRestrictedAccessController(st)

	hOld, err := ctrl.Acquire(context.Background(), "rec-1", "owner-old", 1, 10*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Release hOld
	if err := ctrl.Release(context.Background(), hOld); err != nil {
		t.Fatalf("unexpected error releasing hOld: %v", err)
	}

	// New owner acquires same slot
	hNew, err := ctrl.Acquire(context.Background(), "rec-1", "owner-new", 1, 10*time.Second)
	if err != nil {
		t.Fatalf("unexpected error acquiring hNew: %v", err)
	}

	// Stale hOld attempt to release must NOT release hNew
	if err := ctrl.Release(context.Background(), hOld); err != nil {
		// Idempotent release for non-matching owner may return nil or error depending on store contract
	}

	// Verify hNew is still held in store by owner-new
	l, ok, err := st.GetLease(context.Background(), hNew.Scope)
	if err != nil || !ok {
		t.Fatalf("expected hNew lease to exist in store")
	}
	if l.Owner() != "owner-new" {
		t.Fatalf("stale handle released new owner's lease! owner is %s", l.Owner())
	}
}

// Test 8: 50 parallel Acquires at capacity 1 yields exactly 1 success under -race.
func TestController_Acquire_50Parallel_Capacity1_ExactlyOneSuccess(t *testing.T) {
	st := store.NewMemoryStore()
	ctrl := NewRestrictedAccessController(st)

	workers := 50
	var wg sync.WaitGroup
	wg.Add(workers)

	successCount := 0
	conflictCount := 0
	var mu sync.Mutex

	for i := 0; i < workers; i++ {
		go func(id int) {
			defer wg.Done()
			owner := fmt.Sprintf("worker-%d", id)
			_, err := ctrl.Acquire(context.Background(), "rec-1", owner, 1, 10*time.Second)

			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				successCount++
			} else if errors.Is(err, ErrRestrictedAccessLimitExceeded) {
				conflictCount++
			}
		}(i)
	}

	wg.Wait()

	if successCount != 1 {
		t.Fatalf("expected exactly 1 success under capacity 1, got %d", successCount)
	}
	if conflictCount != workers-1 {
		t.Fatalf("expected %d capacity conflicts, got %d", workers-1, conflictCount)
	}
}

// Test 9: Two receivers with capacity 1 each yield 1 independent success each.
func TestController_Acquire_TwoReceivers_Capacity1_Independent(t *testing.T) {
	st := store.NewMemoryStore()
	ctrl := NewRestrictedAccessController(st)

	hA, errA := ctrl.Acquire(context.Background(), "rec-A", "owner-1", 1, 10*time.Second)
	if errA != nil {
		t.Fatalf("unexpected error rec-A: %v", errA)
	}

	hB, errB := ctrl.Acquire(context.Background(), "rec-B", "owner-2", 1, 10*time.Second)
	if errB != nil {
		t.Fatalf("unexpected error rec-B: %v", errB)
	}

	if hA.ReceiverID != "rec-A" || hB.ReceiverID != "rec-B" {
		t.Fatalf("receiver IDs mismatched")
	}
	if hA.Slot != 0 || hB.Slot != 0 {
		t.Fatalf("expected slot 0 for both independent receivers")
	}
}

// Test 10: Invalid receiver ID, capacity, owner, or TTL rejected before store access (ErrInvalidInput).
func TestController_Validation_InvalidInputs_Rejected(t *testing.T) {
	st := store.NewMemoryStore()
	ctrl := NewRestrictedAccessController(st)

	ctx := context.Background()

	// Invalid receiver ID
	_, err := ctrl.Acquire(ctx, "", "owner-1", 1, 10*time.Second)
	if err == nil || !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for empty receiver ID, got %v", err)
	}

	// Invalid capacity
	_, err = ctrl.Acquire(ctx, "rec-1", "owner-1", 0, 10*time.Second)
	if err == nil || !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for capacity 0, got %v", err)
	}

	// Invalid owner
	_, err = ctrl.Acquire(ctx, "rec-1", "", 1, 10*time.Second)
	if err == nil || !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for empty owner, got %v", err)
	}

	// Invalid TTL
	_, err = ctrl.Acquire(ctx, "rec-1", "owner-1", 1, 0)
	if err == nil || !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for 0 TTL, got %v", err)
	}
}

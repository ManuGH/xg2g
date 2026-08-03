// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package receiverusage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/store"
)

type failListStore struct {
	store.LeaseStore
	err error
}

func (f *failListStore) ListLeases(ctx context.Context) ([]store.Lease, error) {
	return nil, f.err
}

// Test 1: Empty store results in all slots free.
func TestInventory_EmptyStore_AllSlotsFree(t *testing.T) {
	st := store.NewMemoryStore()
	inv, err := Inventory(context.Background(), st, "rec-1", 2, fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !inv.InventoryTrusted {
		t.Fatalf("expected InventoryTrusted to be true")
	}
	if len(inv.FreeSlots) != 2 || inv.FreeSlots[0] != 0 || inv.FreeSlots[1] != 1 {
		t.Fatalf("expected free slots [0, 1], got %v", inv.FreeSlots)
	}
	if len(inv.Active) != 0 || len(inv.Expired) != 0 {
		t.Fatalf("expected 0 active/expired leases")
	}
}

// Test 2: Active leases reconstructed under Active.
func TestInventory_ActiveLeases_ReconstructedUnderActive(t *testing.T) {
	st := store.NewMemoryStore()
	ctrl := NewRestrictedAccessController(st)

	h1, err := ctrl.Acquire(context.Background(), "rec-1", "owner-1", 2, 10*time.Minute)
	if err != nil {
		t.Fatalf("unexpected acquire: %v", err)
	}

	inv, err := Inventory(context.Background(), st, "rec-1", 2, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(inv.Active) != 1 {
		t.Fatalf("expected 1 active lease, got %d", len(inv.Active))
	}
	if inv.Active[0].Scope != h1.Scope || inv.Active[0].Owner != "owner-1" {
		t.Fatalf("mismatched active lease data")
	}
	if len(inv.FreeSlots) != 1 || inv.FreeSlots[0] != 1 {
		t.Fatalf("expected free slots [1], got %v", inv.FreeSlots)
	}
}

// Test 3: Expired leases appear exclusively under Expired.
func TestInventory_ExpiredLeases_AppearUnderExpired(t *testing.T) {
	st := store.NewMemoryStore()

	// Manually insert an expired lease into MemoryStore
	scope := "receiver:rec-1:restricted-access:0"
	_, _, err := st.TryAcquireLease(context.Background(), scope, "owner-expired", 1*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	inv, err := Inventory(context.Background(), st, "rec-1", 2, time.Now().Add(10*time.Second))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(inv.Active) != 0 {
		t.Fatalf("expected 0 active leases, got %d", len(inv.Active))
	}
	if len(inv.Expired) != 1 {
		t.Fatalf("expected 1 expired lease, got %d", len(inv.Expired))
	}
	if inv.Expired[0].Owner != "owner-expired" {
		t.Fatalf("expected owner-expired, got %s", inv.Expired[0].Owner)
	}
}

// Test 4: Inventory() performs zero store mutations.
func TestInventory_ReadOnly_DoesNotMutateStore(t *testing.T) {
	st := store.NewMemoryStore()
	ctrl := NewRestrictedAccessController(st)

	_, err := ctrl.Acquire(context.Background(), "rec-1", "owner-1", 2, 10*time.Minute)
	if err != nil {
		t.Fatalf("unexpected acquire: %v", err)
	}

	leasesBefore, _ := st.ListLeases(context.Background())

	_, err = Inventory(context.Background(), st, "rec-1", 2, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	leasesAfter, _ := st.ListLeases(context.Background())
	if len(leasesBefore) != len(leasesAfter) {
		t.Fatalf("Inventory() mutated store lease count!")
	}
}

// Test 5: CleanupExpired() releases only truly expired leases for target receiver.
func TestCleanupExpired_ReleasesOnlyExpired(t *testing.T) {
	st := store.NewMemoryStore()

	scope0 := "receiver:rec-1:restricted-access:0"
	scope1 := "receiver:rec-1:restricted-access:1"

	// acquire scope0 with 1s ttl
	_, _, _ = st.TryAcquireLease(context.Background(), scope0, "owner-exp", 1*time.Second)
	// acquire scope1 with 1h ttl
	_, _, _ = st.TryAcquireLease(context.Background(), scope1, "owner-1h", 1*time.Hour)

	cleanupTime := time.Now().Add(5 * time.Second)

	cleaned, err := CleanupExpired(context.Background(), st, "rec-1", 2, cleanupTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cleaned != 1 {
		t.Fatalf("expected 1 lease cleaned, got %d", cleaned)
	}

	// Verify scope0 is gone, scope1 remains
	_, ok0, _ := st.GetLease(context.Background(), scope0)
	_, ok1, _ := st.GetLease(context.Background(), scope1)

	if ok0 {
		t.Fatalf("scope0 should have been cleaned up")
	}
	if !ok1 {
		t.Fatalf("scope1 should remain active in store")
	}
}

// Test 6: Renewed lease is NOT deleted by outdated cleanup.
func TestCleanupExpired_RenewedLease_NotDeleted(t *testing.T) {
	st := store.NewMemoryStore()
	scope0 := "receiver:rec-1:restricted-access:0"

	// Acquire initially with 1s TTL
	_, _, _ = st.TryAcquireLease(context.Background(), scope0, "owner-1", 1*time.Second)

	// Take inventory at t+5s (lease expired)
	invTime := time.Now().Add(5 * time.Second)
	inv, _ := Inventory(context.Background(), st, "rec-1", 1, invTime)
	if len(inv.Expired) != 1 {
		t.Fatalf("expected 1 expired lease in inventory")
	}

	// Renew lease for owner-1 with long TTL
	_, _, _ = st.TryAcquireLease(context.Background(), scope0, "owner-1", 1*time.Hour)

	// Run CleanupExpired using old inventory evaluation time
	cleaned, err := CleanupExpired(context.Background(), st, "rec-1", 1, invTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cleaned != 0 {
		t.Fatalf("renewed lease must NOT be cleaned up! cleaned count = %d", cleaned)
	}
}

// Test 7: Foreign receiver leases remain strictly isolated.
func TestInventory_ForeignReceiver_Isolated(t *testing.T) {
	st := store.NewMemoryStore()
	ctrl := NewRestrictedAccessController(st)

	_, _ = ctrl.Acquire(context.Background(), "rec-A", "owner-A", 1, 10*time.Minute)

	invB, err := Inventory(context.Background(), st, "rec-B", 1, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(invB.Active) != 0 {
		t.Fatalf("rec-B inventory should not include rec-A leases")
	}
	if len(invB.FreeSlots) != 1 || invB.FreeSlots[0] != 0 {
		t.Fatalf("expected rec-B slot 0 free")
	}
}

// Test 8: Out-of-range slot produces InventoryIssue and InventoryTrusted = false.
func TestInventory_OutOfRangeSlot_Untrusted(t *testing.T) {
	st := store.NewMemoryStore()

	// Acquire slot 2 directly in store
	scope2 := "receiver:rec-1:restricted-access:2"
	_, _, _ = st.TryAcquireLease(context.Background(), scope2, "owner-2", 10*time.Minute)

	// Inventory with capacity = 1 (slot 2 is out of range)
	inv, err := Inventory(context.Background(), st, "rec-1", 1, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inv.InventoryTrusted {
		t.Fatalf("expected InventoryTrusted to be false for out-of-range slot")
	}

	hasOutOfRangeIssue := false
	for _, issue := range inv.Inconsistencies {
		if issue.Kind == IssueSlotOutOfRange {
			hasOutOfRangeIssue = true
		}
	}
	if !hasOutOfRangeIssue {
		t.Fatalf("expected IssueSlotOutOfRange issue in inventory")
	}
}

// Test 9: Non-canonical scope key produces InventoryIssue and InventoryTrusted = false.
func TestInventory_NonCanonicalScope_Untrusted(t *testing.T) {
	st := store.NewMemoryStore()

	// Insert non-canonical leading zero scope key
	badScope := "receiver:rec-1:restricted-access:01"
	_, _, _ = st.TryAcquireLease(context.Background(), badScope, "owner-bad", 10*time.Minute)

	inv, err := Inventory(context.Background(), st, "rec-1", 2, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inv.InventoryTrusted {
		t.Fatalf("expected InventoryTrusted to be false for non-canonical scope")
	}

	hasNonCanonicalIssue := false
	for _, issue := range inv.Inconsistencies {
		if issue.Kind == IssueNonCanonicalScope {
			hasNonCanonicalIssue = true
		}
	}
	if !hasNonCanonicalIssue {
		t.Fatalf("expected IssueNonCanonicalScope issue in inventory")
	}
}

// Test 10: Store error returned as technical error (never interpreted as free pool).
func TestInventory_StoreError_TechnicalError(t *testing.T) {
	techErr := errors.New("database disk I/O error")
	st := &failListStore{err: techErr}

	_, err := Inventory(context.Background(), st, "rec-1", 2, time.Now())
	if err == nil {
		t.Fatalf("expected technical error, got nil")
	}
	if !errors.Is(err, techErr) {
		t.Fatalf("expected technical error %v, got %v", techErr, err)
	}
}

// Test 11: Concurrent Renew during Inventory is race-free under -race.
func TestInventory_ConcurrentRenew_RaceFree(t *testing.T) {
	st := store.NewMemoryStore()
	ctrl := NewRestrictedAccessController(st)

	h1, _ := ctrl.Acquire(context.Background(), "rec-1", "owner-1", 2, 10*time.Minute)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_, _ = Inventory(context.Background(), st, "rec-1", 2, time.Now())
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_ = ctrl.Renew(context.Background(), h1, 10*time.Minute)
		}
	}()

	wg.Wait()
}

// Test 12: Capacity reduction (2 -> 1) marks slot 1 as out-of-range without terminating it automatically.
func TestInventory_CapacityReduction_MarksOutOfRange(t *testing.T) {
	st := store.NewMemoryStore()
	ctrl := NewRestrictedAccessController(st)

	h0, _ := ctrl.Acquire(context.Background(), "rec-1", "owner-0", 2, 10*time.Minute)
	h1, _ := ctrl.Acquire(context.Background(), "rec-1", "owner-1", 2, 10*time.Minute)

	// Reduce capacity to 1
	inv, err := Inventory(context.Background(), st, "rec-1", 1, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inv.InventoryTrusted {
		t.Fatalf("expected InventoryTrusted to be false when active slot 1 exceeds capacity 1")
	}

	// Verify both leases remain active in store (capacity reduction does NOT auto-terminate active sessions)
	l0, ok0, _ := st.GetLease(context.Background(), h0.Scope)
	l1, ok1, _ := st.GetLease(context.Background(), h1.Scope)

	if !ok0 || l0.Owner() != "owner-0" {
		t.Fatalf("h0 lease was unexpectedly modified or terminated")
	}
	if !ok1 || l1.Owner() != "owner-1" {
		t.Fatalf("h1 lease was unexpectedly modified or terminated")
	}
}

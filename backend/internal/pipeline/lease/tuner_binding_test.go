// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lease

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestTunerBindingAcquireAndRelease(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 100 * time.Millisecond})
	defer mgr.Close()

	tb := NewTunerBinding(mgr)
	ctx := context.Background()
	owner := Owner("live-session-1")
	slot := 0
	ttl := 10 * time.Minute

	// 1. Acquire Tuner Slot 0
	l, err := tb.AcquireTunerSlot(ctx, owner, slot, ttl)
	if err != nil {
		t.Fatalf("unexpected acquire error: %v", err)
	}
	if l.Scope != ScopeForTunerSlot(slot) {
		t.Errorf("expected scope %s, got %s", ScopeForTunerSlot(slot), l.Scope)
	}

	// 2. Verify GetTunerLease
	fetched, found := tb.GetTunerLease(slot)
	if !found || fetched.ID != l.ID {
		t.Errorf("expected GetTunerLease to find lease %s, got found=%v", l.ID, found)
	}

	// 3. Attempting second acquire on same slot must fail with ErrScopeConflict
	_, err = tb.AcquireTunerSlot(ctx, "live-session-2", slot, ttl)
	if !errors.Is(err, ErrScopeConflict) {
		t.Errorf("expected ErrScopeConflict, got %v", err)
	}

	// 4. Release Tuner Slot
	rel, err := tb.ReleaseTunerSlot(l.ID, owner, ReasonReleasedByOwner)
	if err != nil {
		t.Fatalf("release failed: %v", err)
	}
	if rel.State != StateReleased {
		t.Errorf("expected state RELEASED, got %s", rel.State)
	}

	// 5. Verify GetTunerLease returns false after release
	_, found = tb.GetTunerLease(slot)
	if found {
		t.Errorf("expected GetTunerLease to return false after release")
	}

	// 6. Now another session can acquire slot 0
	l2, err := tb.AcquireTunerSlot(ctx, "dvr-recording-1", slot, ttl)
	if err != nil {
		t.Fatalf("acquire after release failed: %v", err)
	}
	if l2.Owner != "dvr-recording-1" {
		t.Errorf("expected owner dvr-recording-1, got %s", l2.Owner)
	}
}

func TestTunerBindingExpiration(t *testing.T) {
	var mockMu sync.Mutex
	mockTime := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	getMockTime := func() time.Time {
		mockMu.Lock()
		defer mockMu.Unlock()
		return mockTime
	}

	mgr := NewManager(ManagerConfig{
		SweepInterval: 1 * time.Hour,
		Clock:         getMockTime,
	})
	defer mgr.Close()

	tb := NewTunerBinding(mgr)
	slot := 1

	l, err := tb.AcquireTunerSlot(context.Background(), "live-session-1", slot, 10*time.Second)
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	// Advance clock past TTL
	mockMu.Lock()
	mockTime = mockTime.Add(15 * time.Second)
	mockMu.Unlock()

	// GetTunerLease should evaluate expiration and return false
	_, found := tb.GetTunerLease(slot)
	if found {
		t.Errorf("expected tuner lease to be expired and not found")
	}

	// Acquire on expired slot should succeed immediately
	l2, err := tb.AcquireTunerSlot(context.Background(), "dvr-recording-1", slot, 10*time.Minute)
	if err != nil {
		t.Fatalf("acquire after expiration failed: %v", err)
	}
	if l2.ID == l.ID {
		t.Errorf("expected new lease ID for dvr-recording-1")
	}
}

func TestTunerBindingInvalidSlot(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 100 * time.Millisecond})
	defer mgr.Close()

	tb := NewTunerBinding(mgr)
	_, err := tb.AcquireTunerSlot(context.Background(), "worker", -1, 5*time.Minute)
	if !errors.Is(err, ErrInvalidScope) {
		t.Errorf("expected ErrInvalidScope for negative slot, got %v", err)
	}

	_, found := tb.GetTunerLease(-1)
	if found {
		t.Errorf("expected GetTunerLease for negative slot to be false")
	}
}

func TestTunerBindingNilManager(t *testing.T) {
	var tb *TunerBinding
	_, err := tb.AcquireTunerSlot(context.Background(), "worker", 0, 5*time.Minute)
	if !errors.Is(err, ErrBindingUnavailable) {
		t.Errorf("expected ErrBindingUnavailable for nil TunerBinding, got %v", err)
	}

	_, err = tb.ReleaseTunerSlot("id", "worker", ReasonReleasedByOwner)
	if !errors.Is(err, ErrBindingUnavailable) {
		t.Errorf("expected ErrBindingUnavailable for nil TunerBinding Release, got %v", err)
	}

	_, err = tb.RenewTunerSlot("id", "worker", 5*time.Minute)
	if !errors.Is(err, ErrBindingUnavailable) {
		t.Errorf("expected ErrBindingUnavailable for nil TunerBinding Renew, got %v", err)
	}

	_, found := tb.GetTunerLease(0)
	if found {
		t.Errorf("expected GetTunerLease for nil TunerBinding to be false")
	}
}

func TestTunerBindingRaceConditions(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 10 * time.Millisecond})
	defer mgr.Close()
	tb := NewTunerBinding(mgr)

	const numWorkers = 40
	const opsPerWorker = 80
	var wg sync.WaitGroup

	ctx := context.Background()

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		workerID := Owner(fmt.Sprintf("tuner-worker-%d", i))
		slot := i % 4 // 4 tuner slots shared across 40 workers

		go func(wOwner Owner, wSlot int) {
			defer wg.Done()
			for j := 0; j < opsPerWorker; j++ {
				l, err := tb.AcquireTunerSlot(ctx, wOwner, wSlot, 30*time.Millisecond)
				if err == nil && l != nil {
					time.Sleep(1 * time.Millisecond)
					if j%2 == 0 {
						_, _ = tb.RenewTunerSlot(l.ID, wOwner, 50*time.Millisecond)
					}
					_, _ = tb.ReleaseTunerSlot(l.ID, wOwner, ReasonReleasedByOwner)
				} else {
					_, _ = tb.GetTunerLease(wSlot)
				}
			}
		}(workerID, slot)
	}

	wg.Wait()
}

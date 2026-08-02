// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lease

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestLeaseIsActive(t *testing.T) {
	now := time.Now()

	var nilLease *Lease
	if nilLease.IsActive(now) {
		t.Errorf("expected nil lease IsActive to be false")
	}

	l := &Lease{
		State:     StateAcquired,
		ExpiresAt: now.Add(10 * time.Minute),
	}
	if !l.IsActive(now) {
		t.Errorf("expected active lease to be active")
	}

	if l.IsActive(now.Add(11 * time.Minute)) {
		t.Errorf("expected lease past expiration to not be active")
	}

	l.State = StateReleased
	if l.IsActive(now) {
		t.Errorf("expected released lease to not be active")
	}
}

func TestManagerAcquireSuccess(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 100 * time.Millisecond})
	defer mgr.Close()

	ctx := context.Background()
	owner := Owner("worker-1")
	scope := Scope("tuner:0")
	ttl := 5 * time.Minute

	l, err := mgr.Acquire(ctx, owner, scope, ttl)
	if err != nil {
		t.Fatalf("unexpected acquire error: %v", err)
	}

	if l.ID == "" {
		t.Errorf("expected non-empty lease ID")
	}
	if l.Owner != owner {
		t.Errorf("expected owner %s, got %s", owner, l.Owner)
	}
	if l.Scope != scope {
		t.Errorf("expected scope %s, got %s", scope, l.Scope)
	}
	if l.State != StateAcquired {
		t.Errorf("expected state %s, got %s", StateAcquired, l.State)
	}
	if l.ReasonCode != ReasonAcquired {
		t.Errorf("expected reason %s, got %s", ReasonAcquired, l.ReasonCode)
	}
}

func TestManagerAcquireScopeConflict(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 100 * time.Millisecond})
	defer mgr.Close()

	ctx := context.Background()
	scope := Scope("tuner:0")

	l1, err := mgr.Acquire(ctx, "owner-1", scope, 5*time.Minute)
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	if l1 == nil {
		t.Fatalf("first acquire returned nil lease")
	}

	// Attempting second acquire on same scope must fail
	_, err = mgr.Acquire(ctx, "owner-2", scope, 5*time.Minute)
	if !errors.Is(err, ErrScopeConflict) {
		t.Errorf("expected ErrScopeConflict, got %v", err)
	}
}

func TestManagerIdempotentRelease(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 100 * time.Millisecond})
	defer mgr.Close()

	ctx := context.Background()
	owner := Owner("owner-1")
	scope := Scope("tuner:0")

	l, err := mgr.Acquire(ctx, owner, scope, 5*time.Minute)
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	// First release
	rel1, err := mgr.Release(ctx, l.ID, owner, ReasonReleasedByOwner)
	if err != nil {
		t.Fatalf("first release failed: %v", err)
	}
	if rel1.State != StateReleased {
		t.Errorf("expected state RELEASED, got %s", rel1.State)
	}

	// Second release (Idempotency check: must succeed cleanly without error)
	rel2, err := mgr.Release(ctx, l.ID, owner, ReasonReleasedByOwner)
	if err != nil {
		t.Fatalf("second release failed: %v", err)
	}
	if rel2.State != StateReleased {
		t.Errorf("expected state RELEASED on second call, got %s", rel2.State)
	}

	// Scope is now free for new acquisition
	l2, err := mgr.Acquire(ctx, "owner-2", scope, 5*time.Minute)
	if err != nil {
		t.Fatalf("acquire after release failed: %v", err)
	}
	if l2.Owner != "owner-2" {
		t.Errorf("expected owner-2, got %s", l2.Owner)
	}
}

func TestManagerReleaseOwnerMismatch(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 100 * time.Millisecond})
	defer mgr.Close()

	ctx := context.Background()
	l, err := mgr.Acquire(ctx, "owner-1", "tuner:0", 5*time.Minute)
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	_, err = mgr.Release(ctx, l.ID, "wrong-owner", ReasonReleasedByOwner)
	if !errors.Is(err, ErrOwnerMismatch) {
		t.Errorf("expected ErrOwnerMismatch, got %v", err)
	}
}

func TestManagerExpirationSweep(t *testing.T) {
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

	l, err := mgr.Acquire(context.Background(), "owner-1", "tuner:0", 10*time.Second)
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	mockMu.Lock()
	mockTime = mockTime.Add(15 * time.Second)
	currTime := mockTime
	mockMu.Unlock()

	expiredCount := mgr.Sweep(currTime)
	if expiredCount != 1 {
		t.Errorf("expected 1 expired lease, got %d", expiredCount)
	}

	fetched, ok := mgr.Get(l.ID)
	if !ok {
		t.Fatalf("failed to fetch lease")
	}
	if fetched.State != StateExpired {
		t.Errorf("expected state EXPIRED, got %s", fetched.State)
	}
	if fetched.ReasonCode != ReasonExpired {
		t.Errorf("expected reason LEASE_EXPIRED, got %s", fetched.ReasonCode)
	}

	// Scope is now free for another owner
	l2, err := mgr.Acquire(context.Background(), "owner-2", "tuner:0", 10*time.Second)
	if err != nil {
		t.Fatalf("acquire after expiration failed: %v", err)
	}
	if l2.Owner != "owner-2" {
		t.Errorf("expected owner-2, got %s", l2.Owner)
	}
}

func TestExpiredButUnsweptConsistency(t *testing.T) {
	var mockMu sync.Mutex
	mockTime := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	getMockTime := func() time.Time {
		mockMu.Lock()
		defer mockMu.Unlock()
		return mockTime
	}

	mgr := NewManager(ManagerConfig{
		SweepInterval: 1 * time.Hour, // Sweep hasn't run yet!
		Clock:         getMockTime,
	})
	defer mgr.Close()

	l, err := mgr.Acquire(context.Background(), "owner-1", "tuner:0", 10*time.Second)
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	// Advance clock past expiration WITHOUT calling Sweep()
	mockMu.Lock()
	mockTime = mockTime.Add(15 * time.Second)
	mockMu.Unlock()

	// 1. Get() must return EXPIRED consistently
	g, ok := mgr.Get(l.ID)
	if !ok {
		t.Fatalf("expected lease to exist")
	}
	if g.State != StateExpired {
		t.Errorf("expected Get() state EXPIRED for unswept lease, got %s", g.State)
	}

	// 2. Renew() must fail with ErrLeaseInactive
	_, err = mgr.Renew(context.Background(), l.ID, "owner-1", 10*time.Minute)
	if !errors.Is(err, ErrLeaseInactive) {
		t.Errorf("expected ErrLeaseInactive on renew expired unswept lease, got %v", err)
	}

	// 3. Release() must return state EXPIRED idempotently without marking it RELEASED
	rel, err := mgr.Release(context.Background(), l.ID, "owner-1", ReasonReleasedByOwner)
	if err != nil {
		t.Fatalf("unexpected release error: %v", err)
	}
	if rel.State != StateExpired {
		t.Errorf("expected Release() to preserve state EXPIRED, got %s", rel.State)
	}

	// 4. Acquire on scope must succeed immediately because scope index was cleared upon expiration
	l2, err := mgr.Acquire(context.Background(), "owner-2", "tuner:0", 10*time.Minute)
	if err != nil {
		t.Fatalf("expected Acquire on expired scope to succeed, got %v", err)
	}
	if l2.Owner != "owner-2" {
		t.Errorf("expected owner-2, got %s", l2.Owner)
	}
}

func TestManagerRenew(t *testing.T) {
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

	l, err := mgr.Acquire(context.Background(), "owner-1", "tuner:0", 10*time.Minute)
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	renewed, err := mgr.Renew(context.Background(), l.ID, "owner-1", 20*time.Minute)
	if err != nil {
		t.Fatalf("renew failed: %v", err)
	}

	expectedExpiry := mockTime.Add(20 * time.Minute)
	if !renewed.ExpiresAt.Equal(expectedExpiry) {
		t.Errorf("expected expiry %v, got %v", expectedExpiry, renewed.ExpiresAt)
	}
}

func TestManagerRevoke(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 100 * time.Millisecond})
	defer mgr.Close()

	l, err := mgr.Acquire(context.Background(), "owner-1", "tuner:0", 10*time.Minute)
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	rev, err := mgr.Revoke(l.ID, ReasonPreempted)
	if err != nil {
		t.Fatalf("revoke failed: %v", err)
	}
	if rev.State != StateRevoked {
		t.Errorf("expected state REVOKED, got %s", rev.State)
	}
	if rev.ReasonCode != ReasonPreempted {
		t.Errorf("expected reason LEASE_PREEMPTED, got %s", rev.ReasonCode)
	}
}

func TestManagerInvalidParams(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 100 * time.Millisecond})
	defer mgr.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := mgr.Acquire(ctx, "owner-1", "tuner:0", 5*time.Minute)
	if err == nil {
		t.Errorf("expected error on canceled context")
	}

	ctx = context.Background()
	_, err = mgr.Acquire(ctx, "", "tuner:0", 5*time.Minute)
	if !errors.Is(err, ErrInvalidOwner) {
		t.Errorf("expected ErrInvalidOwner on empty owner, got %v", err)
	}

	_, err = mgr.Acquire(ctx, "owner-1", "", 5*time.Minute)
	if !errors.Is(err, ErrInvalidScope) {
		t.Errorf("expected ErrInvalidScope on empty scope, got %v", err)
	}

	_, err = mgr.Acquire(ctx, "owner-1", "tuner:0", -1*time.Minute)
	if !errors.Is(err, ErrInvalidTTL) {
		t.Errorf("expected ErrInvalidTTL on negative TTL, got %v", err)
	}

	_, err = mgr.Renew(ctx, "non-existent-id", "owner-1", 5*time.Minute)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound on renew non-existent lease, got %v", err)
	}

	_, err = mgr.Renew(ctx, "non-existent-id", "owner-1", -1*time.Minute)
	if !errors.Is(err, ErrInvalidTTL) {
		t.Errorf("expected ErrInvalidTTL on renew negative TTL, got %v", err)
	}

	_, err = mgr.Revoke("non-existent-id", ReasonRevokedByAdmin)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound on revoke non-existent lease, got %v", err)
	}
}

func TestManagerRejectsOperationsAfterClose(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 100 * time.Millisecond})

	l, err := mgr.Acquire(context.Background(), "owner-1", "tuner:0", 5*time.Minute)
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	if err := mgr.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	// 1. Acquire after Close() must return ErrManagerClosed
	_, err = mgr.Acquire(context.Background(), "owner-2", "tuner:1", 5*time.Minute)
	if !errors.Is(err, ErrManagerClosed) {
		t.Errorf("expected ErrManagerClosed on Acquire after Close(), got %v", err)
	}

	// 2. Renew after Close() must return ErrManagerClosed
	_, err = mgr.Renew(context.Background(), l.ID, "owner-1", 10*time.Minute)
	if !errors.Is(err, ErrManagerClosed) {
		t.Errorf("expected ErrManagerClosed on Renew after Close(), got %v", err)
	}

	// 3. Release of existing lease after Close() remains allowed for cleanup
	rel, err := mgr.Release(context.Background(), l.ID, "owner-1", ReasonReleasedByOwner)
	if err != nil {
		t.Fatalf("release after Close() should be allowed for cleanup, got %v", err)
	}
	if rel.State != StateReleased {
		t.Errorf("expected state RELEASED, got %s", rel.State)
	}
}

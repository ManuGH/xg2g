// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package session

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockUpstream implements io.ReadCloser
type mockUpstream struct {
	io.Reader
	isClosed atomic.Bool
}

func newMockUpstream() *mockUpstream {
	return &mockUpstream{
		Reader: bytes.NewReader(make([]byte, 1024)),
	}
}

func (m *mockUpstream) Close() error {
	m.isClosed.Store(true)
	return nil
}

// mockConnector tracks dial invocations
type mockConnector struct {
	mu           sync.Mutex
	connectCount int
	dialDelay    time.Duration
	failFirst    int
	failErr      error
}

func (c *mockConnector) Connect(ctx context.Context, key SessionKey) (io.ReadCloser, error) {
	c.mu.Lock()
	c.connectCount++
	delay := c.dialDelay
	c.mu.Unlock()

	if delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.failFirst > 0 {
		c.failFirst--
		err := c.failErr
		if err == nil {
			err = errors.New("upstream connection refused")
		}
		return nil, err
	}

	return newMockUpstream(), nil
}

func (c *mockConnector) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connectCount
}

func TestSessionKey_Canonicalize(t *testing.T) {
	k1 := SessionKey{
		ReceiverHost: "10.10.55.64:8001",
		ServiceRef:   "1:0:19:2B66:3F3:1:C00000:0:0:0:",
	}.Canonicalize()

	if k1.ReceiverHost != "10.10.55.64" || k1.StreamPort != 8001 {
		t.Fatalf("unexpected host/port: %s:%d", k1.ReceiverHost, k1.StreamPort)
	}
	if k1.ServiceRef != "1:0:19:2B66:3F3:1:C00000:0:0:0" {
		t.Fatalf("trailing colon not stripped: %s", k1.ServiceRef)
	}
	if k1.Profile != "native" || k1.SourceType != "enigma2" {
		t.Fatalf("defaults not applied: profile=%s source=%s", k1.Profile, k1.SourceType)
	}
}

func TestManager_CoalescedStart_20ConcurrentAcquires(t *testing.T) {
	connector := &mockConnector{dialDelay: 50 * time.Millisecond}
	mgr := NewManager(ManagerConfig{
		WarmHoldDuration: 100 * time.Millisecond,
		ConnectTimeout:   1 * time.Second,
	}, connector)
	defer mgr.Close()

	key := SessionKey{ReceiverHost: "10.10.55.64", ServiceRef: "1:0:19:ORF1"}

	const numCallers = 20
	var wg sync.WaitGroup
	leases := make([]*Lease, numCallers)
	errs := make([]error, numCallers)

	for i := 0; i < numCallers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			lease, err := mgr.Acquire(ctx, key)
			leases[idx] = lease
			errs[idx] = err
		}(i)
	}

	wg.Wait()

	if connector.Count() != 1 {
		t.Fatalf("expected exactly 1 upstream connect, got %d", connector.Count())
	}

	for i := 0; i < numCallers; i++ {
		if errs[i] != nil {
			t.Fatalf("caller %d failed: %v", i, errs[i])
		}
		if leases[i] == nil {
			t.Fatalf("caller %d received nil lease", i)
		}
		if leases[i].State() != StateActive {
			t.Fatalf("caller %d lease not active: %v", i, leases[i].State())
		}
	}

	// Release all leases
	for i := 0; i < numCallers; i++ {
		leases[i].Release()
	}

	// Double release is safe & idempotent
	for i := 0; i < numCallers; i++ {
		leases[i].Release()
	}
}

func TestManager_CancelSubsetDuringStarting(t *testing.T) {
	connector := &mockConnector{dialDelay: 100 * time.Millisecond}
	mgr := NewManager(ManagerConfig{
		WarmHoldDuration: 200 * time.Millisecond,
		ConnectTimeout:   1 * time.Second,
	}, connector)
	defer mgr.Close()

	key := SessionKey{ReceiverHost: "10.10.55.64", ServiceRef: "1:0:19:ZDF"}

	const total = 20
	const cancelCount = 5
	var wg sync.WaitGroup
	leases := make([]*Lease, total)
	errs := make([]error, total)

	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			var ctx context.Context
			var cancel context.CancelFunc

			if idx < cancelCount {
				// Cancel early during start
				ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
			} else {
				ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
			}
			defer cancel()

			lease, err := mgr.Acquire(ctx, key)
			leases[idx] = lease
			errs[idx] = err
		}(i)
	}

	wg.Wait()

	if connector.Count() != 1 {
		t.Fatalf("expected exactly 1 upstream connect, got %d", connector.Count())
	}

	// Cancelled callers must report context error
	for i := 0; i < cancelCount; i++ {
		if !errors.Is(errs[i], context.DeadlineExceeded) && !errors.Is(errs[i], context.Canceled) {
			t.Fatalf("expected context error for caller %d, got %v", i, errs[i])
		}
	}

	// Remaining 15 callers must receive active leases
	for i := cancelCount; i < total; i++ {
		if errs[i] != nil {
			t.Fatalf("caller %d failed: %v", i, errs[i])
		}
		if leases[i] == nil || leases[i].State() != StateActive {
			t.Fatalf("caller %d received invalid lease", i)
		}
		leases[i].Release()
	}
}

func TestManager_AllWaitersCancelDuringStarting(t *testing.T) {
	connector := &mockConnector{dialDelay: 150 * time.Millisecond}
	mgr := NewManager(ManagerConfig{
		WarmHoldDuration: 100 * time.Millisecond,
		ConnectTimeout:   1 * time.Second,
	}, connector)
	defer mgr.Close()

	key := SessionKey{ReceiverHost: "10.10.55.64", ServiceRef: "1:0:19:ORF2"}

	const total = 5
	var wg sync.WaitGroup

	for i := 0; i < total; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()

			_, _ = mgr.Acquire(ctx, key)
		}()
	}

	wg.Wait()

	// Wait for dial to finish and abort
	time.Sleep(200 * time.Millisecond)

	// Clean up map should have 0 active
	if mgr.ActiveCount() != 0 {
		t.Fatalf("expected 0 active sessions after all cancelled, got %d", mgr.ActiveCount())
	}

	// Next acquire should succeed and dial fresh
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	lease, err := mgr.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("subsequent acquire failed: %v", err)
	}
	defer lease.Release()

	if connector.Count() != 2 {
		t.Fatalf("expected 2 total connects, got %d", connector.Count())
	}
}

func TestManager_WarmHoldAndReactivate(t *testing.T) {
	connector := &mockConnector{dialDelay: 10 * time.Millisecond}
	mgr := NewManager(ManagerConfig{
		WarmHoldDuration: 150 * time.Millisecond,
		ConnectTimeout:   1 * time.Second,
	}, connector)
	defer mgr.Close()

	key := SessionKey{ReceiverHost: "10.10.55.64", ServiceRef: "1:0:19:SKY1"}

	ctx := context.Background()
	lease1, err := mgr.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("acquire 1 failed: %v", err)
	}

	// Release: session enters StateHolding
	lease1.Release()

	if lease1.State() != StateHolding {
		t.Fatalf("expected StateHolding, got %v", lease1.State())
	}

	// Re-acquire before 150ms expiry
	time.Sleep(30 * time.Millisecond)
	lease2, err := mgr.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("acquire 2 failed: %v", err)
	}
	defer lease2.Release()

	if lease2.State() != StateActive {
		t.Fatalf("expected StateActive after re-acquire, got %v", lease2.State())
	}

	// Should not have dialed a second time!
	if connector.Count() != 1 {
		t.Fatalf("expected exactly 1 connect due to warm-hold, got %d", connector.Count())
	}
}

func TestManager_WarmHoldExpiryTeardown(t *testing.T) {
	connector := &mockConnector{dialDelay: 10 * time.Millisecond}
	mgr := NewManager(ManagerConfig{
		WarmHoldDuration: 50 * time.Millisecond,
		ConnectTimeout:   1 * time.Second,
	}, connector)
	defer mgr.Close()

	key := SessionKey{ReceiverHost: "10.10.55.64", ServiceRef: "1:0:19:SKY2"}

	ctx := context.Background()
	lease1, err := mgr.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("acquire 1 failed: %v", err)
	}

	lease1.Release()

	// Wait past 50ms hold duration
	time.Sleep(100 * time.Millisecond)

	if mgr.ActiveCount() != 0 {
		t.Fatalf("expected 0 active sessions after hold expiration, got %d", mgr.ActiveCount())
	}

	// Next acquire must create a fresh connection
	lease2, err := mgr.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("acquire 2 failed: %v", err)
	}
	defer lease2.Release()

	if connector.Count() != 2 {
		t.Fatalf("expected 2 connects after hold expired, got %d", connector.Count())
	}
}

func TestManager_FailedStartAndSubsequentRetry(t *testing.T) {
	connector := &mockConnector{
		dialDelay: 50 * time.Millisecond,
		failFirst: 1,
		failErr:   errors.New("tuner locked"),
	}
	mgr := NewManager(ManagerConfig{
		WarmHoldDuration: 50 * time.Millisecond,
		ConnectTimeout:   1 * time.Second,
	}, connector)
	defer mgr.Close()

	key := SessionKey{ReceiverHost: "10.10.55.64", ServiceRef: "1:0:19:ERR"}

	const total = 5
	var wg sync.WaitGroup
	errs := make([]error, total)

	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()

			_, err := mgr.Acquire(ctx, key)
			errs[idx] = err
		}(i)
	}

	wg.Wait()

	// All 5 must see the error
	for i := 0; i < total; i++ {
		if errs[i] == nil || errs[i].Error() != "tuner locked" {
			t.Fatalf("expected tuner locked error, got %v", errs[i])
		}
	}

	// Map must be empty (no zombie)
	if mgr.ActiveCount() != 0 {
		t.Fatalf("expected 0 active sessions after failure, got %d", mgr.ActiveCount())
	}

	// Retry should now succeed
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	lease, err := mgr.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("subsequent acquire failed: %v", err)
	}
	defer lease.Release()

	if connector.Count() != 2 {
		t.Fatalf("expected 2 connects, got %d", connector.Count())
	}
}

func TestManager_CloseTerminal(t *testing.T) {
	connector := &mockConnector{}
	mgr := NewManager(ManagerConfig{
		WarmHoldDuration: 500 * time.Millisecond,
		ConnectTimeout:   1 * time.Second,
	}, connector)

	key := SessionKey{ReceiverHost: "10.10.55.64", ServiceRef: "1:0:19:CLOSE"}

	ctx := context.Background()
	lease, err := mgr.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	// Close manager
	if err := mgr.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	// Existing session should be stopped
	if lease.State() != StateStopped {
		t.Fatalf("expected StateStopped, got %v", lease.State())
	}

	// Any subsequent acquire must return ErrManagerClosed
	_, err = mgr.Acquire(ctx, key)
	if !errors.Is(err, ErrManagerClosed) {
		t.Fatalf("expected ErrManagerClosed, got %v", err)
	}
}

func TestManager_UserEndToEndLifecycleScenario(t *testing.T) {
	// 20 Acquire gleichzeitig
	// → ConnectCount = 1
	// 5 Caller canceln während Starting
	// → verbleibende 15 bekommen dieselbe Session
	// → ConnectCount weiterhin = 1
	// 15 Release gleichzeitig
	// → RefCount = 0
	// → Holding
	// Acquire exakt beim Hold-Timeout
	// → entweder bestehende Session atomar reaktiviert
	//    ODER alte vollständig beendet + exakt eine neue gestartet
	// → niemals Zombie / doppelte Session
	connector := &mockConnector{dialDelay: 100 * time.Millisecond}
	mgr := NewManager(ManagerConfig{
		WarmHoldDuration: 80 * time.Millisecond,
		ConnectTimeout:   1 * time.Second,
	}, connector)
	defer mgr.Close()

	key := SessionKey{ReceiverHost: "10.10.55.64", ServiceRef: "1:0:19:USER_SCENARIO"}

	const total = 20
	const cancelCount = 5
	var wg sync.WaitGroup
	leases := make([]*Lease, total)
	errs := make([]error, total)

	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			var ctx context.Context
			var cancel context.CancelFunc

			if idx < cancelCount {
				ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
			} else {
				ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
			}
			defer cancel()

			lease, err := mgr.Acquire(ctx, key)
			leases[idx] = lease
			errs[idx] = err
		}(i)
	}

	wg.Wait()

	if connector.Count() != 1 {
		t.Fatalf("step 1: expected ConnectCount == 1, got %d", connector.Count())
	}

	// 15 remaining callers must have received valid leases on the same active session
	for i := cancelCount; i < total; i++ {
		if errs[i] != nil {
			t.Fatalf("step 2: caller %d failed: %v", i, errs[i])
		}
		if leases[i] == nil || leases[i].State() != StateActive {
			t.Fatalf("step 2: caller %d invalid lease state: %v", i, leases[i])
		}
	}

	// 15 Release gleichzeitig
	var releaseWg sync.WaitGroup
	for i := cancelCount; i < total; i++ {
		releaseWg.Add(1)
		go func(l *Lease) {
			defer releaseWg.Done()
			l.Release()
		}(leases[i])
	}
	releaseWg.Wait()

	// Wait almost to the hold timeout
	time.Sleep(80 * time.Millisecond)

	// Acquire right at/around hold boundary
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	finalLease, err := mgr.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("step 3: acquire at hold boundary failed: %v", err)
	}
	defer finalLease.Release()

	if finalLease.State() != StateActive {
		t.Fatalf("step 3: final lease not active: %v", finalLease.State())
	}

	// Connect count must be either 1 (reactivated holding) or 2 (cleanly expired and restarted fresh)
	count := connector.Count()
	if count != 1 && count != 2 {
		t.Fatalf("step 3: expected ConnectCount 1 or 2, got %d", count)
	}

	// Active count in registry must be exactly 1 (never zombie / duplicate session)
	if mgr.ActiveCount() != 1 {
		t.Fatalf("step 3: expected exactly 1 active session in map, got %d", mgr.ActiveCount())
	}
}

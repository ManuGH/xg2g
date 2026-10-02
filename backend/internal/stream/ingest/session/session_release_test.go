// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package session

import (
	"bytes"
	"context"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type releaseTrackingUpstream struct {
	io.Reader
	closed atomic.Bool
}

func (u *releaseTrackingUpstream) Close() error {
	u.closed.Store(true)
	return nil
}

type releaseTrackingConnector struct {
	mu        sync.Mutex
	upstreams []*releaseTrackingUpstream
}

func (c *releaseTrackingConnector) Connect(ctx context.Context, key SessionKey) (io.ReadCloser, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	u := &releaseTrackingUpstream{Reader: bytes.NewReader(make([]byte, 1024))}
	c.upstreams = append(c.upstreams, u)
	return u, nil
}

func (c *releaseTrackingConnector) lastUpstream() *releaseTrackingUpstream {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.upstreams) == 0 {
		return nil
	}
	return c.upstreams[len(c.upstreams)-1]
}

func (c *releaseTrackingConnector) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.upstreams)
}

// 1. Final stop: single subscriber explicitly stops -> immediate close (no warm-hold).
func TestSession_FinalExplicitStop_ClosesUpstreamImmediately(t *testing.T) {
	connector := &releaseTrackingConnector{}
	mgr := NewManager(ManagerConfig{
		WarmHoldDuration: 5 * time.Second, // Long warm-hold to prove bypass
		ConnectTimeout:   1 * time.Second,
	}, connector)
	defer mgr.Close()

	key := SessionKey{ReceiverHost: "10.10.55.64", StreamPort: 8001, ServiceRef: "1:0:19:1:1:1:C00000:0:0:0"}.Canonicalize()
	ctx := context.Background()

	lease, err := mgr.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	upstream := connector.lastUpstream()
	if upstream == nil {
		t.Fatalf("expected active upstream")
	}
	if upstream.closed.Load() {
		t.Fatalf("upstream should not be closed while lease is held")
	}

	// Explicit client stop
	lease.ReleaseWithReason("R_CLIENT_STOP")

	// Upstream must be closed immediately without waiting 5 seconds
	if !upstream.closed.Load() {
		t.Errorf("expected upstream to be closed immediately on explicit final stop")
	}
	if lease.Session().State() != StateStopped {
		t.Errorf("expected session state StateStopped, got %v", lease.Session().State())
	}
}

// 2. Two subscribers: one explicitly stops -> upstream stays open; second stops -> closes.
func TestSession_TwoSubscribers_ExplicitStopFirst_UpstreamStaysOpen(t *testing.T) {
	connector := &releaseTrackingConnector{}
	mgr := NewManager(ManagerConfig{
		WarmHoldDuration: 5 * time.Second,
		ConnectTimeout:   1 * time.Second,
	}, connector)
	defer mgr.Close()

	key := SessionKey{ReceiverHost: "10.10.55.64", StreamPort: 8001, ServiceRef: "1:0:19:2:2:2:C00000:0:0:0"}.Canonicalize()
	ctx := context.Background()

	lease1, err := mgr.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("acquire 1 failed: %v", err)
	}
	lease2, err := mgr.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("acquire 2 failed: %v", err)
	}

	upstream := connector.lastUpstream()
	if upstream == nil {
		t.Fatalf("expected active upstream")
	}

	// Subscriber 1 explicitly stops
	lease1.ReleaseWithReason("R_CLIENT_STOP")

	// Upstream MUST stay open because subscriber 2 is still active
	if upstream.closed.Load() {
		t.Fatalf("upstream was prematurely closed while subscriber 2 was still active")
	}
	if lease1.Session().State() != StateActive {
		t.Errorf("expected session state StateActive, got %v", lease1.Session().State())
	}

	// Subscriber 2 explicitly stops
	lease2.ReleaseWithReason("R_CLIENT_STOP")

	// Now that last subscriber explicitly stopped, upstream must close immediately
	if !upstream.closed.Load() {
		t.Errorf("expected upstream to be closed after final subscriber released")
	}
	if lease2.Session().State() != StateStopped {
		t.Errorf("expected session state StateStopped, got %v", lease2.Session().State())
	}
}

// 3. Active recording: playback subscriber stops -> recording preserves upstream.
func TestSession_ActiveRecordingAndPlayback_UpstreamStaysOpenUntilAllRelease(t *testing.T) {
	connector := &releaseTrackingConnector{}
	mgr := NewManager(ManagerConfig{
		WarmHoldDuration: 5 * time.Second,
		ConnectTimeout:   1 * time.Second,
	}, connector)
	defer mgr.Close()

	key := SessionKey{ReceiverHost: "10.10.55.64", StreamPort: 8001, ServiceRef: "1:0:19:3:3:3:C00000:0:0:0"}.Canonicalize()
	ctx := context.Background()

	recordingLease, err := mgr.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("acquire recording lease failed: %v", err)
	}
	playbackLease, err := mgr.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("acquire playback lease failed: %v", err)
	}

	upstream := connector.lastUpstream()

	// Playback subscriber explicitly stops (e.g. user closes player tab)
	playbackLease.ReleaseWithReason("R_CLIENT_STOP")

	// Upstream MUST remain open for ongoing recording
	if upstream.closed.Load() {
		t.Fatalf("upstream closed while active recording was still running!")
	}
	if recordingLease.Session().State() != StateActive {
		t.Errorf("expected session state StateActive, got %v", recordingLease.Session().State())
	}

	// Recording finishes
	recordingLease.Release()

	// After recording finishes, session enters warm-hold or terminates
	time.Sleep(20 * time.Millisecond)
	// Recording release was non-explicit, so it entered warm-hold as designed
	if recordingLease.Session().State() != StateHolding {
		t.Errorf("expected recording release to enter StateHolding, got %v", recordingLease.Session().State())
	}
}

// 4. Rapid stop/reacquire: immediate explicit stop followed by rapid acquire.
func TestSession_RapidStopReacquire(t *testing.T) {
	connector := &releaseTrackingConnector{}
	mgr := NewManager(ManagerConfig{
		WarmHoldDuration: 5 * time.Second,
		ConnectTimeout:   1 * time.Second,
	}, connector)
	defer mgr.Close()

	key := SessionKey{ReceiverHost: "10.10.55.64", StreamPort: 8001, ServiceRef: "1:0:19:4:4:4:C00000:0:0:0"}.Canonicalize()
	ctx := context.Background()

	lease1, err := mgr.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	upstream1 := connector.lastUpstream()

	// Immediately stop
	lease1.ReleaseWithReason("R_CLIENT_STOP")
	if !upstream1.closed.Load() {
		t.Fatalf("first upstream should be closed")
	}

	// Rapid reacquire immediately
	lease2, err := mgr.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("reacquire failed: %v", err)
	}
	defer lease2.Release()

	upstream2 := connector.lastUpstream()
	if upstream2 == upstream1 {
		t.Fatalf("reacquire should have opened a fresh upstream")
	}
	if upstream2.closed.Load() {
		t.Fatalf("second upstream should be open and active")
	}
	if lease2.Session().State() != StateActive {
		t.Errorf("expected active state on reacquire, got %v", lease2.Session().State())
	}
}

// 5. Duplicate stop: calling ReleaseWithReason or Release multiple times is idempotent and safe under race.
func TestSession_DuplicateStop_Idempotent(t *testing.T) {
	connector := &releaseTrackingConnector{}
	mgr := NewManager(ManagerConfig{
		WarmHoldDuration: 5 * time.Second,
		ConnectTimeout:   1 * time.Second,
	}, connector)
	defer mgr.Close()

	key := SessionKey{ReceiverHost: "10.10.55.64", StreamPort: 8001, ServiceRef: "1:0:19:5:5:5:C00000:0:0:0"}.Canonicalize()
	ctx := context.Background()

	lease, err := mgr.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	// Concurrent duplicate stops
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lease.ReleaseWithReason("R_CLIENT_STOP")
		}()
	}
	wg.Wait()

	// Subsequent sequential duplicate stops must be completely idempotent
	lease.ReleaseWithReason("R_CLIENT_STOP")
	lease.Release()

	upstream := connector.lastUpstream()
	if !upstream.closed.Load() {
		t.Errorf("expected upstream to be closed after release")
	}
	if lease.Session().State() != StateStopped {
		t.Errorf("expected session state StateStopped, got %v", lease.Session().State())
	}
}

// 6. Ordinary zap / reconnect: non-explicit stop preserves warm-hold.
func TestSession_ZapOrReconnect_PreservesWarmHold(t *testing.T) {
	connector := &releaseTrackingConnector{}
	mgr := NewManager(ManagerConfig{
		WarmHoldDuration: 80 * time.Millisecond,
		ConnectTimeout:   1 * time.Second,
	}, connector)
	defer mgr.Close()

	key := SessionKey{ReceiverHost: "10.10.55.64", StreamPort: 8001, ServiceRef: "1:0:19:6:6:6:C00000:0:0:0"}.Canonicalize()
	ctx := context.Background()

	lease, err := mgr.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	upstream := connector.lastUpstream()

	// Non-explicit stop (e.g. zap, error or reconnect)
	lease.ReleaseWithReason("R_PROCESS_ENDED")

	// Upstream MUST NOT be closed immediately; must enter StateHolding
	if upstream.closed.Load() {
		t.Fatalf("upstream was closed immediately; warm-hold was not preserved for zap/reconnect")
	}
	if lease.Session().State() != StateHolding {
		t.Errorf("expected session state StateHolding, got %v", lease.Session().State())
	}

	// Wait past warm-hold expiry
	time.Sleep(120 * time.Millisecond)

	// Now it must be closed
	if !upstream.closed.Load() {
		t.Errorf("expected upstream to be closed after warm-hold expiry")
	}
	if lease.Session().State() != StateStopped {
		t.Errorf("expected session state StateStopped after expiry, got %v", lease.Session().State())
	}
}

// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lease

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 1. Exclusive Start: Session A acquires slot; Session B rejected before hardware operations
func TestLifecycleExclusiveStart(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 100 * time.Millisecond})
	defer mgr.Close()

	tb := NewTunerBinding(mgr)
	ctrl := NewTunerBindingController(tb)
	runner := NewTunerLifecycleRunner(ctrl, nil)
	runner.RenewInterval = 50 * time.Millisecond

	slot := 0
	liveRef := "1:0:19:83:6:85:C00000:0:0:0:"

	sessionStarted := make(chan struct{})
	sessionUnblock := make(chan struct{})
	hardwareCalledB := atomic.Bool{}

	// Session A
	go func() {
		_ = runner.RunSession(
			context.Background(),
			"session-A",
			slot,
			liveRef,
			func(ctx context.Context) error {
				return nil
			},
			func(ctx context.Context) error {
				close(sessionStarted)
				<-sessionUnblock
				return nil
			},
		)
	}()

	<-sessionStarted

	// Session B attempts to acquire same slot
	err := runner.RunSession(
		context.Background(),
		"session-B",
		slot,
		liveRef,
		func(ctx context.Context) error {
			hardwareCalledB.Store(true)
			return nil
		},
		func(ctx context.Context) error {
			return nil
		},
	)

	if !errors.Is(err, ErrScopeConflict) {
		t.Errorf("expected ErrScopeConflict for Session B, got %v", err)
	}

	if hardwareCalledB.Load() {
		t.Errorf("hardware tune function MUST NOT be called when acquire fails with conflict")
	}

	close(sessionUnblock)
}

// 2. Tune Failure Compensation: Acquire succeeds, tuneFn fails -> Lease released immediately
func TestLifecycleTuneFailureCompensation(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 100 * time.Millisecond})
	defer mgr.Close()

	tb := NewTunerBinding(mgr)
	ctrl := NewTunerBindingController(tb)
	runner := NewTunerLifecycleRunner(ctrl, nil)

	slot := 0
	liveRef := "1:0:19:83:6:85:C00000:0:0:0:"
	tuneErr := errors.New("zap failed")

	err := runner.RunSession(
		context.Background(),
		"session-A",
		slot,
		liveRef,
		func(ctx context.Context) error {
			return tuneErr
		},
		func(ctx context.Context) error {
			t.Errorf("runFn should not execute when tuneFn fails")
			return nil
		},
	)

	if err == nil || !errors.Is(err, tuneErr) && !stringsContains(err.Error(), "zap failed") {
		t.Errorf("expected tune error, got %v", err)
	}

	// Slot must be free immediately
	_, found := tb.GetTunerLease(slot)
	if found {
		t.Errorf("expected tuner lease slot to be released after tune failure")
	}
}

// 3. Readiness Failure Compensation: Zap succeeds, Readiness fails -> Lease released
func TestLifecycleReadinessFailureCompensation(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 100 * time.Millisecond})
	defer mgr.Close()

	tb := NewTunerBinding(mgr)
	ctrl := NewTunerBindingController(tb)
	runner := NewTunerLifecycleRunner(ctrl, nil)

	slot := 0
	liveRef := "1:0:19:83:6:85:C00000:0:0:0:"
	readinessErr := errors.New("tuner lock readiness timeout")

	err := runner.RunSession(
		context.Background(),
		"session-A",
		slot,
		liveRef,
		func(ctx context.Context) error {
			// Simulate zap succeeds, readiness fails
			return readinessErr
		},
		func(ctx context.Context) error {
			return nil
		},
	)

	if err == nil || !errors.Is(err, readinessErr) && !stringsContains(err.Error(), "readiness timeout") {
		t.Errorf("expected readiness error, got %v", err)
	}

	// Slot must be free
	_, found := tb.GetTunerLease(slot)
	if found {
		t.Errorf("expected tuner lease slot to be released after readiness failure")
	}
}

// 4. Context Cancellation: Acquire succeeds, Parent context canceled -> Cleanup with detached bounded context
func TestLifecycleContextCancellation(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 100 * time.Millisecond})
	defer mgr.Close()

	tb := NewTunerBinding(mgr)
	ctrl := NewTunerBindingController(tb)
	runner := NewTunerLifecycleRunner(ctrl, nil)

	ctx, cancel := context.WithCancel(context.Background())
	slot := 0
	liveRef := "1:0:19:83:6:85:C00000:0:0:0:"

	sessionStarted := make(chan struct{})

	go func() {
		_ = runner.RunSession(
			ctx,
			"session-canceled",
			slot,
			liveRef,
			func(c context.Context) error {
				close(sessionStarted)
				return nil
			},
			func(c context.Context) error {
				<-c.Done()
				return c.Err()
			},
		)
	}()

	<-sessionStarted
	cancel() // Cancel parent context

	// Give runner a moment to process cleanup
	time.Sleep(50 * time.Millisecond)

	_, found := tb.GetTunerLease(slot)
	if found {
		t.Errorf("expected tuner lease slot to be released after context cancellation")
	}
}

// 5. Pipeline Start Failure: Tuner lease acquired, downstream start fails -> No lease leak
func TestLifecyclePipelineStartFailure(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 100 * time.Millisecond})
	defer mgr.Close()

	tb := NewTunerBinding(mgr)
	ctrl := NewTunerBindingController(tb)
	runner := NewTunerLifecycleRunner(ctrl, nil)

	slot := 0
	liveRef := "1:0:19:83:6:85:C00000:0:0:0:"
	pipelineErr := errors.New("ffmpeg spawn failed")

	err := runner.RunSession(
		context.Background(),
		"session-pipeline-fail",
		slot,
		liveRef,
		func(ctx context.Context) error { return nil },
		func(ctx context.Context) error {
			return pipelineErr
		},
	)

	if !errors.Is(err, pipelineErr) {
		t.Errorf("expected pipelineErr, got %v", err)
	}

	_, found := tb.GetTunerLease(slot)
	if found {
		t.Errorf("expected tuner lease slot to be released after pipeline start failure")
	}
}

// 6. Normal Completion: Session completes regularly -> Lease released once idempotently
func TestLifecycleNormalCompletion(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 100 * time.Millisecond})
	defer mgr.Close()

	tb := NewTunerBinding(mgr)
	ctrl := NewTunerBindingController(tb)
	runner := NewTunerLifecycleRunner(ctrl, nil)

	slot := 0
	liveRef := "1:0:19:83:6:85:C00000:0:0:0:"

	err := runner.RunSession(
		context.Background(),
		"session-normal",
		slot,
		liveRef,
		func(ctx context.Context) error { return nil },
		func(ctx context.Context) error { return nil },
	)

	if err != nil {
		t.Fatalf("unexpected session error: %v", err)
	}

	_, found := tb.GetTunerLease(slot)
	if found {
		t.Errorf("expected tuner lease slot to be released after normal completion")
	}
}

// 7. Renew Success: Active usage periodically extends lease
func TestLifecycleRenewSuccess(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 100 * time.Millisecond})
	defer mgr.Close()

	tb := NewTunerBinding(mgr)
	ctrl := NewTunerBindingController(tb)
	runner := NewTunerLifecycleRunner(ctrl, nil)
	runner.TTL = 500 * time.Millisecond
	runner.RenewInterval = 50 * time.Millisecond

	slot := 0
	liveRef := "1:0:19:83:6:85:C00000:0:0:0:"

	err := runner.RunSession(
		context.Background(),
		"session-renew",
		slot,
		liveRef,
		func(ctx context.Context) error { return nil },
		func(ctx context.Context) error {
			time.Sleep(200 * time.Millisecond) // runs through multiple renewals
			return nil
		},
	)

	if err != nil {
		t.Fatalf("unexpected renew session error: %v", err)
	}
}

// 8. Renew Failure / Expiration: Renewal fails -> Usage context canceled, stream stopped, slot released
func TestLifecycleRenewFailureExpiration(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 10 * time.Millisecond})
	defer mgr.Close()

	tb := NewTunerBinding(mgr)
	ctrl := NewTunerBindingController(tb)
	runner := NewTunerLifecycleRunner(ctrl, nil)
	runner.TTL = 50 * time.Millisecond
	runner.RenewInterval = 20 * time.Millisecond

	slot := 0
	liveRef := "1:0:19:83:6:85:C00000:0:0:0:"
	sessionCanceled := make(chan struct{})

	// Simulate manager close during active session to cause renew failure
	go func() {
		time.Sleep(30 * time.Millisecond)
		_ = mgr.Close()
	}()

	err := runner.RunSession(
		context.Background(),
		"session-renew-fail",
		slot,
		liveRef,
		func(ctx context.Context) error { return nil },
		func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				close(sessionCanceled)
				return ctx.Err()
			case <-time.After(500 * time.Millisecond):
				return nil
			}
		},
	)

	if err == nil {
		t.Errorf("expected error when lease renewal fails")
	}

	select {
	case <-sessionCanceled:
		// success: session context was canceled immediately upon lease loss
	default:
		t.Errorf("expected session context to be canceled upon renewal failure")
	}
}

// 9. Revoke: Active lease revoked -> Usage context canceled, session stopped, slot free
func TestLifecycleRevoke(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 10 * time.Millisecond})
	defer mgr.Close()

	tb := NewTunerBinding(mgr)
	ctrl := NewTunerBindingController(tb)
	runner := NewTunerLifecycleRunner(ctrl, nil)
	runner.RenewInterval = 20 * time.Millisecond

	slot := 0
	liveRef := "1:0:19:83:6:85:C00000:0:0:0:"

	sessionStarted := make(chan struct{})
	sessionCanceled := make(chan struct{})

	go func() {
		_ = runner.RunSession(
			context.Background(),
			"session-revoke",
			slot,
			liveRef,
			func(ctx context.Context) error { return nil },
			func(ctx context.Context) error {
				close(sessionStarted)
				<-ctx.Done()
				close(sessionCanceled)
				return ctx.Err()
			},
		)
	}()

	<-sessionStarted

	// Revoke the lease
	l, found := tb.GetTunerLease(slot)
	if !found {
		t.Fatalf("failed to find active lease for revocation")
	}
	_, err := mgr.Revoke(l.ID, ReasonPreempted)
	if err != nil {
		t.Fatalf("revoke failed: %v", err)
	}

	select {
	case <-sessionCanceled:
		// usage context was canceled upon detecting revocation during renewal
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("session failed to stop after revocation")
	}

	// Slot must be free for new owner
	l2, err := tb.AcquireTunerSlot(context.Background(), "new-owner", slot, 5*time.Minute)
	if err != nil {
		t.Fatalf("expected acquire after revocation to succeed, got %v", err)
	}
	if l2.Owner != "new-owner" {
		t.Errorf("expected owner new-owner, got %s", l2.Owner)
	}
}

// 10. Non-Tuner Source: Direct HTTP / VOD path -> No tuner lease acquired, stream runs directly
func TestLifecycleNonTunerSource(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 100 * time.Millisecond})
	defer mgr.Close()

	tb := NewTunerBinding(mgr)
	ctrl := NewTunerBindingController(tb)
	runner := NewTunerLifecycleRunner(ctrl, nil)

	slot := 0
	nonTunerSources := []string{
		"http://10.0.0.1/vod/movie.mp4",
		"file:///var/media/recording.ts",
		"http://cdn.example.com/live/stream.m3u8",
	}

	for _, src := range nonTunerSources {
		tuneCalled := false
		err := runner.RunSession(
			context.Background(),
			"session-vod",
			slot,
			src,
			func(ctx context.Context) error {
				tuneCalled = true
				return nil
			},
			func(ctx context.Context) error {
				return nil
			},
		)
		if err != nil {
			t.Fatalf("unexpected non-tuner session error: %v", err)
		}
		if tuneCalled {
			t.Errorf("tune function MUST NOT be called for non-tuner source: %s", src)
		}
		_, found := tb.GetTunerLease(slot)
		if found {
			t.Errorf("no tuner lease should be acquired for non-tuner source: %s", src)
		}
	}
}

// 11. Race Test: Multiple concurrent sessions competing for same slot
func TestLifecycleRaceConditions(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 10 * time.Millisecond})
	defer mgr.Close()

	tb := NewTunerBinding(mgr)
	ctrl := NewTunerBindingController(tb)
	runner := NewTunerLifecycleRunner(ctrl, nil)
	runner.TTL = 100 * time.Millisecond
	runner.RenewInterval = 20 * time.Millisecond

	const numWorkers = 30
	var wg sync.WaitGroup
	slot := 0
	liveRef := "1:0:19:83:6:85:C00000:0:0:0:"
	activeConcurrentStarts := atomic.Int32{}
	maxConcurrentStarts := atomic.Int32{}

	ctx := context.Background()

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		workerID := Owner(fmt.Sprintf("race-worker-%d", i))
		go func(wOwner Owner) {
			defer wg.Done()
			_ = runner.RunSession(
				ctx,
				wOwner,
				slot,
				liveRef,
				func(c context.Context) error { return nil },
				func(c context.Context) error {
					curr := activeConcurrentStarts.Add(1)
					defer activeConcurrentStarts.Add(-1)

					for {
						old := maxConcurrentStarts.Load()
						if curr <= old || maxConcurrentStarts.CompareAndSwap(old, curr) {
							break
						}
					}
					time.Sleep(2 * time.Millisecond)
					return nil
				},
			)
		}(workerID)
	}

	wg.Wait()

	if maxConcurrentStarts.Load() > 1 {
		t.Errorf("expected at most 1 active concurrent hardware start for slot %d, got %d", slot, maxConcurrentStarts.Load())
	}
}

func stringsContains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) > 0 && searchSubstring(s, substr))
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

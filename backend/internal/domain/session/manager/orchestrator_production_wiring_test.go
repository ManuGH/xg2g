// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package manager

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	pipelineLease "github.com/ManuGH/xg2g/internal/pipeline/lease"
)

// ThreadSafeFakeMediaPipeline implements ports.MediaPipeline with thread-safe tracking.
type ThreadSafeFakeMediaPipeline struct {
	mu          sync.Mutex
	started     []ports.RunHandle
	stopped     []ports.RunHandle
	startCalled chan struct{}
}

func NewThreadSafeFakeMediaPipeline() *ThreadSafeFakeMediaPipeline {
	return &ThreadSafeFakeMediaPipeline{
		startCalled: make(chan struct{}, 10),
	}
}

func (f *ThreadSafeFakeMediaPipeline) Start(ctx context.Context, spec ports.StreamSpec) (ports.RunHandle, error) {
	f.mu.Lock()
	h := ports.RunHandle("handle-" + spec.SessionID)
	f.started = append(f.started, h)
	f.mu.Unlock()
	f.startCalled <- struct{}{}
	return h, nil
}

func (f *ThreadSafeFakeMediaPipeline) Stop(ctx context.Context, handle ports.RunHandle) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = append(f.stopped, handle)
	return nil
}

func (f *ThreadSafeFakeMediaPipeline) Drain(ctx context.Context, handle ports.RunHandle) error {
	return nil
}

func (f *ThreadSafeFakeMediaPipeline) Health(ctx context.Context, handle ports.RunHandle) ports.HealthStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, h := range f.stopped {
		if h == handle {
			return ports.HealthStatus{Healthy: false}
		}
	}
	return ports.HealthStatus{Healthy: true}
}

func (f *ThreadSafeFakeMediaPipeline) StartedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.started)
}

func (f *ThreadSafeFakeMediaPipeline) StartedHandles() []ports.RunHandle {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]ports.RunHandle, len(f.started))
	copy(cp, f.started)
	return cp
}

func (f *ThreadSafeFakeMediaPipeline) StoppedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.stopped)
}

func (f *ThreadSafeFakeMediaPipeline) StoppedHandles() []ports.RunHandle {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]ports.RunHandle, len(f.stopped))
	copy(cp, f.stopped)
	return cp
}

func receiveOrFail[T any](t *testing.T, ctx context.Context, ch <-chan T, what string) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-ctx.Done():
		t.Fatalf("timed out waiting for %s: %v", what, ctx.Err())
		var zero T
		return zero
	}
}

// TestProductionWiring_SingleSourceOfTruth verifies that Orchestrator uses SessionStoreTunerLeaseController
// on o.Store, enforcing a single source of truth for tuner leases.
func TestProductionWiring_SingleSourceOfTruth(t *testing.T) {
	memStore := store.NewMemoryStore()

	orch := &Orchestrator{
		Store:          memStore,
		Owner:          "worker-node-1",
		TunerSlots:     []int{0},
		LeaseTTL:       10 * time.Minute,
		HeartbeatEvery: 50 * time.Millisecond,
		LeaseKeyFunc: func(e model.StartSessionEvent) string {
			return model.LeaseKeyService(e.ServiceRef)
		},
	}

	ctx := context.Background()

	// 1. Session 1 acquires tuner slot 0 via Orchestrator
	slot1, l1, handle1, ok1, err := orch.acquireTunerLease(ctx, orch.TunerSlots, "session-1")
	if err != nil || !ok1 {
		t.Fatalf("first acquire failed: err=%v, ok=%v", err, ok1)
	}
	if slot1 != 0 || handle1 == nil {
		t.Errorf("expected slot 0 and non-nil handle, got slot=%d, handle=%v", slot1, handle1)
	}
	if string(l1.Key()) != "tuner:0" {
		t.Errorf("expected lease key tuner:0, got %s", l1.Key())
	}

	// 2. Verify SessionStoreTunerLeaseController directly against the SAME store sees slot 0 busy
	controller := pipelineLease.NewSessionStoreTunerLeaseController(memStore)
	_, err = controller.Acquire(ctx, "session-2", 0, 10*time.Minute)
	if !errors.Is(err, pipelineLease.ErrScopeConflict) {
		t.Errorf("expected ErrScopeConflict from controller on occupied slot 0, got %v", err)
	}

	// 3. Release lease via controller using the single authoritative handle
	if err := controller.Release(ctx, handle1, pipelineLease.ReasonReleasedByOwner); err != nil {
		t.Fatalf("release via controller failed: %v", err)
	}

	// 4. Now second session can acquire slot 0 via Orchestrator
	slot2, _, handle2, ok2, err := orch.acquireTunerLease(ctx, orch.TunerSlots, "session-2")
	if err != nil || !ok2 {
		t.Fatalf("acquire after release failed: err=%v, ok=%v", err, ok2)
	}
	if slot2 != 0 || handle2 == nil {
		t.Errorf("expected slot 0 and non-nil handle, got slot=%d, handle=%v", slot2, handle2)
	}
}

// TestProductionWiring_TunerLeaseLossStopsPipeline proves end-to-end that when a tuner lease is lost
// in the production Store, Orchestrator stops the real MediaPipeline RunHandle, marks session terminal
// with RLeaseExpired, and frees the slot for subsequent sessions.
func TestProductionWiring_TunerLeaseLossStopsPipeline(t *testing.T) {
	memStore := store.NewMemoryStore()
	fakePipeline := NewThreadSafeFakeMediaPipeline()

	orch := &Orchestrator{
		Store:               memStore,
		Pipeline:            fakePipeline,
		Platform:            NewStubPlatform(),
		Owner:               "worker-node-1",
		TunerSlots:          []int{0},
		LeaseTTL:            100 * time.Millisecond,
		HeartbeatEvery:      20 * time.Millisecond,
		PipelineStopTimeout: 1 * time.Second,
		StartConcurrency:    1,
		StopConcurrency:     1,
		LeaseKeyFunc: func(e model.StartSessionEvent) string {
			return model.LeaseKeyService(e.ServiceRef)
		},
	}

	ctx := context.Background()
	sessID := "sid-tuner-loss"
	serviceRef := "1:0:19:83:6:85:C00000:0:0:0:"

	record := &model.SessionRecord{
		SessionID:  sessID,
		ServiceRef: serviceRef,
		State:      model.SessionNew,
	}
	if err := memStore.PutSession(ctx, record); err != nil {
		t.Fatalf("failed to put session record: %v", err)
	}

	testCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	startErrCh := make(chan error, 1)
	go func() {
		startErrCh <- orch.handleStart(ctx, model.StartSessionEvent{
			SessionID:  sessID,
			ServiceRef: serviceRef,
			ProfileID:  "p1",
		})
	}()

	// 1. Wait until FakeMediaPipeline.Start() executes (channel receive with bounded timeout helper)
	receiveOrFail(t, testCtx, fakePipeline.startCalled, "pipeline start")

	if fakePipeline.StartedCount() != 1 {
		t.Fatalf("expected exactly 1 pipeline start, got %d", fakePipeline.StartedCount())
	}
	expectedRunHandle := fakePipeline.StartedHandles()[0]

	// 2. Revoke / release the tuner lease in the store externally to simulate lease loss
	tunerKey := string(pipelineLease.ScopeForTunerSlot(0))
	if err := memStore.ReleaseLease(ctx, tunerKey, sessID); err != nil {
		t.Fatalf("failed to revoke tuner lease in store: %v", err)
	}

	// 3. Wait for handleStart to complete after lease loss detection and assert returning error
	startErr := receiveOrFail(t, testCtx, startErrCh, "session termination")
	if startErr == nil {
		t.Fatal("expected handleStart to return a lease-loss error, got nil")
	}

	// 4. Assert FakeMediaPipeline.Stop() was called with the exact RunHandle
	if fakePipeline.StoppedCount() != 1 {
		t.Fatalf("expected exactly 1 pipeline stop call, got %d", fakePipeline.StoppedCount())
	}
	if gotHandle := fakePipeline.StoppedHandles()[0]; gotHandle != expectedRunHandle {
		t.Fatalf("expected stopped handle %v, got %v", expectedRunHandle, gotHandle)
	}

	// 5. Assert session state in store is terminal with RLeaseExpired
	rec, err := memStore.GetSession(ctx, sessID)
	if err != nil || rec == nil {
		t.Fatalf("failed to reload session record: %v", err)
	}
	if !rec.State.IsTerminal() {
		t.Errorf("expected terminal session state, got %s", rec.State)
	}
	if rec.Reason != model.RLeaseExpired {
		t.Errorf("expected reason RLeaseExpired, got %s", rec.Reason)
	}

	// 6. Assert tuner slot 0 is acquireable again by another session
	controller := pipelineLease.NewSessionStoreTunerLeaseController(memStore)
	handle2, err := controller.Acquire(ctx, "session-2", 0, 5*time.Minute)
	if err != nil || handle2 == nil {
		t.Fatalf("expected tuner slot 0 to be acquireable by session-2 after teardown, got err=%v, handle=%v", err, handle2)
	}

	// 7. Assert no active session / worker goroutine remains in Orchestrator registry
	orch.mu.Lock()
	activeCount := len(orch.active)
	_, stillActive := orch.active[sessID]
	orch.mu.Unlock()

	if activeCount != 0 || stillActive {
		t.Fatalf("expected 0 active sessions in orchestrator registry after teardown, got count=%d, stillActive=%v", activeCount, stillActive)
	}
}

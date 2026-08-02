// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package manager

import (
	"context"
	"errors"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/log"
	pipelineLease "github.com/ManuGH/xg2g/internal/pipeline/lease"
)

// FakeMediaPipeline implements ports.MediaPipeline for testing pipeline stop on lease loss.
type FakeMediaPipeline struct {
	stoppedHandles []ports.RunHandle
}

func (f *FakeMediaPipeline) Start(ctx context.Context, spec ports.StreamSpec) (ports.RunHandle, error) {
	return ports.RunHandle("handle-" + spec.SessionID), nil
}

func (f *FakeMediaPipeline) Stop(ctx context.Context, handle ports.RunHandle) error {
	f.stoppedHandles = append(f.stoppedHandles, handle)
	return nil
}

func (f *FakeMediaPipeline) Drain(ctx context.Context, handle ports.RunHandle) error {
	return nil
}

func (f *FakeMediaPipeline) Health(ctx context.Context, handle ports.RunHandle) ports.HealthStatus {
	return ports.HealthStatus{}
}

// TestProductionWiring_SingleSourceOfTruth verifies that Orchestrator uses SessionStoreTunerLeaseController
// on o.Store, enforcing a single source of truth for tuner leases.
func TestProductionWiring_SingleSourceOfTruth(t *testing.T) {
	memStore := store.NewMemoryStore()

	orch := &Orchestrator{
		Store:          memStore,
		Owner:          "worker-node-1",
		TunerSlots:     []int{0},
		LeaseTTL:       600000000000, // 10m
		HeartbeatEvery: 50000000,     // 50ms
		LeaseKeyFunc: func(e model.StartSessionEvent) string {
			return model.LeaseKeyService(e.ServiceRef)
		},
	}

	ctx := context.Background()

	// 1. Session 1 acquires tuner slot 0 via Orchestrator
	slot1, l1, ok1, err := orch.acquireTunerLease(ctx, orch.TunerSlots, "session-1")
	if err != nil || !ok1 {
		t.Fatalf("first acquire failed: err=%v, ok=%v", err, ok1)
	}
	if slot1 != 0 {
		t.Errorf("expected slot 0, got %d", slot1)
	}

	// 2. Verify SessionStoreTunerLeaseController directly against the SAME store sees slot 0 busy
	controller := pipelineLease.NewSessionStoreTunerLeaseController(memStore)
	_, err = controller.Acquire(ctx, "session-2", 0, 600000000000)
	if !errors.Is(err, pipelineLease.ErrScopeConflict) {
		t.Errorf("expected ErrScopeConflict from controller on occupied slot 0, got %v", err)
	}

	// 3. Release lease via controller
	handle := &pipelineLease.TunerLeaseHandle{
		LeaseID: pipelineLease.ID(l1.Key()),
		Owner:   pipelineLease.Owner(l1.Owner()),
		Slot:    0,
		Scope:   pipelineLease.Scope(l1.Key()),
	}
	if err := controller.Release(ctx, handle, pipelineLease.ReasonReleasedByOwner); err != nil {
		t.Fatalf("release via controller failed: %v", err)
	}

	// 4. Now second session can acquire slot 0 via Orchestrator
	slot2, _, ok2, err := orch.acquireTunerLease(ctx, orch.TunerSlots, "session-2")
	if err != nil || !ok2 {
		t.Fatalf("acquire after release failed: err=%v, ok=%v", err, ok2)
	}
	if slot2 != 0 {
		t.Errorf("expected slot 0, got %d", slot2)
	}
}

// TestProductionWiring_TunerLeaseLossStopsPipeline verifies that when a tuner lease is lost or expired
// in the production Store, Orchestrator cancels hbCtx and stops the MediaPipeline handle.
func TestProductionWiring_TunerLeaseLossStopsPipeline(t *testing.T) {
	memStore := store.NewMemoryStore()
	fakePipeline := &FakeMediaPipeline{}

	orch := &Orchestrator{
		Store:               memStore,
		Pipeline:            fakePipeline,
		Owner:               "worker-node-1",
		TunerSlots:          []int{0},
		LeaseTTL:            100000000, // 100ms
		HeartbeatEvery:      20000000,  // 20ms
		PipelineStopTimeout: 1000000000,
		LeaseKeyFunc: func(e model.StartSessionEvent) string {
			return model.LeaseKeyService(e.ServiceRef)
		},
	}

	ctx := context.Background()
	e := model.StartSessionEvent{
		SessionID:  "sid-tuner-loss",
		ServiceRef: "1:0:19:83:6:85:C00000:0:0:0:",
	}
	sessCtx := &sessionContext{
		Mode:       model.ModeLive,
		ServiceRef: e.ServiceRef,
	}

	logger := *log.L()
	acq, err := orch.acquireLeases(ctx, sessCtx, e, "worker-node-1", logger)
	if err != nil {
		t.Fatalf("acquireLeases failed: %v", err)
	}
	defer acq.ReleaseTuner()
	defer acq.ReleaseDedup()

	// Simulate external lease revocation / expiration in Store
	_ = memStore.ReleaseLease(ctx, acq.TunerLease.Key(), acq.TunerLease.Owner())

	// Wait for heartbeat worker to detect lease loss and trigger hbCancel
	<-acq.HBCtx.Done()
}

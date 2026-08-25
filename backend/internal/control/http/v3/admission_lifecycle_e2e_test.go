// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/domain/session/manager"
	"github.com/ManuGH/xg2g/internal/domain/session/manager/testkit"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	infrabus "github.com/ManuGH/xg2g/internal/infra/bus"
	"github.com/ManuGH/xg2g/internal/infra/platform"
	"github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdmissionAndLifecycle_EndToEndReclaimSequence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	memStore := store.NewMemoryStore()
	memBus := bus.NewMemoryBus()
	cfg := &config.AppConfig{
		Engine: config.EngineConfig{
			TunerSlots: []int{0}, // 1 Tuner (slot 0)
		},
		Sessions: config.SessionsConfig{
			LeaseTTL: 1 * time.Second,
		},
	}

	// Step 1: Initial state: Admission confirms 1 Tuner available
	admState := newStoreAdmissionState(memStore, len(cfg.Engine.TunerSlots))
	snap1, err := admState.Snapshot(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, snap1.TunerSlots)
	assert.Equal(t, 0, snap1.SessionsActive)

	// Step 2: Wire the real production Orchestrator with EventBus
	pipe := testkit.NewStepperPipeline()
	orch := &manager.Orchestrator{
		Store:               memStore,
		Bus:                 infrabus.NewAdapter(memBus),
		Pipeline:            pipe,
		Platform:            platform.NewOSPlatform(),
		LeaseTTL:            1 * time.Second,
		HeartbeatEvery:      100 * time.Millisecond,
		Owner:               "test-worker-a",
		TunerSlots:          []int{0},
		StartConcurrency:    10,
		StopConcurrency:     10,
		PipelineStopTimeout: 500 * time.Millisecond,
		Sweeper: manager.SweeperConfig{
			Interval:         1 * time.Minute,
			SessionRetention: 24 * time.Hour,
		},
		LeaseKeyFunc: func(e model.StartSessionEvent) string {
			return model.LeaseKeyService(e.ServiceRef)
		},
	}

	// Subscribe an observer to EventStopSession to prove event counts explicitly
	stopObserver, err := memBus.Subscribe(ctx, string(model.EventStopSession))
	require.NoError(t, err)
	defer func() { _ = stopObserver.Close() }()

	orchCtx, cancelOrch := context.WithCancel(ctx)
	defer cancelOrch()
	go func() {
		_ = orch.Run(orchCtx)
	}()

	// Wait for Orchestrator to acquire guard lock and finish subscriber setup
	require.Eventually(t, func() bool {
		l, ok, err := memStore.GetLease(ctx, "system:orchestrator:guard_lock")
		return err == nil && ok && l != nil && l.Owner() == "test-worker-a"
	}, 3*time.Second, 20*time.Millisecond, "expected Orchestrator to acquire guard lock and initialize subscriptions")

	// Step 3: Client A starts stream via real EventBus
	sessIDA := "sess-client-a"
	refA := "1:0:19:283D:3FB:1:C00000:0:0:0:"
	require.NoError(t, memStore.PutSession(ctx, &model.SessionRecord{
		SessionID:  sessIDA,
		ServiceRef: refA,
		State:      model.SessionNew,
	}))

	require.NoError(t, memBus.Publish(ctx, string(model.EventStartSession), model.StartSessionEvent{
		SessionID:  sessIDA,
		ServiceRef: refA,
		ProfileID:  "p1",
	}))

	select {
	case <-pipe.StartCalled():
		pipe.AllowStart()
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Client A pipeline start")
	}

	// Verify Client A is actively holding tuner:0 lease and is READY
	require.Eventually(t, func() bool {
		s, err := memStore.GetSession(ctx, sessIDA)
		if err != nil || s == nil {
			return false
		}
		l, ok, _ := memStore.GetLease(ctx, "tuner:0")
		return s.State == model.SessionReady && ok && l != nil && l.Owner() == sessIDA
	}, 3*time.Second, 50*time.Millisecond, "expected Client A to reach SessionReady and acquire tuner:0")

	// Step 4: Admission confirms 0 Tuners available (occupied by Client A)
	snap2, err := admState.Snapshot(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, snap2.TunerSlots, "expected 0 tuners free while Client A is active")
	assert.Equal(t, 1, snap2.SessionsActive)

	// Step 5: Wait for Client A's lease to expire
	time.Sleep(1200 * time.Millisecond)

	// Step 6: Real LeaseExpiryWorker executes reaper pass
	// Worker queries store -> detects expired lease -> publishes StopSessionEvent to real EventBus
	worker := &manager.LeaseExpiryWorker{
		Store:  memStore,
		Bus:    infrabus.NewAdapter(memBus),
		Config: cfg,
	}
	worker.ExpireStaleSessions(ctx)

	// Verify the real reaper published exactly 1 StopSessionEvent on the event bus
	select {
	case msg := <-stopObserver.C():
		evt, ok := msg.(model.StopSessionEvent)
		require.True(t, ok)
		assert.Equal(t, sessIDA, evt.SessionID)
		assert.Equal(t, model.RLeaseExpired, evt.Reason)
	case <-time.After(2 * time.Second):
		t.Fatal("expected StopSessionEvent to be received by stopObserver")
	}

	// Step 7: Real Orchestrator processes StopSessionEvent -> cancels pipeline -> ForceReleaseLeases -> sets STOPPED
	require.Eventually(t, func() bool {
		s, err := memStore.GetSession(ctx, sessIDA)
		if err != nil || s == nil {
			return false
		}
		_, hasTunerLease, _ := memStore.GetLease(ctx, "tuner:0")
		return s.State == model.SessionStopped && !hasTunerLease
	}, 3*time.Second, 50*time.Millisecond, "expected Orchestrator to transition session to STOPPED and release tuner:0 lease")

	// Step 8: Idempotency check: Second reaper sweep must produce zero additional stops or bus events
	worker.ExpireStaleSessions(ctx)
	select {
	case msg := <-stopObserver.C():
		t.Fatalf("unexpected extra stop event published during second reaper pass: %v", msg)
	default:
		// 0 extra events emitted on bus
	}
	sessAAfter, err := memStore.GetSession(ctx, sessIDA)
	require.NoError(t, err)
	assert.Equal(t, model.SessionStopped, sessAAfter.State)

	// Step 9: Admission confirms 1 Tuner is available again
	snap3, err := admState.Snapshot(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, snap3.TunerSlots, "expected 1 tuner available after reaper & orchestrator reclaim")
	assert.Equal(t, 0, snap3.SessionsActive)

	// Step 10: Client B starts stream -> Successfully acquires newly freed tuner:0
	sessIDB := "sess-client-b"
	refB := "1:0:19:283E:3FB:1:C00000:0:0:0:"
	require.NoError(t, memStore.PutSession(ctx, &model.SessionRecord{
		SessionID:  sessIDB,
		ServiceRef: refB,
		State:      model.SessionNew,
	}))

	pipeB := testkit.NewStepperPipeline()
	orch.Pipeline = pipeB // reuse running orchestrator for Client B

	require.NoError(t, memBus.Publish(ctx, string(model.EventStartSession), model.StartSessionEvent{
		SessionID:  sessIDB,
		ServiceRef: refB,
		ProfileID:  "p1",
	}))

	select {
	case <-pipeB.StartCalled():
		pipeB.AllowStart()
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Client B pipeline start")
	}

	require.Eventually(t, func() bool {
		s, err := memStore.GetSession(ctx, sessIDB)
		if err != nil || s == nil {
			return false
		}
		l, ok, _ := memStore.GetLease(ctx, "tuner:0")
		return s.State == model.SessionReady && ok && l != nil && l.Owner() == sessIDB
	}, 3*time.Second, 50*time.Millisecond, "expected Client B to acquire tuner:0 and reach SessionReady")

	// Step 11: Admission confirms 0 Tuners available (occupied by Client B)
	snap4, err := admState.Snapshot(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, snap4.TunerSlots)
	assert.Equal(t, 1, snap4.SessionsActive)
}

// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package worker

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/v3/bus"
	"github.com/ManuGH/xg2g/internal/v3/exec"
	"github.com/ManuGH/xg2g/internal/v3/model"
	"github.com/ManuGH/xg2g/internal/v3/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestContention_Blocked verifies that with 1 slot, a second session cannot start
// even if it has a different ServiceRef.
func TestContention_Blocked(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	st := store.NewMemoryStore()
	b := bus.NewMemoryBus()
	orch := &Orchestrator{
		Store:          st,
		Bus:            b,
		LeaseTTL:       5 * time.Second,
		HeartbeatEvery: 1 * time.Second,
		Owner:          "worker-1",
		TunerSlots:     []int{0}, // Only 1 slot
		ExecFactory:    &exec.StubFactory{},
		LeaseKeyFunc: func(e model.StartSessionEvent) string {
			return LeaseKeyService(e.ServiceRef)
		},
	}

	// 1. Start Session A
	sessA := "session-A"
	refA := "ref:A"
	require.NoError(t, st.PutSession(ctx, &model.SessionRecord{SessionID: sessA, ServiceRef: refA, State: model.SessionNew}))

	// Run A in background
	go func() {
		_ = orch.handleStart(ctx, model.StartSessionEvent{SessionID: sessA, ServiceRef: refA, ProfileID: "p1"})
	}()

	// Wait for A to acquire slot and reach READY
	require.Eventually(t, func() bool {
		s, err := st.GetSession(ctx, sessA)
		return err == nil && s.State == model.SessionReady
	}, 1*time.Second, 10*time.Millisecond)

	// Verify A holds tuner:0
	// (We can't easily query locks in MemoryStore without Debug, but we know it's READY)

	// 2. Start Session B (Different Ref)
	sessB := "session-B"
	refB := "ref:B" // Different service, should NOT block on dedup lease
	require.NoError(t, st.PutSession(ctx, &model.SessionRecord{SessionID: sessB, ServiceRef: refB, State: model.SessionNew}))

	// Attempt Start B
	// It should fail to acquire Tuner Lease
	err := orch.handleStart(ctx, model.StartSessionEvent{SessionID: sessB, ServiceRef: refB, ProfileID: "p1"})

	// Expect nil error (it just returns) but Session B stays in NEW
	assert.NoError(t, err)

	sB, err := st.GetSession(ctx, sessB)
	require.NoError(t, err)
	assert.Equal(t, model.SessionStopped, sB.State, "Session B should be STOPPED due to Tuner contention")
}

// TestRecovery_StaleTunerLease verifies that a stale session holding a tuner slot
// is correctly recovered by probing the tuner key.
func TestRecovery_StaleTunerLease(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()
	orch := &Orchestrator{
		Store:    st,
		LeaseTTL: 100 * time.Millisecond, // Fast expiry
	}

	// Setup Stale Session with Tuner Slot 0
	sessID := "stale-tuner-sess"
	slot := 0
	st.PutSession(ctx, &model.SessionRecord{
		SessionID:  sessID,
		ServiceRef: "abc",
		State:      model.SessionStarting,
		ContextData: map[string]string{
			model.CtxKeyTunerSlot: strconv.Itoa(slot),
		},
	})

	// Setup Lease for tuner:0 (Expired)
	// We simulate it by acquiring and letting time pass, or manual injection?
	// MemoryStore checks expiry on Acquire.
	// If we DON'T acquire it, it is free.
	// If it is free, Recovery acquires it -> Reset.
	// If we acquire it and it expires, same result.
	// Let's just NOT acquire it (simulation of crash + expiry).
	// So Tuner:0 is free.

	// Run Recovery
	err := orch.recoverStaleLeases(ctx)
	require.NoError(t, err)

	// Verify Reset
	s, err := st.GetSession(ctx, sessID)
	require.NoError(t, err)
	assert.Equal(t, model.SessionNew, s.State, "Should reset to NEW")
	assert.Equal(t, "true", s.ContextData["recovered"])
}

// TestRecovery_ActiveTunerLease verifies that active tuner lease prevents recovery.
func TestRecovery_ActiveTunerLease(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()
	orch := &Orchestrator{
		Store:    st,
		LeaseTTL: 5 * time.Second,
	}

	// Setup Active Session with Tuner Slot 0
	sessID := "active-tuner-sess"
	slot := 0
	st.PutSession(ctx, &model.SessionRecord{
		SessionID:  sessID,
		ServiceRef: "abc",
		State:      model.SessionStarting,
		ContextData: map[string]string{
			model.CtxKeyTunerSlot: strconv.Itoa(slot),
		},
	})

	// Acquire Tuner Lease (Active)
	key := LeaseKeyTunerSlot(slot)
	_, _, err := st.TryAcquireLease(ctx, key, "worker-active", 5*time.Second)
	require.NoError(t, err)

	// Run Recovery
	err = orch.recoverStaleLeases(ctx)
	require.NoError(t, err)

	// Verify NO Reset
	s, err := st.GetSession(ctx, sessID)
	require.NoError(t, err)
	assert.Equal(t, model.SessionStarting, s.State, "Should remain STARTING")
}

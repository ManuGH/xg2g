// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package manager

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/manager/testkit"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContention_Blocked(t *testing.T) {
	ctx := context.Background()

	st := store.NewMemoryStore()
	b := NewStubBus()
	pipe := testkit.NewStepperPipeline()
	orch := &Orchestrator{
		Store: st,
		Bus:   b,
		// Deterministic setup: no heartbeat ticker, and large TTL to avoid expiry paths.
		LeaseTTL:            24 * time.Hour,
		HeartbeatEvery:      0,
		Owner:               "worker-1",
		TunerSlots:          []int{0},
		Pipeline:            pipe,
		Platform:            NewStubPlatform(),
		PipelineStopTimeout: 0,
		LeaseKeyFunc: func(e model.StartSessionEvent) string {
			return model.LeaseKeyService(e.ServiceRef)
		},
	}

	sessA := "session-A"
	refA := "ref:A"
	require.NoError(t, st.PutSession(ctx, &model.SessionRecord{SessionID: sessA, ServiceRef: refA, State: model.SessionNew}))

	errCh := make(chan error, 1)
	go func() {
		errCh <- orch.handleStart(ctx, model.StartSessionEvent{SessionID: sessA, ServiceRef: refA, ProfileID: "p1"})
	}()

	<-pipe.StartCalled()

	lease, ok, err := st.GetLease(ctx, model.LeaseKeyTunerSlot(0))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, sessA, lease.Owner())

	sA, err := st.GetSession(ctx, sessA)
	require.NoError(t, err)
	assert.Equal(t, model.SessionStarting, sA.State)

	sessB := "session-B"
	refB := "ref:B" // Different service, should NOT block on dedup lease
	require.NoError(t, st.PutSession(ctx, &model.SessionRecord{SessionID: sessB, ServiceRef: refB, State: model.SessionNew}))

	err = orch.handleStart(ctx, model.StartSessionEvent{SessionID: sessB, ServiceRef: refB, ProfileID: "p1"})

	assert.ErrorIs(t, err, ErrAdmissionRejected)

	sB, err := st.GetSession(ctx, sessB)
	require.NoError(t, err)
	assert.Equal(t, model.SessionFailed, sB.State, "Session B should fail due to tuner contention")
	assert.Equal(t, model.RLeaseBusy, sB.Reason, "Session B should report lease busy reason")

	pipe.AllowStart()
	pipe.SetHealthy(false)
	_ = <-pipe.StartReturned()
	_ = <-errCh

	sA, err = st.GetSession(ctx, sessA)
	require.NoError(t, err)
	assert.True(t, sA.State.IsTerminal())
	assert.Equal(t, model.SessionFailed, sA.State)
	assert.Equal(t, model.RProcessEnded, sA.Reason)

	_, ok, err = st.GetLease(ctx, model.LeaseKeyTunerSlot(0))
	require.NoError(t, err)
	assert.False(t, ok)
	_, ok, err = st.GetLease(ctx, model.LeaseKeyService(refA))
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestRecovery_StaleTunerLease(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()
	orch := &Orchestrator{
		Store:     st,
		LeaseTTL:  24 * time.Hour,
	}

	sessID := "stale-tuner-sess"
	slot := 0
	_ = st.PutSession(ctx, &model.SessionRecord{
		SessionID:  sessID,
		ServiceRef: "abc",
		State:      model.SessionStarting,
		ContextData: map[string]string{
			model.CtxKeyTunerSlot: strconv.Itoa(slot),
		},
	})

	err := orch.recoverStaleLeases(ctx)
	require.NoError(t, err)

	s, err := st.GetSession(ctx, sessID)
	require.NoError(t, err)
	assert.Equal(t, model.SessionNew, s.State, "Should reset to NEW")
	assert.Equal(t, "true", s.ContextData["recovered"])
}

func TestRecovery_ActiveTunerLease(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()
	orch := &Orchestrator{
		Store:     st,
		LeaseTTL:  24 * time.Hour,
	}

	sessID := "active-tuner-sess"
	slot := 0
	_ = st.PutSession(ctx, &model.SessionRecord{
		SessionID:  sessID,
		ServiceRef: "abc",
		State:      model.SessionStarting,
		ContextData: map[string]string{
			model.CtxKeyTunerSlot: strconv.Itoa(slot),
		},
	})

	key := model.LeaseKeyTunerSlot(slot) // Using Model Key
	_, _, err := st.TryAcquireLease(ctx, key, "worker-active", 5*time.Second)
	require.NoError(t, err)

	err = orch.recoverStaleLeases(ctx)
	require.NoError(t, err)

	s, err := st.GetSession(ctx, sessID)
	require.NoError(t, err)
	assert.Equal(t, model.SessionStarting, s.State, "Should remain STARTING")
}

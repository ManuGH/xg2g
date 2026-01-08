// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package manager

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/infrastructure/media/stub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContention_Blocked(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	st := store.NewMemoryStore()
	b := NewStubBus()
	orch := &Orchestrator{
		Store:          st,
		Bus:            b,
		LeaseTTL:       5 * time.Second,
		HeartbeatEvery: 1 * time.Second,
		Owner:          "worker-1",
		TunerSlots:     []int{0},
		Pipeline:       stub.NewAdapter(),
		Platform:         NewStubPlatform(),
		LeaseKeyFunc: func(e model.StartSessionEvent) string {
			return model.LeaseKeyService(e.ServiceRef)
		},
	}

	sessA := "session-A"
	refA := "ref:A"
	require.NoError(t, st.PutSession(ctx, &model.SessionRecord{SessionID: sessA, ServiceRef: refA, State: model.SessionNew}))

	go func() {
		_ = orch.handleStart(ctx, model.StartSessionEvent{SessionID: sessA, ServiceRef: refA, ProfileID: "p1"})
	}()

	require.Eventually(t, func() bool {
		s, err := st.GetSession(ctx, sessA)
		return err == nil && s.State == model.SessionReady
	}, 1*time.Second, 10*time.Millisecond)

	sessB := "session-B"
	refB := "ref:B" // Different service, should NOT block on dedup lease
	require.NoError(t, st.PutSession(ctx, &model.SessionRecord{SessionID: sessB, ServiceRef: refB, State: model.SessionNew}))

	err := orch.handleStart(ctx, model.StartSessionEvent{SessionID: sessB, ServiceRef: refB, ProfileID: "p1"})

	assert.Error(t, err)

	sB, err := st.GetSession(ctx, sessB)
	require.NoError(t, err)
	assert.Equal(t, model.SessionFailed, sB.State, "Session B should fail due to tuner contention")
	assert.Equal(t, model.RLeaseBusy, sB.Reason, "Session B should report lease busy reason")
}

func TestRecovery_StaleTunerLease(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()
	orch := &Orchestrator{
		Store:    st,
		LeaseTTL: 100 * time.Millisecond,
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
		Store:    st,
		LeaseTTL: 5 * time.Second,
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

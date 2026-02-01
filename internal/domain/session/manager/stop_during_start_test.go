// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package manager

import (
	"context"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/manager/testkit"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStopDuringStart(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()
	pipe := testkit.NewStepperPipeline()

	orch := &Orchestrator{
		Store:               st,
		LeaseTTL:            24 * time.Hour,
		HeartbeatEvery:      0,
		Owner:               "worker-stop-start",
		TunerSlots:          []int{0},
		Pipeline:            pipe,
		Platform:            NewStubPlatform(),
		PipelineStopTimeout: 10 * time.Second,
		LeaseKeyFunc: func(e model.StartSessionEvent) string {
			return model.LeaseKeyService(e.ServiceRef)
		},
	}

	sessID := "session-stop-start"
	serviceRef := "ref:stop-start"
	require.NoError(t, st.PutSession(ctx, &model.SessionRecord{
		SessionID:  sessID,
		ServiceRef: serviceRef,
		State:      model.SessionNew,
	}))

	errCh := make(chan error, 1)
	go func() {
		errCh <- orch.handleStart(ctx, model.StartSessionEvent{SessionID: sessID, ServiceRef: serviceRef, ProfileID: "p1"})
	}()

	<-pipe.StartCalled()

	lease, ok, err := st.GetLease(ctx, model.LeaseKeyTunerSlot(0))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, sessID, lease.Owner())

	pipe.AllowStart()
	err = <-pipe.StartReturned()
	require.NoError(t, err)

	stopErr := orch.handleStop(ctx, model.StopSessionEvent{SessionID: sessID, Reason: model.RClientStop})
	require.NoError(t, stopErr)

	err = <-errCh
	assert.ErrorIs(t, err, ErrSessionCanceled)

	s, err := st.GetSession(ctx, sessID)
	require.NoError(t, err)
	assert.Equal(t, model.SessionStopped, s.State)
	assert.Equal(t, model.RClientStop, s.Reason)
	assert.Equal(t, model.DNone, s.ReasonDetailCode)

	<-pipe.StopCalled()
	assert.Equal(t, int32(1), pipe.StopCount())

	_, ok, err = st.GetLease(ctx, model.LeaseKeyTunerSlot(0))
	require.NoError(t, err)
	assert.False(t, ok)
	_, ok, err = st.GetLease(ctx, model.LeaseKeyService(serviceRef))
	require.NoError(t, err)
	assert.False(t, ok)

	orch.mu.Lock()
	activeCount := len(orch.active)
	orch.mu.Unlock()
	assert.Equal(t, 0, activeCount)
}

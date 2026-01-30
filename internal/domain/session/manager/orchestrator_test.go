// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

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

func TestOrchestrator_HandleStart_StubExecution(t *testing.T) {
	// Setup
	ctx := context.Background()
	st := store.NewMemoryStore()
	pipe := testkit.NewStepperPipeline()

	orch := &Orchestrator{
		Store:               st,
		LeaseTTL:            24 * time.Hour,
		HeartbeatEvery:      0,
		Owner:               "test-worker-1",
		TunerSlots:          []int{0},
		Admission:           testkit.NewAdmissibleAdmission(),
		Pipeline:            pipe,
		Platform:            NewStubPlatform(),
		PipelineStopTimeout: 0,
		LeaseKeyFunc: func(e model.StartSessionEvent) string {
			return model.LeaseKeyService(e.ServiceRef)
		},
	}

	// Prepare Session
	sessionID := "sess-1"
	serviceRef := "abc:123"
	require.NoError(t, st.PutSession(ctx, &model.SessionRecord{
		SessionID:  sessionID,
		ServiceRef: serviceRef,
		State:      model.SessionNew,
	}))

	// Prepare Event
	evt := model.StartSessionEvent{
		SessionID:  sessionID,
		ServiceRef: serviceRef,
		ProfileID:  "hd",
	}

	execCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error)
	go func() {
		errCh <- orch.handleStart(execCtx, evt)
	}()

	<-pipe.StartCalled()
	cancel()
	pipe.AllowStart()
	_ = <-pipe.StartReturned()

	err := <-errCh
	assert.ErrorIs(t, err, ErrSessionCanceled, "Should exit with session canceled")

	s, err := st.GetSession(ctx, sessionID)
	require.NoError(t, err)
	assert.Equal(t, model.SessionCancelled, s.State)
	assert.Equal(t, model.RCancelled, s.Reason)
	assert.Equal(t, model.DContextCanceled, s.ReasonDetailCode)
}

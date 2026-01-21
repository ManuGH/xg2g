// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package manager

import (
	"context"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/admission"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/infra/media/stub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrchestrator_HandleStart_StubExecution(t *testing.T) {
	// Setup
	ctx := context.Background()
	st := store.NewMemoryStore()
	memBus := NewStubBus()

	orch := &Orchestrator{
		Bus:            memBus,
		Store:          st,
		LeaseTTL:       5 * time.Second,
		HeartbeatEvery: 1 * time.Second,
		Owner:          "test-worker-1",
		TunerSlots:     []int{0},
		Admission:      admission.NewResourceMonitor(10, 10, 0),
		Pipeline:       stub.NewAdapter(),
		Platform:       NewStubPlatform(),
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

	assert.Eventually(t, func() bool {
		s, err := st.GetSession(ctx, sessionID)
		if err != nil {
			return false
		}
		return s.State == model.SessionReady
	}, 2*time.Second, 50*time.Millisecond, "Session should reach READY state")

	cancel()

	err := <-errCh
	assert.ErrorIs(t, err, context.Canceled, "Should exit with context canceled")
}

// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package worker

import (
	"context"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/ManuGH/xg2g/internal/pipeline/exec"
	"github.com/ManuGH/xg2g/internal/pipeline/model"
	"github.com/ManuGH/xg2g/internal/pipeline/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrchestrator_HandleStart_StubExecution(t *testing.T) {
	// Setup
	ctx := context.Background()
	st := store.NewMemoryStore()
	bus := bus.NewMemoryBus()

	// Create Orchestrator with StubFactory explicitly (or rely on default in Run,
	// but here we instantiate struct directly for test, so need to set it)
	orch := &Orchestrator{
		Bus:            bus,
		Store:          st,
		LeaseTTL:       5 * time.Second,
		HeartbeatEvery: 1 * time.Second,
		Owner:          "test-worker-1",
		TunerSlots:     []int{0},            // Provide at least one slot
		ExecFactory:    &exec.StubFactory{}, // Use stub execution
		LeaseKeyFunc: func(e model.StartSessionEvent) string {
			return e.ServiceRef
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

	// Because handleStart blocks in Wait(), we run it in a goroutine
	// and cancel the context to stop it.
	// We expect it to reach READY state before we cancel.

	execCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error)
	go func() {
		errCh <- orch.handleStart(execCtx, evt)
	}()

	// Wait for state transition to READY
	// StubTuner takes 100ms, StubTranscoder takes 50ms (simulated).
	// So we poll for ~500ms max.
	assert.Eventually(t, func() bool {
		s, err := st.GetSession(ctx, sessionID)
		if err != nil {
			return false
		}
		return s.State == model.SessionReady
	}, 1*time.Second, 50*time.Millisecond, "Session should reach READY state")

	// Ensure Lease is held
	// We check if handleStart is still running (it blocks on Wait).

	// Now Cancel to stop execution
	cancel()

	// Verify clean exit
	err := <-errCh
	assert.ErrorIs(t, err, context.Canceled, "Should exit with context canceled")
}

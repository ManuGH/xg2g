package manager

import (
	"context"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/infra/media/stub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOrchestrator_Stop_Ready: Stop when session is READY
func TestOrchestrator_Stop_Ready(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()

	orch := &Orchestrator{
		Store:               st,
		Bus:                 NewStubBus(),
		Pipeline:            stub.NewAdapter(),
		Platform:            NewStubPlatform(),
		LeaseTTL:            5 * time.Second,
		HeartbeatEvery:      1 * time.Second,
		Owner:               "test-stop",
		TunerSlots:          []int{1},
		StartConcurrency:    100,
		StopConcurrency:     100,
		PipelineStopTimeout: 100 * time.Millisecond,
		LeaseKeyFunc:        func(e model.StartSessionEvent) string { return model.LeaseKeyTunerSlot(1) },
		Sweeper: SweeperConfig{
			Interval:         5 * time.Minute,
			SessionRetention: 24 * time.Hour,
		},
	}

	sessionID := "sess-stop-ready"

	// Seed state: session is READY
	err := st.PutSession(ctx, &model.SessionRecord{
		SessionID:     sessionID,
		ServiceRef:    "ref:1",
		State:         model.SessionReady,
		PipelineState: "", // Pipeline is running (no error state)
	})
	require.NoError(t, err)

	// Call handleStop directly (deterministic, no Run loop)
	stopEvt := model.StopSessionEvent{
		SessionID: sessionID,
		Reason:    model.RClientStop,
	}
	err = orch.handleStop(ctx, stopEvt)
	require.NoError(t, err)

	// Assert: State transitioned to terminal
	s, err := st.GetSession(ctx, sessionID)
	require.NoError(t, err)
	require.NotNil(t, s)

	assert.True(t, s.State == model.SessionStopping || s.State == model.SessionStopped || s.State == model.SessionFailed,
		"Expected stop-path state, got %s", s.State)
	assert.Equal(t, model.RClientStop, s.Reason)
}

// TestOrchestrator_Stop_Starting: Stop when session is STARTING
func TestOrchestrator_Stop_Starting(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()

	orch := &Orchestrator{
		Store:               st,
		Bus:                 NewStubBus(),
		Pipeline:            stub.NewAdapter(),
		Platform:            NewStubPlatform(),
		LeaseTTL:            5 * time.Second,
		HeartbeatEvery:      1 * time.Second,
		Owner:               "test-stop-starting",
		TunerSlots:          []int{1},
		StartConcurrency:    100,
		StopConcurrency:     100,
		PipelineStopTimeout: 100 * time.Millisecond,
		LeaseKeyFunc:        func(e model.StartSessionEvent) string { return model.LeaseKeyTunerSlot(1) },
		Sweeper: SweeperConfig{
			Interval:         5 * time.Minute,
			SessionRetention: 24 * time.Hour,
		},
	}

	sessionID := "sess-stop-starting"

	// Seed state: session is STARTING
	err := st.PutSession(ctx, &model.SessionRecord{
		SessionID:     sessionID,
		ServiceRef:    "ref:1",
		State:         model.SessionStarting,
		PipelineState: "", // Starting
	})
	require.NoError(t, err)

	// Call handleStop directly
	stopEvt := model.StopSessionEvent{
		SessionID: sessionID,
		Reason:    model.RClientStop,
	}
	err = orch.handleStop(ctx, stopEvt)
	require.NoError(t, err)

	// Assert: State transitioned to terminal
	s, err := st.GetSession(ctx, sessionID)
	require.NoError(t, err)
	require.NotNil(t, s)

	assert.True(t, s.State == model.SessionStopping || s.State == model.SessionStopped || s.State == model.SessionFailed,
		"Expected stop-path state, got %s", s.State)
	assert.Equal(t, model.RClientStop, s.Reason)
}

// TestOrchestrator_Stop_Idempotency: Stop is idempotent on terminal sessions
func TestOrchestrator_Stop_Idempotency(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()

	orch := &Orchestrator{
		Store:               st,
		Bus:                 NewStubBus(),
		Pipeline:            stub.NewAdapter(),
		Platform:            NewStubPlatform(),
		LeaseTTL:            5 * time.Second,
		HeartbeatEvery:      1 * time.Second,
		Owner:               "test-stop-idem",
		TunerSlots:          []int{1},
		StartConcurrency:    100,
		StopConcurrency:     100,
		PipelineStopTimeout: 100 * time.Millisecond,
		LeaseKeyFunc:        func(e model.StartSessionEvent) string { return model.LeaseKeyTunerSlot(1) },
		Sweeper: SweeperConfig{
			Interval:         5 * time.Minute,
			SessionRetention: 24 * time.Hour,
		},
	}

	sessionID := "sess-idempotent"

	// Seed state: session is already STOPPED
	err := st.PutSession(ctx, &model.SessionRecord{
		SessionID:     sessionID,
		ServiceRef:    "ref:i",
		State:         model.SessionStopped,
		Reason:        model.RClientStop,
		UpdatedAtUnix: time.Now().Unix(),
	})
	require.NoError(t, err)

	originalRecord, _ := st.GetSession(ctx, sessionID)

	// Call handleStop twice
	stopEvt := model.StopSessionEvent{
		SessionID: sessionID,
		Reason:    model.RClientStop, // Use same reason
	}

	_ = orch.handleStop(ctx, stopEvt)
	_ = orch.handleStop(ctx, stopEvt)

	// Assert: No regression, state remains terminal
	s, err := st.GetSession(ctx, sessionID)
	require.NoError(t, err)
	require.NotNil(t, s)

	assert.Equal(t, model.SessionStopped, s.State, "State should remain STOPPED")
	// Original reason should be preserved (idempotent - first stop wins)
	assert.Equal(t, originalRecord.Reason, s.Reason, "Original stop reason preserved")
}

// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package manager

import (
	"context"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/stretchr/testify/require"
)

// startSlotHoldingPipeline keeps every started session "healthy", so handleStart
// blocks in waitForProcessExit and holds its start-concurrency slot for the whole
// session lifetime. Each Start signals the session id on entered.
type startSlotHoldingPipeline struct {
	entered chan string
}

func (p *startSlotHoldingPipeline) Start(_ context.Context, spec ports.StreamSpec) (ports.RunHandle, error) {
	p.entered <- spec.SessionID
	return ports.RunHandle(spec.SessionID), nil
}
func (p *startSlotHoldingPipeline) Stop(context.Context, ports.RunHandle) error { return nil }
func (p *startSlotHoldingPipeline) Health(context.Context, ports.RunHandle) ports.HealthStatus {
	return ports.HealthStatus{Healthy: true}
}

// waitForEntered blocks until a session enters Start. Per this package's
// determinism contract (no time.After/Sleep/Eventually), it relies on a blocking
// channel read; if the Run loop is wedged the read never returns and `go test
// -timeout` fails the run with a goroutine dump pointing here.
func waitForEntered(t *testing.T, ch <-chan string, want string) {
	t.Helper()
	got := <-ch
	require.Equal(t, want, got, "unexpected session entered Start")
}

// TestOrchestrator_StopNotStarvedBySaturatedStartSem reproduces the control-plane
// wedge: with the single start slot held by an active session, a queued start used
// to block the Run loop on the semaphore, starving stop events — so no slot could
// ever free. The fix dispatches first and acquires the slot inside the worker, so
// the stop is still processed, frees the slot, and the queued start proceeds.
func TestOrchestrator_StopNotStarvedBySaturatedStartSem(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	st := store.NewMemoryStore()
	bus := NewStubBus()
	entered := make(chan string, 4)

	orch := &Orchestrator{
		Store:               st,
		Bus:                 bus,
		LeaseTTL:            24 * time.Hour,
		HeartbeatEvery:      24 * time.Hour,
		Owner:               "test-wedge",
		TunerSlots:          []int{0, 1, 2},
		Pipeline:            &startSlotHoldingPipeline{entered: entered},
		PipelineStopTimeout: 1 * time.Second,
		StartConcurrency:    1, // single slot — one active session saturates it
		StopConcurrency:     5,
		LeaseKeyFunc: func(e model.StartSessionEvent) string {
			return model.LeaseKeyService(e.ServiceRef)
		},
		Sweeper: SweeperConfig{
			Interval:         5 * time.Minute,
			SessionRetention: 24 * time.Hour,
		},
	}

	go func() { _ = orch.Run(ctx) }()
	bus.WaitForSubscriber(string(model.EventStartSession))
	bus.WaitForSubscriber(string(model.EventStopSession))

	start := func(id string) {
		require.NoError(t, st.PutSession(ctx, &model.SessionRecord{
			SessionID:  id,
			ServiceRef: id,
			State:      model.SessionNew,
		}))
		require.NoError(t, bus.Publish(ctx, string(model.EventStartSession), model.StartSessionEvent{
			SessionID:  id,
			ServiceRef: id,
			ProfileID:  "test",
		}))
	}

	// A takes the only slot and stays active (Health healthy => slot held).
	start("A")
	waitForEntered(t, entered, "A")

	// B is queued; it cannot get a slot until A's frees.
	start("B")

	// Stopping A must be serviced even though the start slot is saturated. If the
	// Run loop were blocked acquiring B's slot, this stop would never run.
	require.NoError(t, bus.Publish(ctx, string(model.EventStopSession), model.StopSessionEvent{
		SessionID: "A",
	}))

	// The stop frees A's slot, so B finally starts.
	waitForEntered(t, entered, "B")
}

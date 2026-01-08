package manager

import (
	"context"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/ManuGH/xg2g/internal/pipeline/exec"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrchestrator_Observability_TuneFailure(t *testing.T) {
	// Setup
	ctx := context.Background()
	st := store.NewMemoryStore()
	bus := bus.NewMemoryBus()

	// Failing Factory
	factory := &FailingFactory{
		TuneErr: context.DeadlineExceeded,
	}

	orch := &Orchestrator{
		Bus:            bus,
		Store:          st,
		LeaseTTL:       5 * time.Second,
		HeartbeatEvery: 1 * time.Second,
		Owner:          "test-worker-obs",
		TunerSlots:     []int{0},
		ExecFactory:    factory,
		LeaseKeyFunc: func(e model.StartSessionEvent) string {
			return e.ServiceRef
		},
	}

	// Capture initial metrics
	// We can use testutil.ToFloat64 to check counters
	// But first we need to ensure the metric is initialized or use delta.
	// Since we are adding to global metrics, delta is safer.

	// Run handleStart
	evt := model.StartSessionEvent{
		SessionID:  "sess-obs-1",
		ServiceRef: "ref:fail",
		ProfileID:  "hd",
	}

	err := orch.handleStart(ctx, evt)

	// Verification
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	// Check Session State -> STOPPED (Unified Finalizer)
	s, storeErr := st.GetSession(ctx, "sess-obs-1")
	require.NoError(t, storeErr)
	assert.Equal(t, model.SessionStopped, s.State)
	assert.Equal(t, model.RTuneTimeout, s.Reason)

}

type FailingFactory struct {
	exec.StubFactory
	TuneErr error
}

func (f *FailingFactory) NewTuner(slot int) (exec.Tuner, error) {
	return &FailingTuner{Err: f.TuneErr}, nil
}

type FailingTuner struct {
	exec.StubTuner
	Err error
}

func (t *FailingTuner) Tune(ctx context.Context, ref string) error {
	return t.Err
}

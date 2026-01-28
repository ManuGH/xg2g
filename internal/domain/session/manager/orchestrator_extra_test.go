package manager

import (
	"context"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrchestrator_Observability_TuneFailure(t *testing.T) {
	// Setup
	ctx := context.Background()
	st := store.NewMemoryStore()
	memBus := NewStubBus()

	// Failing Pipeline
	failPipe := &FailingPipeline{
		StartErr: context.DeadlineExceeded,
	}

	orch := &Orchestrator{
		Bus:            memBus,
		Store:          st,
		LeaseTTL:       5 * time.Second,
		HeartbeatEvery: 1 * time.Second,
		Owner:          "test-worker-obs",
		TunerSlots:     []int{0},
		Admission:      newAdmissionMonitor(10, 10, 0),
		Pipeline:       failPipe,
		LeaseKeyFunc: func(e model.StartSessionEvent) string {
			return model.LeaseKeyService(e.ServiceRef)
		},
	}

	// Run handleStart
	evt := model.StartSessionEvent{
		SessionID:  "sess-obs-1",
		ServiceRef: "ref:fail",
		ProfileID:  "hd",
	}

	require.NoError(t, st.PutSession(ctx, &model.SessionRecord{
		SessionID:  "sess-obs-1",
		ServiceRef: "ref:fail",
		State:      model.SessionNew,
	}))

	err := orch.handleStart(ctx, evt)

	assert.ErrorIs(t, err, context.DeadlineExceeded)

	s, storeErr := st.GetSession(ctx, "sess-obs-1")
	require.NoError(t, storeErr)
	assert.Equal(t, model.SessionFailed, s.State)
}

type FailingPipeline struct {
	StartErr error
}

func (f *FailingPipeline) Start(ctx context.Context, spec ports.StreamSpec) (ports.RunHandle, error) {
	return "", f.StartErr
}

func (f *FailingPipeline) Stop(ctx context.Context, handle ports.RunHandle) error {
	return nil
}

func (f *FailingPipeline) Health(ctx context.Context, handle ports.RunHandle) ports.HealthStatus {
	return ports.HealthStatus{Healthy: false}
}

func TestReconcileTuners_UniqueSlotCount(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()

	// Reset metric for test (global state)
	metrics.SetTunersInUse(0)

	orch := &Orchestrator{
		Store:      st,
		TunerSlots: []int{1}, // Slot 1
		Admission:  newAdmissionMonitor(10, 10, 0),
	}

	// 1. Create two sessions claiming slot "1" (Context Data)
	s1 := &model.SessionRecord{
		SessionID:  "s1",
		ServiceRef: "ref1",
		State:      model.SessionReady,
		ContextData: map[string]string{
			model.CtxKeyTunerSlot: "1",
		},
	}
	s2 := &model.SessionRecord{
		SessionID:  "s2",
		ServiceRef: "ref2",
		State:      model.SessionReady,
		ContextData: map[string]string{
			model.CtxKeyTunerSlot: "1",
		},
	}
	require.NoError(t, st.PutSession(ctx, s1))
	require.NoError(t, st.PutSession(ctx, s2))

	// 2. Create Lease for Slot 1 owned by s1 (Truth)
	key := model.LeaseKeyTunerSlot(1)
	_, _, err := st.TryAcquireLease(ctx, key, "s1", 10*time.Second)
	require.NoError(t, err)

	// 3. Run Reconciliation
	err = orch.reconcileTunerMetrics(ctx)
	require.NoError(t, err)

	// 4. Expect Gauge = 1 (s1 matches lease, s2 is drift/ignored/duplicate)
	val := metrics.GetTunersInUse()
	assert.Equal(t, 1.0, val, "Gauge should count unique slots only")
}

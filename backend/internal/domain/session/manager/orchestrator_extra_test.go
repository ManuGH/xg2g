package manager

import (
	"context"
	"sync/atomic"
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
		LeaseTTL:       24 * time.Hour,
		HeartbeatEvery: 0,
		Owner:          "test-worker-obs",
		TunerSlots:     []int{0},
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

	assert.ErrorIs(t, err, ErrPipelineFailure)
	assert.True(t, failPipe.StartCalled())

	s, storeErr := st.GetSession(ctx, "sess-obs-1")
	require.NoError(t, storeErr)
	assert.Equal(t, model.SessionFailed, s.State)
	assert.Equal(t, model.RTuneTimeout, s.Reason)
	assert.Equal(t, model.DDeadlineExceeded, s.ReasonDetailCode)
	require.NotNil(t, s.PlaybackTrace)
	assert.Equal(t, "compatible", s.PlaybackTrace.RequestProfile)
	assert.Equal(t, string(model.PlaybackStopClassInput), string(s.PlaybackTrace.StopClass))
	assert.Equal(t, string(model.RTuneTimeout), s.PlaybackTrace.StopReason)
	assert.Equal(t, "tuner", s.PlaybackTrace.InputKind)
	assert.NotNil(t, s.PlaybackTrace.FFmpegPlan)
}

func TestOrchestrator_Observability_PreflightFailureMapsToInput(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()
	memBus := NewStubBus()

	failPipe := &FailingPipeline{
		StartErr: ports.NewPreflightError(ports.NewPreflightResult("sync_miss", 0, 0, 0, 17999)),
	}

	orch := &Orchestrator{
		Bus:            memBus,
		Store:          st,
		LeaseTTL:       24 * time.Hour,
		HeartbeatEvery: 0,
		Owner:          "test-worker-preflight",
		TunerSlots:     []int{0},
		Pipeline:       failPipe,
		LeaseKeyFunc: func(e model.StartSessionEvent) string {
			return model.LeaseKeyService(e.ServiceRef)
		},
	}

	evt := model.StartSessionEvent{
		SessionID:  "sess-preflight-1",
		ServiceRef: "ref:preflight",
		ProfileID:  "hd",
	}

	require.NoError(t, st.PutSession(ctx, &model.SessionRecord{
		SessionID:  evt.SessionID,
		ServiceRef: evt.ServiceRef,
		State:      model.SessionNew,
	}))

	err := orch.handleStart(ctx, evt)

	assert.ErrorIs(t, err, ErrPipelineFailure)
	assert.True(t, failPipe.StartCalled())

	s, storeErr := st.GetSession(ctx, evt.SessionID)
	require.NoError(t, storeErr)
	assert.Equal(t, model.SessionFailed, s.State)
	assert.Equal(t, model.RUpstreamCorrupt, s.Reason)
	assert.Equal(t, "preflight failed invalid_ts: sync_miss", s.ReasonDetailDebug)
	require.NotNil(t, s.PlaybackTrace)
	assert.Equal(t, "invalid_ts", s.PlaybackTrace.PreflightReason)
	assert.Equal(t, "sync_miss", s.PlaybackTrace.PreflightDetail)
	assert.Equal(t, string(model.PlaybackStopClassInput), string(s.PlaybackTrace.StopClass))
	assert.Equal(t, string(model.RUpstreamCorrupt), s.PlaybackTrace.StopReason)
}

type FailingPipeline struct {
	StartErr    error
	startCalled atomic.Bool
}

func (f *FailingPipeline) Start(ctx context.Context, spec ports.StreamSpec) (ports.RunHandle, error) {
	f.startCalled.Store(true)
	return "", f.StartErr
}

func (f *FailingPipeline) Stop(ctx context.Context, handle ports.RunHandle) error {
	return nil
}

func (f *FailingPipeline) Health(ctx context.Context, handle ports.RunHandle) ports.HealthStatus {
	return ports.HealthStatus{Healthy: false}
}

func (f *FailingPipeline) StartCalled() bool {
	return f.startCalled.Load()
}

func TestReconcileTuners_UniqueSlotCount(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()

	// Reset metric for test (global state)
	metrics.SetTunersInUse(0)

	orch := &Orchestrator{
		Store:      st,
		TunerSlots: []int{1}, // Slot 1
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

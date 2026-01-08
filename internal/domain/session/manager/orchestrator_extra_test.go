package manager

import (
	"context"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
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

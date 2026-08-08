package manager

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/stretchr/testify/require"
)

// specCapturingPipeline records the StreamSpec it was started with, so the
// deadlines the orchestrator hands down can be asserted on.
type specCapturingPipeline struct {
	spec ports.StreamSpec
}

func (p *specCapturingPipeline) Start(_ context.Context, spec ports.StreamSpec) (ports.RunHandle, error) {
	p.spec = spec
	return ports.RunHandle(fmt.Sprintf("%s-handle", spec.SessionID)), nil
}

func (p *specCapturingPipeline) Stop(context.Context, ports.RunHandle) error { return nil }

func (p *specCapturingPipeline) Health(context.Context, ports.RunHandle) ports.HealthStatus {
	return ports.HealthStatus{Healthy: true}
}

func newDeadlineTestOrchestrator(t *testing.T, sid string) (*Orchestrator, *specCapturingPipeline) {
	t.Helper()
	st := store.NewMemoryStore()
	require.NoError(t, st.PutSession(context.Background(), &model.SessionRecord{
		SessionID: sid,
		State:     model.SessionStarting,
		Profile:   model.ProfileSpec{Name: "default"},
	}))
	pipeline := &specCapturingPipeline{}
	orch := fastGeometryOrchestrator()
	orch.Store = st
	orch.Pipeline = pipeline
	return orch, pipeline
}

// TestStartPipeline_HandsTheAttemptDeadlinesToTheAdapter is the plumbing the two
// budget holes are fixed through. The adapter cannot bound its own preparation or
// its startup watchdog against a budget it cannot see, so an attempt that carries
// one must pass it down.
func TestStartPipeline_HandsTheAttemptDeadlinesToTheAdapter(t *testing.T) {
	const sid = "sess-startup-deadlines"
	orch, pipeline := newDeadlineTestOrchestrator(t, sid)

	start := time.Now()
	budget := orch.newStartupBudget(start, false)
	attempt := budget.attempt(0, false)

	_, _, err := orch.startPipeline(
		context.Background(),
		model.StartSessionEvent{SessionID: sid, ServiceRef: "ref:live", ProfileID: "default"},
		&sessionContext{Mode: model.ModeLive, ServiceRef: "ref:live"},
		model.ProfileSpec{Name: "default"},
		-1,
		attempt,
	)
	require.NoError(t, err)

	require.False(t, pipeline.spec.PrepareDeadline.IsZero(), "a bounded attempt must bound the adapter's preparation")
	require.False(t, pipeline.spec.ReadyDeadline.IsZero(), "a bounded attempt must tell the adapter when waiting stops")

	// Preparation ends before the attempt does, leaving the media time behind it.
	require.True(t, pipeline.spec.PrepareDeadline.Before(pipeline.spec.ReadyDeadline),
		"preparation must end before the attempt stops being waited on")

	// And the attempt stops short of the whole budget, because the reserve behind
	// it belongs to the retry.
	attemptDeadline, bounded := attempt.deadline()
	require.True(t, bounded)
	require.WithinDuration(t, attemptDeadline, pipeline.spec.ReadyDeadline, time.Second)
	require.True(t, pipeline.spec.ReadyDeadline.Before(budget.Deadline),
		"the attempt must not claim the reserve held for the retry")
}

// TestStartPipeline_LeavesVODUnbounded keeps recordings free of live deadlines:
// an unbounded attempt must hand down zero times, not zero-valued deadlines that
// an adapter would read as "already expired".
func TestStartPipeline_LeavesVODUnbounded(t *testing.T) {
	const sid = "sess-startup-deadlines-vod"
	orch, pipeline := newDeadlineTestOrchestrator(t, sid)

	budget := orch.newStartupBudget(time.Now(), true)

	_, _, err := orch.startPipeline(
		context.Background(),
		model.StartSessionEvent{SessionID: sid, ServiceRef: "ref:live", ProfileID: "default"},
		&sessionContext{Mode: model.ModeLive, ServiceRef: "ref:live"},
		model.ProfileSpec{Name: "default"},
		-1,
		budget.attempt(0, false),
	)
	require.NoError(t, err)

	require.True(t, pipeline.spec.PrepareDeadline.IsZero(), "an unbounded attempt must not bound preparation")
	require.True(t, pipeline.spec.ReadyDeadline.IsZero(), "an unbounded attempt has no ready deadline")
}

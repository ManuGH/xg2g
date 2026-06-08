package manager

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordedStopEventBus struct {
	mu     sync.Mutex
	topics []string
	events []model.StopSessionEvent
}

func (b *recordedStopEventBus) Publish(ctx context.Context, topic string, event interface{}) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.topics = append(b.topics, topic)
	if evt, ok := event.(model.StopSessionEvent); ok {
		b.events = append(b.events, evt)
	}
	return nil
}

func TestLeaseExpiryWorker_RequestsCleanupStopForStartingSessions(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()
	now := time.Now()
	bus := &recordedStopEventBus{}

	expired := &model.SessionRecord{
		SessionID:          "sess-starting-expired",
		State:              model.SessionStarting,
		ServiceRef:         "1:0:1:1:1:1:C00000:0:0:0:",
		CorrelationID:      "corr-expired",
		CreatedAtUnix:      now.Add(-2 * time.Minute).Unix(),
		UpdatedAtUnix:      now.Add(-2 * time.Minute).Unix(),
		LeaseExpiresAtUnix: now.Add(-1 * time.Minute).Unix(),
	}
	fresh := &model.SessionRecord{
		SessionID:          "sess-starting-fresh",
		State:              model.SessionStarting,
		ServiceRef:         "1:0:1:2:2:2:C00000:0:0:0:",
		CreatedAtUnix:      now.Unix(),
		UpdatedAtUnix:      now.Unix(),
		LeaseExpiresAtUnix: now.Add(2 * time.Minute).Unix(),
	}

	require.NoError(t, st.PutSession(ctx, expired))
	require.NoError(t, st.PutSession(ctx, fresh))

	worker := &LeaseExpiryWorker{
		Store:  st,
		Bus:    bus,
		Config: &config.AppConfig{},
	}

	worker.expireStaleSessions(ctx)

	gotExpired, err := st.GetSession(ctx, expired.SessionID)
	require.NoError(t, err)
	require.NotNil(t, gotExpired)
	assert.Equal(t, model.SessionStopping, gotExpired.State)
	assert.Equal(t, model.RLeaseExpired, gotExpired.Reason)
	assert.Equal(t, model.PipeStopRequested, gotExpired.PipelineState)
	assert.Equal(t, "LEASE_EXPIRED", gotExpired.StopReason)

	gotFresh, err := st.GetSession(ctx, fresh.SessionID)
	require.NoError(t, err)
	require.NotNil(t, gotFresh)
	assert.Equal(t, model.SessionStarting, gotFresh.State)

	require.Len(t, bus.topics, 1)
	assert.Equal(t, string(model.EventStopSession), bus.topics[0])
	require.Len(t, bus.events, 1)
	assert.Equal(t, expired.SessionID, bus.events[0].SessionID)
	assert.Equal(t, model.RLeaseExpired, bus.events[0].Reason)
	assert.Equal(t, expired.CorrelationID, bus.events[0].CorrelationID)
}

func TestLeaseExpiryWorker_TerminalizesNewSessionsWithoutPublish(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()
	now := time.Now()
	bus := &recordedStopEventBus{}

	expired := &model.SessionRecord{
		SessionID:          "sess-new-expired",
		State:              model.SessionNew,
		ServiceRef:         "1:0:1:1:1:1:C00000:0:0:0:",
		CreatedAtUnix:      now.Add(-2 * time.Minute).Unix(),
		UpdatedAtUnix:      now.Add(-2 * time.Minute).Unix(),
		LeaseExpiresAtUnix: now.Add(-1 * time.Minute).Unix(),
	}

	require.NoError(t, st.PutSession(ctx, expired))

	worker := &LeaseExpiryWorker{
		Store:  st,
		Bus:    bus,
		Config: &config.AppConfig{},
	}

	worker.expireStaleSessions(ctx)

	gotExpired, err := st.GetSession(ctx, expired.SessionID)
	require.NoError(t, err)
	require.NotNil(t, gotExpired)
	assert.Equal(t, model.SessionStopped, gotExpired.State)
	assert.Equal(t, model.RLeaseExpired, gotExpired.Reason)
	assert.Equal(t, "LEASE_EXPIRED", gotExpired.StopReason)
	assert.Empty(t, bus.topics)
}

func TestLeaseExpiryWorker_NilConfigDoesNotPanic(t *testing.T) {
	// The review thread identified a potential nil-pointer dereference when
	// w.Config is nil. Verify that Run() recovers with the default interval
	// instead of panicking, and that expireStaleSessions also works without
	// a Config.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	st := store.NewMemoryStore()
	bus := &recordedStopEventBus{}

	worker := &LeaseExpiryWorker{
		Store:  st,
		Bus:    bus,
		Config: nil,
	}

	// expireStaleSessions must not panic with nil Config.
	require.NotPanics(t, func() {
		worker.expireStaleSessions(ctx)
	})

	// Run must not panic with nil Config; it should default to 10s.
	// Use an already-canceled context so Run returns immediately.
	cancelledCtx, cancel2 := context.WithCancel(context.Background())
	cancel2() // cancel immediately

	require.NotPanics(t, func() {
		err := worker.Run(cancelledCtx)
		require.ErrorIs(t, err, context.Canceled)
	})
}

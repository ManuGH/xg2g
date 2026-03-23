package manager

import (
	"context"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLeaseExpiryWorker_ExpiresPrimingSessions(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()
	now := time.Now()

	expired := &model.SessionRecord{
		SessionID:          "sess-priming-expired",
		State:              model.SessionPriming,
		ServiceRef:         "1:0:1:1:1:1:C00000:0:0:0:",
		CreatedAtUnix:      now.Add(-2 * time.Minute).Unix(),
		UpdatedAtUnix:      now.Add(-2 * time.Minute).Unix(),
		LeaseExpiresAtUnix: now.Add(-1 * time.Minute).Unix(),
	}
	fresh := &model.SessionRecord{
		SessionID:          "sess-priming-fresh",
		State:              model.SessionPriming,
		ServiceRef:         "1:0:1:2:2:2:C00000:0:0:0:",
		CreatedAtUnix:      now.Unix(),
		UpdatedAtUnix:      now.Unix(),
		LeaseExpiresAtUnix: now.Add(2 * time.Minute).Unix(),
	}

	require.NoError(t, st.PutSession(ctx, expired))
	require.NoError(t, st.PutSession(ctx, fresh))

	worker := &LeaseExpiryWorker{
		Store:  st,
		Config: &config.AppConfig{},
	}

	worker.expireStaleSessions(ctx)

	gotExpired, err := st.GetSession(ctx, expired.SessionID)
	require.NoError(t, err)
	require.NotNil(t, gotExpired)
	assert.Equal(t, model.SessionStopped, gotExpired.State)
	assert.Equal(t, model.RLeaseExpired, gotExpired.Reason)
	assert.Equal(t, "LEASE_EXPIRED", gotExpired.StopReason)

	gotFresh, err := st.GetSession(ctx, fresh.SessionID)
	require.NoError(t, err)
	require.NotNil(t, gotFresh)
	assert.Equal(t, model.SessionPriming, gotFresh.State)
}

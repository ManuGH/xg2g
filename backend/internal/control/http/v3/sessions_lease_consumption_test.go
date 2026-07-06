package v3

import (
	"context"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/stretchr/testify/require"
)

func TestRenewLeaseFromConsumption_ExtendsStaleLease(t *testing.T) {
	s, st := newV3TestServer(t, t.TempDir())
	s.cfg.Sessions.LeaseTTL = 120 * time.Second

	sessionID := "550e8400-e29b-41d4-a716-446655440101"
	now := time.Now().UTC()
	oldExpiry := now.Add(20 * time.Second).Unix()

	require.NoError(t, st.PutSession(context.Background(), &model.SessionRecord{
		SessionID:          sessionID,
		State:              model.SessionReady,
		ServiceRef:         "1:0:1:445D:453:1:C00000:0:0:0:",
		HeartbeatInterval:  30,
		LeaseExpiresAtUnix: oldExpiry,
		// Last heartbeat outside the idempotency window → renewal must fire.
		LastHeartbeatUnix: now.Add(-31 * time.Second).Unix(),
	}))

	s.renewLeaseFromConsumption(context.Background(), sessionID)

	updated, err := st.GetSession(context.Background(), sessionID)
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Greater(t, updated.LeaseExpiresAtUnix, oldExpiry, "lease must be extended by consumption")
	require.GreaterOrEqual(t, updated.LastHeartbeatUnix, now.Unix(), "renewal must reset the idempotency window")
}

func TestRenewLeaseFromConsumption_IdempotentWithinInterval(t *testing.T) {
	s, st := newV3TestServer(t, t.TempDir())
	s.cfg.Sessions.LeaseTTL = 120 * time.Second

	sessionID := "550e8400-e29b-41d4-a716-446655440102"
	now := time.Now().UTC()
	expiry := now.Add(90 * time.Second).Unix()
	lastHB := now.Add(-5 * time.Second).Unix()

	require.NoError(t, st.PutSession(context.Background(), &model.SessionRecord{
		SessionID:          sessionID,
		State:              model.SessionReady,
		ServiceRef:         "1:0:1:445D:453:1:C00000:0:0:0:",
		HeartbeatInterval:  30,
		LeaseExpiresAtUnix: expiry,
		LastHeartbeatUnix:  lastHB,
	}))

	s.renewLeaseFromConsumption(context.Background(), sessionID)

	updated, err := st.GetSession(context.Background(), sessionID)
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, expiry, updated.LeaseExpiresAtUnix, "no write within the idempotency window")
	require.Equal(t, lastHB, updated.LastHeartbeatUnix)
}

func TestRenewLeaseFromConsumption_ZeroIntervalFallsBackToDefaultWindow(t *testing.T) {
	s, st := newV3TestServer(t, t.TempDir())
	s.cfg.Sessions.LeaseTTL = 120 * time.Second

	sessionID := "550e8400-e29b-41d4-a716-446655440105"
	now := time.Now().UTC()
	expiry := now.Add(90 * time.Second).Unix()
	lastHB := now.Add(-5 * time.Second).Unix()

	// HeartbeatInterval 0 must not disable throttling: with the fallback
	// window (30s), a heartbeat 5s ago keeps this renewal a no-op instead of
	// producing one store write per playlist fetch.
	require.NoError(t, st.PutSession(context.Background(), &model.SessionRecord{
		SessionID:          sessionID,
		State:              model.SessionReady,
		ServiceRef:         "1:0:1:445D:453:1:C00000:0:0:0:",
		HeartbeatInterval:  0,
		LeaseExpiresAtUnix: expiry,
		LastHeartbeatUnix:  lastHB,
	}))

	s.renewLeaseFromConsumption(context.Background(), sessionID)

	updated, err := st.GetSession(context.Background(), sessionID)
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, expiry, updated.LeaseExpiresAtUnix, "zero interval must not bypass the idempotency window")
	require.Equal(t, lastHB, updated.LastHeartbeatUnix)
}

func TestRenewLeaseFromConsumption_SkipsExpiredAndTerminal(t *testing.T) {
	s, st := newV3TestServer(t, t.TempDir())
	s.cfg.Sessions.LeaseTTL = 120 * time.Second
	now := time.Now().UTC()

	expiredID := "550e8400-e29b-41d4-a716-446655440103"
	expiredExpiry := now.Add(-10 * time.Second).Unix()
	require.NoError(t, st.PutSession(context.Background(), &model.SessionRecord{
		SessionID:          expiredID,
		State:              model.SessionReady,
		ServiceRef:         "1:0:1:445D:453:1:C00000:0:0:0:",
		HeartbeatInterval:  30,
		LeaseExpiresAtUnix: expiredExpiry,
		LastHeartbeatUnix:  now.Add(-120 * time.Second).Unix(),
	}))

	terminalID := "550e8400-e29b-41d4-a716-446655440104"
	terminalExpiry := now.Add(60 * time.Second).Unix()
	require.NoError(t, st.PutSession(context.Background(), &model.SessionRecord{
		SessionID:          terminalID,
		State:              model.SessionStopped,
		ServiceRef:         "1:0:1:445D:453:1:C00000:0:0:0:",
		HeartbeatInterval:  30,
		LeaseExpiresAtUnix: terminalExpiry,
		LastHeartbeatUnix:  now.Add(-120 * time.Second).Unix(),
	}))

	s.renewLeaseFromConsumption(context.Background(), expiredID)
	s.renewLeaseFromConsumption(context.Background(), terminalID)

	expired, err := st.GetSession(context.Background(), expiredID)
	require.NoError(t, err)
	require.Equal(t, expiredExpiry, expired.LeaseExpiresAtUnix, "expired sessions belong to the reaper")

	terminal, err := st.GetSession(context.Background(), terminalID)
	require.NoError(t, err)
	require.Equal(t, terminalExpiry, terminal.LeaseExpiresAtUnix, "terminal sessions must not be revived")
}

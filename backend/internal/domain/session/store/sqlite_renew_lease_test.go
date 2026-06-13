// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestSqliteStore_RenewLeaseFailsClosedAfterLoss ensures RenewLease does not
// resurrect a lease that was released/swept or taken over by another owner. The
// old implementation delegated to TryAcquireLease and recreated the lease,
// defeating the heartbeat loop's lease-loss abort and leaving zombie sessions
// holding tuner slots.
func TestSqliteStore_RenewLeaseFailsClosedAfterLoss(t *testing.T) {
	st, err := NewSqliteStore(filepath.Join(t.TempDir(), "leases.db"))
	require.NoError(t, err)
	defer func() { _ = st.Close() }()

	ctx := context.Background()
	const key, owner = "tuner:0", "node-A"

	// Held lease renews fine.
	_, ok, err := st.TryAcquireLease(ctx, key, owner, time.Hour)
	require.NoError(t, err)
	require.True(t, ok)

	_, ok, err = st.RenewLease(ctx, key, owner, time.Hour)
	require.NoError(t, err)
	require.True(t, ok, "renew should succeed while the lease is held")

	// Lease lost (released/swept): renew must fail closed and NOT recreate it.
	require.NoError(t, st.ReleaseLease(ctx, key, owner))

	_, ok, err = st.RenewLease(ctx, key, owner, time.Hour)
	require.NoError(t, err)
	require.False(t, ok, "renew must fail closed after the lease was lost")

	_, found, err := st.GetLease(ctx, key)
	require.NoError(t, err)
	require.False(t, found, "renew must not recreate a lost lease")

	// Taken over by another owner: original owner's renew fails closed.
	_, ok, err = st.TryAcquireLease(ctx, key, "node-B", time.Hour)
	require.NoError(t, err)
	require.True(t, ok)

	_, ok, err = st.RenewLease(ctx, key, owner, time.Hour)
	require.NoError(t, err)
	require.False(t, ok, "renew must fail when another owner holds the lease")
}

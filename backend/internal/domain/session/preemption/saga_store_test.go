// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package preemption

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// RunUnifiedSagaStoreContractTests runs the complete store contract test suite against any SagaStore implementation.
func RunUnifiedSagaStoreContractTests(t *testing.T, storeFactory func(t *testing.T) SagaStore) {
	ctx := context.Background()

	t.Run("CreateSaga_IdempotencyAndDuplicateDetection", func(t *testing.T) {
		store := storeFactory(t)
		now := time.Now().UTC()

		rec := PreemptionSagaRecord{
			SagaID:               "saga-1",
			ReceiverID:           "rec-1",
			ContractID:           "contract-1",
			PreparedTeardownHash: "hash-100",
			State:                StatePrepared,
			Mode:                 PreemptionModeEnforce,
			Version:              1,
			CreatedAt:            now,
			UpdatedAt:            now,
			ExpiresAt:            now.Add(30 * time.Second),
		}

		// Initial creation -> created = true
		createdRec, created, err := store.CreateSaga(ctx, rec)
		require.NoError(t, err)
		require.True(t, created)
		require.Equal(t, "saga-1", createdRec.SagaID)

		// Duplicate with identical content -> created = false, no error, returns existing
		dupRec, created2, err := store.CreateSaga(ctx, rec)
		require.NoError(t, err)
		require.False(t, created2)
		require.Equal(t, "saga-1", dupRec.SagaID)

		// Conflict with same SagaID but different ContractID -> ErrSagaIdentityConflict
		mismatched := rec
		mismatched.ContractID = "contract-DIFFERENT"
		_, _, err = store.CreateSaga(ctx, mismatched)
		require.ErrorIs(t, err, ErrSagaIdentityConflict)

		// Conflict with different SagaID but same PreparedTeardownHash -> ErrDuplicateSaga
		diffSaga := rec
		diffSaga.SagaID = "saga-2"
		_, _, err = store.CreateSaga(ctx, diffSaga)
		require.ErrorIs(t, err, ErrDuplicateSaga)
	})

	t.Run("ClaimSagaExecution_FencingTokensAndContention", func(t *testing.T) {
		store := storeFactory(t)
		now := time.Now().UTC()

		rec := PreemptionSagaRecord{
			SagaID:               "saga-claim-1",
			ReceiverID:           "rec-1",
			ContractID:           "c-1",
			PreparedTeardownHash: "h-1",
			State:                StatePrepared,
			Mode:                 PreemptionModeEnforce,
			Version:              1,
			CreatedAt:            now,
			UpdatedAt:            now,
			ExpiresAt:            now.Add(30 * time.Second),
		}
		_, _, err := store.CreateSaga(ctx, rec)
		require.NoError(t, err)

		// Worker 1 claims receiver-1
		t1 := SagaTransition{ReasonCode: SagaReasonReceiverClaimed, OccurredAt: now, Actor: "worker-1"}
		s1, c1, err := store.ClaimSagaExecution(ctx, "saga-claim-1", 1, "rec-1", now, now.Add(10*time.Second), t1)
		require.NoError(t, err)
		require.Equal(t, StateExecutionClaimed, s1.State)
		require.Equal(t, uint64(2), s1.Version)
		require.Equal(t, uint64(1), c1.FencingToken)

		// Worker 2 attempts parallel claim for different saga on same active lock -> ErrReceiverClaimLocked
		rec2 := PreemptionSagaRecord{
			SagaID:               "saga-claim-2",
			ReceiverID:           "rec-1",
			ContractID:           "c-2",
			PreparedTeardownHash: "h-2",
			State:                StatePrepared,
			Mode:                 PreemptionModeEnforce,
			Version:              1,
			CreatedAt:            now,
			UpdatedAt:            now,
			ExpiresAt:            now.Add(30 * time.Second),
		}
		_, _, err = store.CreateSaga(ctx, rec2)
		require.NoError(t, err)

		t2 := SagaTransition{ReasonCode: SagaReasonReceiverClaimed, OccurredAt: now, Actor: "worker-2"}
		_, _, err = store.ClaimSagaExecution(ctx, "saga-claim-2", 1, "rec-1", now, now.Add(10*time.Second), t2)
		require.ErrorIs(t, err, ErrReceiverClaimLocked)

		// Override after lease expiration generates monotonic FencingToken 2
		later := now.Add(15 * time.Second) // after lease expiration
		s2, c2, err := store.ClaimSagaExecution(ctx, "saga-claim-2", 1, "rec-1", later, later.Add(10*time.Second), t2)
		require.NoError(t, err)
		require.Equal(t, StateExecutionClaimed, s2.State)
		require.Equal(t, uint64(2), c2.FencingToken, "FencingToken MUST be strictly monotonic (2 > 1)")

		// Stale Worker 1 attempts CAS with old FencingToken 1 -> ErrFencingTokenMismatch
		tCAS := SagaTransition{ReasonCode: SagaReasonTargetRevalidated, OccurredAt: later, Actor: "worker-1"}
		_, err = store.CompareAndSwapState(ctx, "saga-claim-1", 2, "rec-1", 1, StateExecutionClaimed, StateTargetRevalidated, tCAS)
		require.ErrorIs(t, err, ErrFencingTokenMismatch)
	})

	t.Run("FencingTokenPreservedAcrossRelease", func(t *testing.T) {
		store := storeFactory(t)
		now := time.Now().UTC()

		rec := PreemptionSagaRecord{
			SagaID:               "saga-rel-1",
			ReceiverID:           "rec-release",
			ContractID:           "c-rel",
			PreparedTeardownHash: "h-rel",
			State:                StatePrepared,
			Mode:                 PreemptionModeEnforce,
			Version:              1,
			CreatedAt:            now,
			UpdatedAt:            now,
			ExpiresAt:            now.Add(30 * time.Second),
		}
		_, _, err := store.CreateSaga(ctx, rec)
		require.NoError(t, err)

		t1 := SagaTransition{ReasonCode: SagaReasonReceiverClaimed, OccurredAt: now, Actor: "worker-1"}
		s1, c1, err := store.ClaimSagaExecution(ctx, "saga-rel-1", 1, "rec-release", now, now.Add(10*time.Second), t1)
		require.NoError(t, err)
		require.Equal(t, uint64(1), c1.FencingToken)

		// Terminate saga and release claim
		tRel := SagaTransition{ReasonCode: SagaReasonRequesterDropped, OccurredAt: now, Actor: "worker-1"}
		_, err = store.TransitionAndReleaseClaim(ctx, "saga-rel-1", s1.Version, "rec-release", c1.FencingToken, StateFailedBeforeMutation, tRel)
		require.NoError(t, err)

		// Next claim for rec-release MUST generate FencingToken 2 (not reset to 1!)
		rec2 := PreemptionSagaRecord{
			SagaID:               "saga-rel-2",
			ReceiverID:           "rec-release",
			ContractID:           "c-rel-2",
			PreparedTeardownHash: "h-rel-2",
			State:                StatePrepared,
			Mode:                 PreemptionModeEnforce,
			Version:              1,
			CreatedAt:            now,
			UpdatedAt:            now,
			ExpiresAt:            now.Add(30 * time.Second),
		}
		_, _, err = store.CreateSaga(ctx, rec2)
		require.NoError(t, err)

		t2 := SagaTransition{ReasonCode: SagaReasonReceiverClaimed, OccurredAt: now, Actor: "worker-2"}
		_, c2, err := store.ClaimSagaExecution(ctx, "saga-rel-2", 1, "rec-release", now, now.Add(10*time.Second), t2)
		require.NoError(t, err)
		require.Equal(t, uint64(2), c2.FencingToken, "FencingToken MUST NOT reset to 1 after release")
	})

	t.Run("RenewReceiverClaim_ValidAndInvalidScenarios", func(t *testing.T) {
		store := storeFactory(t)
		now := time.Now().UTC()

		rec := PreemptionSagaRecord{
			SagaID:               "saga-renew",
			ReceiverID:           "rec-renew",
			ContractID:           "c-renew",
			PreparedTeardownHash: "h-renew",
			State:                StatePrepared,
			Mode:                 PreemptionModeEnforce,
			Version:              1,
			CreatedAt:            now,
			UpdatedAt:            now,
			ExpiresAt:            now.Add(30 * time.Second),
		}
		_, _, err := store.CreateSaga(ctx, rec)
		require.NoError(t, err)

		t1 := SagaTransition{ReasonCode: SagaReasonReceiverClaimed, OccurredAt: now, Actor: "worker-1"}
		_, c1, err := store.ClaimSagaExecution(ctx, "saga-renew", 1, "rec-renew", now, now.Add(10*time.Second), t1)
		require.NoError(t, err)

		// Renew claim with valid token & claim version
		renewed, err := store.RenewReceiverClaim(ctx, "rec-renew", "saga-renew", c1.FencingToken, c1.ClaimVersion, now, now.Add(30*time.Second))
		require.NoError(t, err)
		require.Equal(t, c1.FencingToken, renewed.FencingToken)
		require.Equal(t, c1.ClaimVersion+1, renewed.ClaimVersion)

		// Renew with invalid token -> ErrFencingTokenMismatch
		_, err = store.RenewReceiverClaim(ctx, "rec-renew", "saga-renew", 999, renewed.ClaimVersion, now, now.Add(30*time.Second))
		require.ErrorIs(t, err, ErrFencingTokenMismatch)
	})

	t.Run("PhaseBoundary_RejectsMutationTransitionsInE33a", func(t *testing.T) {
		store := storeFactory(t)
		now := time.Now().UTC()

		rec := PreemptionSagaRecord{
			SagaID:               "saga-boundary",
			ReceiverID:           "rec-bound",
			ContractID:           "c-bound",
			PreparedTeardownHash: "h-bound",
			State:                StatePrepared,
			Mode:                 PreemptionModeEnforce,
			Version:              1,
			CreatedAt:            now,
			UpdatedAt:            now,
			ExpiresAt:            now.Add(30 * time.Second),
		}
		_, _, err := store.CreateSaga(ctx, rec)
		require.NoError(t, err)

		t1 := SagaTransition{ReasonCode: SagaReasonReceiverClaimed, OccurredAt: now, Actor: "worker-1"}
		s1, c1, err := store.ClaimSagaExecution(ctx, "saga-boundary", 1, "rec-bound", now, now.Add(10*time.Second), t1)
		require.NoError(t, err)

		// Attempting CAS to TEARDOWN_REQUESTED MUST be rejected in Phase E3.3a
		tMut := SagaTransition{ReasonCode: SagaReasonPhaseNotEnabled, OccurredAt: now, Actor: "worker-1"}
		_, err = store.CompareAndSwapState(ctx, "saga-boundary", s1.Version, "rec-bound", c1.FencingToken, StateExecutionClaimed, StateTeardownRequested, tMut)
		require.ErrorIs(t, err, ErrMutationPhaseDisabled)
	})
}

func TestMemorySagaStore_Contract(t *testing.T) {
	RunUnifiedSagaStoreContractTests(t, func(t *testing.T) SagaStore {
		return NewMemorySagaStore()
	})
}

func TestPersistentSagaStore_Contract(t *testing.T) {
	RunUnifiedSagaStoreContractTests(t, func(t *testing.T) SagaStore {
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "saga_test.db")
		store, err := NewPersistentSagaStore(dbPath)
		require.NoError(t, err)
		t.Cleanup(func() {
			_ = store.Close()
		})
		return store
	})
}

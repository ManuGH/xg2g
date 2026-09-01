// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package preemption

import (
	"context"
	"encoding/json"
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

		// Verify History From state is PREPARED (not EXECUTION_CLAIMED)
		require.Len(t, s1.History, 1)
		require.Equal(t, StatePrepared, s1.History[0].From)
		require.Equal(t, StateExecutionClaimed, s1.History[0].To)

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

	t.Run("TransitionAndReleaseClaim_InvalidTokenPerformsZeroMutation", func(t *testing.T) {
		store := storeFactory(t)
		now := time.Now().UTC()

		rec := PreemptionSagaRecord{
			SagaID:               "saga-trans-zero",
			ReceiverID:           "rec-zero",
			ContractID:           "c-zero",
			PreparedTeardownHash: "h-zero",
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
		s1, c1, err := store.ClaimSagaExecution(ctx, "saga-trans-zero", 1, "rec-zero", now, now.Add(10*time.Second), t1)
		require.NoError(t, err)
		require.Equal(t, uint64(1), c1.FencingToken)

		// Snapshot saga and claim before invalid transition
		sagaBefore, err := store.GetSaga(ctx, "saga-trans-zero")
		require.NoError(t, err)
		claimBefore, err := store.GetReceiverClaim(ctx, "rec-zero")
		require.NoError(t, err)
		sagaJSONBefore, _ := json.Marshal(sagaBefore)
		claimJSONBefore, _ := json.Marshal(claimBefore)

		// Call TransitionAndReleaseClaim with INVALID FencingToken (999 != 1)
		tFail := SagaTransition{ReasonCode: SagaReasonRequesterDropped, OccurredAt: now, Actor: "worker-impostor"}
		_, err = store.TransitionAndReleaseClaim(ctx, "saga-trans-zero", s1.Version, "rec-zero", 999, StateFailedBeforeMutation, tFail)
		require.ErrorIs(t, err, ErrFencingTokenMismatch)

		// Verify ZERO MUTATION on both saga and receiver claim!
		sagaAfter, err := store.GetSaga(ctx, "saga-trans-zero")
		require.NoError(t, err)
		claimAfter, err := store.GetReceiverClaim(ctx, "rec-zero")
		require.NoError(t, err)
		sagaJSONAfter, _ := json.Marshal(sagaAfter)
		claimJSONAfter, _ := json.Marshal(claimAfter)

		require.Equal(t, string(sagaJSONBefore), string(sagaJSONAfter), "Saga MUST NOT be mutated when fencing token is invalid")
		require.Equal(t, string(claimJSONBefore), string(claimJSONAfter), "Receiver claim MUST NOT be released when fencing token is invalid")
	})

	t.Run("TransitionAndReleaseClaim_RecordsCorrectFromState", func(t *testing.T) {
		store := storeFactory(t)
		now := time.Now().UTC()

		rec := PreemptionSagaRecord{
			SagaID:               "saga-hist-test",
			ReceiverID:           "rec-hist",
			ContractID:           "c-hist",
			PreparedTeardownHash: "h-hist",
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
		s1, c1, err := store.ClaimSagaExecution(ctx, "saga-hist-test", 1, "rec-hist", now, now.Add(10*time.Second), t1)
		require.NoError(t, err)

		// Release claim and transition to FAILED_BEFORE_MUTATION
		tRel := SagaTransition{ReasonCode: SagaReasonRequesterDropped, OccurredAt: now, Actor: "worker-1"}
		s2, err := store.TransitionAndReleaseClaim(ctx, "saga-hist-test", s1.Version, "rec-hist", c1.FencingToken, StateFailedBeforeMutation, tRel)
		require.NoError(t, err)

		// Verify History: last transition MUST have From = StateExecutionClaimed and To = StateFailedBeforeMutation
		require.GreaterOrEqual(t, len(s2.History), 1)
		lastTrans := s2.History[len(s2.History)-1]
		require.Equal(t, StateExecutionClaimed, lastTrans.From)
		require.Equal(t, StateFailedBeforeMutation, lastTrans.To)
	})

	t.Run("QuarantineReceiverClaim_RequiresValidOwnerAndFencingToken", func(t *testing.T) {
		store := storeFactory(t)
		now := time.Now().UTC()

		rec := PreemptionSagaRecord{
			SagaID:               "saga-quar-1",
			ReceiverID:           "rec-quar",
			ContractID:           "c-quar",
			PreparedTeardownHash: "h-quar",
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
		_, c1, err := store.ClaimSagaExecution(ctx, "saga-quar-1", 1, "rec-quar", now, now.Add(10*time.Second), t1)
		require.NoError(t, err)

		// Quarantine attempt with invalid token -> ErrFencingTokenMismatch
		_, err = store.QuarantineReceiverClaim(ctx, "rec-quar", "saga-quar-1", 999, "stale token test", now)
		require.ErrorIs(t, err, ErrFencingTokenMismatch)

		// Quarantine attempt with invalid saga ID -> error
		_, err = store.QuarantineReceiverClaim(ctx, "rec-quar", "saga-impostor", c1.FencingToken, "impostor saga test", now)
		require.Error(t, err)

		// Quarantine attempt with valid token and owner -> SUCCESS
		quarClaim, err := store.QuarantineReceiverClaim(ctx, "rec-quar", "saga-quar-1", c1.FencingToken, "hardware fault", now)
		require.NoError(t, err)
		require.Equal(t, ClaimStatusQuarantined, quarClaim.Status)
		require.Equal(t, c1.FencingToken, quarClaim.FencingToken, "Quarantine MUST preserve the per-receiver FencingToken")

		// The answer has to describe the row that was just written. Both fields
		// were previously left at their zero values by the persistent store while
		// the in-memory one returned them, so the same call meant different things
		// depending on which SagaStore was behind it. Asserted here, in the shared
		// suite, so neither implementation can drift from the other again.
		require.Equal(t, c1.ClaimVersion+1, quarClaim.ClaimVersion,
			"Quarantine MUST return the incremented ClaimVersion, not a zero value")
		require.Equal(t, c1.LeaseUntil.UTC(), quarClaim.LeaseUntil.UTC(),
			"Quarantine does not touch lease_until, so it MUST return the lease the claim already had")
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

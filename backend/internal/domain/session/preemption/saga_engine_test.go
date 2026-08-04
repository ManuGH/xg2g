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

func TestEngine_LifecycleAndRevalidation_MemoryAndSQLiteStores(t *testing.T) {
	stores := map[string]func(t *testing.T) SagaStore{
		"MemoryStore": func(t *testing.T) SagaStore {
			return NewMemorySagaStore()
		},
		"PersistentSQLiteStore": func(t *testing.T) SagaStore {
			dir := t.TempDir()
			store, err := NewPersistentSagaStore(filepath.Join(dir, "engine_test.db"))
			require.NoError(t, err)
			t.Cleanup(func() { _ = store.Close() })
			return store
		},
	}

	for name, storeFactory := range stores {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			store := storeFactory(t)
			engine := NewPreemptionSagaEngine(store)
			eval := NewEvaluator()
			preparer := NewTeardownPreparer()
			now := time.Now().UTC()

			req := PreemptionRequest{
				RequestID:             "req-eng-1",
				ReceiverID:            "rec-1",
				RequesterOwner:        "client-A",
				RequesterAllocationID: "alloc-req-1",
				RequesterRevision:     "res-rev-1",
				RequesterPriority:     PriorityAttributes{BasePriority: 100},
				RequestedResources: []ResourceClaim{
					{Kind: ResourceKindTunerSlot, Resource: "tuner-1", Quantity: 1},
				},
			}

			snapshot := ResourceSnapshot{
				ReceiverID:       "rec-1",
				SnapshotRevision: "rev-100",
				ObservedAt:       now,
				Allocations: []ActiveAllocation{
					{
						AllocationID: "alloc-1",
						Owner:        "client-B",
						Priority:     PriorityAttributes{BasePriority: 50},
						Claims:       req.RequestedResources,
					},
				},
			}

			proof := ConflictResolutionProof{
				ReceiverID:              "rec-1",
				SnapshotRevision:        "rev-100",
				HardwareProfileRevision: "hw-rev-1",
				HardwareProfileStatus:   HardwareProfileValid,
				EvidenceClassification:  EvidenceDirectObservation,
				RequestedResources:      req.RequestedResources,
				AllocationMappings: []AllocationResourceMapping{
					{AllocationID: "alloc-1", FreedResources: req.RequestedResources},
				},
			}

			// E3.1 Select Plan
			selRes, err := eval.SelectPlan(req, snapshot, proof, now)
			require.NoError(t, err)

			// E3.2 Prepare Teardown
			prepRes, err := preparer.PrepareTeardown(selRes.Contract, snapshot, proof, now)
			require.NoError(t, err)
			prepared := prepRes.Prepared

			// 1. Create Saga in PREPARED state (ReceiverID MUST be rec-1, NOT alloc-1!)
			saga, created, err := engine.CreateSaga(ctx, prepared, PreemptionModeEnforce, now)
			require.NoError(t, err)
			require.True(t, created)
			require.Equal(t, StatePrepared, saga.State)
			require.Equal(t, "rec-1", saga.ReceiverID, "Saga ReceiverID MUST be rec-1")

			// 2. Claim Receiver Execution (PREPARED -> EXECUTION_CLAIMED)
			claimedSaga, claim, err := engine.ClaimExecution(ctx, saga.SagaID, "rec-1", 15*time.Second, now)
			require.NoError(t, err)
			require.Equal(t, StateExecutionClaimed, claimedSaga.State)
			require.Equal(t, uint64(1), claim.FencingToken)

			// 3. Valid RequesterReservation
			reservation := RequesterReservation{
				ReservationID:         "res-1",
				RequestID:             "req-eng-1",
				ReceiverID:            "rec-1",
				Owner:                 "client-A",
				ContractID:            selRes.Contract.ContractID,
				PreparedTeardownHash:  prepared.PreparedTeardownHash,
				RequesterAllocationID: "alloc-req-1",
				ResourceClaims:        req.RequestedResources,
				Revision:              "res-rev-1",
				ExpiresAt:             now.Add(30 * time.Second),
			}

			// 4. Revalidate and Transition (EXECUTION_CLAIMED -> TARGET_REVALIDATED)
			revalidatedSaga, err := engine.RevalidateAndTransition(ctx, saga.SagaID, "rec-1", claim.FencingToken, selRes.Contract, prepared, snapshot, proof, reservation, now)
			require.NoError(t, err)
			require.Equal(t, StateTargetRevalidated, revalidatedSaga.State)

			// 5. Attempt Teardown Transition MUST be rejected in E3.3a
			_, err = engine.AttemptTeardownTransition(ctx, saga.SagaID, "rec-1", claim.FencingToken)
			require.ErrorIs(t, err, ErrMutationPhaseDisabled)
		})
	}
}

func TestEngine_RequesterReservation_StrictValidation(t *testing.T) {
	ctx := context.Background()
	store := NewMemorySagaStore()
	engine := NewPreemptionSagaEngine(store)
	eval := NewEvaluator()
	preparer := NewTeardownPreparer()
	now := time.Now().UTC()

	req := PreemptionRequest{
		RequestID:             "req-res-test",
		ReceiverID:            "rec-1",
		RequesterOwner:        "client-A",
		RequesterAllocationID: "alloc-req-1",
		RequesterRevision:     "rev-1",
		RequesterPriority:     PriorityAttributes{BasePriority: 100},
		RequestedResources: []ResourceClaim{
			{Kind: ResourceKindTunerSlot, Resource: "tuner-1", Quantity: 1},
		},
	}

	snapshot := ResourceSnapshot{
		ReceiverID:       "rec-1",
		SnapshotRevision: "rev-100",
		ObservedAt:       now,
		Allocations: []ActiveAllocation{
			{AllocationID: "alloc-1", Owner: "client-B", Revision: "alloc-rev-100", Claims: req.RequestedResources},
		},
	}

	proof := ConflictResolutionProof{
		ReceiverID:              "rec-1",
		SnapshotRevision:        "rev-100",
		HardwareProfileRevision: "hw-rev-1",
		HardwareProfileStatus:   HardwareProfileValid,
		EvidenceClassification:  EvidenceDirectObservation,
		RequestedResources:      req.RequestedResources,
		AllocationMappings: []AllocationResourceMapping{
			{AllocationID: "alloc-1", FreedResources: req.RequestedResources},
		},
	}

	createTestSaga := func(requestID string) (PreemptionSagaRecord, ReceiverClaim, SelectionResult, *PreparedTeardown) {
		r := req
		r.RequestID = requestID
		sRes, _ := eval.SelectPlan(r, snapshot, proof, now)
		pRes, _ := preparer.PrepareTeardown(sRes.Contract, snapshot, proof, now)
		s, _, _ := engine.CreateSaga(ctx, pRes.Prepared, PreemptionModeEnforce, now)
		sClaimed, c, _ := engine.ClaimExecution(ctx, s.SagaID, "rec-1", 15*time.Second, now)
		return sClaimed, c, sRes, pRes.Prepared
	}

	// 1. Receiver ID mismatch -> Rejection
	saga1, claim1, selRes1, prepared1 := createTestSaga("req-res-1")
	resBadRec := RequesterReservation{
		ReservationID:         "res-1",
		RequestID:             "req-res-1",
		ReceiverID:            "rec-OTHER", // Bad receiver!
		Owner:                 "client-A",
		ContractID:            selRes1.Contract.ContractID,
		PreparedTeardownHash:  prepared1.PreparedTeardownHash,
		RequesterAllocationID: "alloc-req-1",
		ResourceClaims:        req.RequestedResources,
		Revision:              "rev-1",
		ExpiresAt:             now.Add(30 * time.Second),
	}
	_, err := engine.RevalidateAndTransition(ctx, saga1.SagaID, "rec-1", claim1.FencingToken, selRes1.Contract, prepared1, snapshot, proof, resBadRec, now)
	require.Error(t, err)
	require.Contains(t, err.Error(), "receiver ID")

	// 2. Request ID mismatch -> Rejection
	saga2, claim2, selRes2, prepared2 := createTestSaga("req-res-2")
	resBadReq := RequesterReservation{
		ReservationID:         "res-2",
		RequestID:             "req-OTHER", // Bad request!
		ReceiverID:            "rec-1",
		Owner:                 "client-A",
		ContractID:            selRes2.Contract.ContractID,
		PreparedTeardownHash:  prepared2.PreparedTeardownHash,
		RequesterAllocationID: "alloc-req-1",
		ResourceClaims:        req.RequestedResources,
		Revision:              "rev-1",
		ExpiresAt:             now.Add(30 * time.Second),
	}
	_, err = engine.RevalidateAndTransition(ctx, saga2.SagaID, "rec-1", claim2.FencingToken, selRes2.Contract, prepared2, snapshot, proof, resBadReq, now)
	require.Error(t, err)
	require.Contains(t, err.Error(), "request ID")

	// 3. Claims mismatch -> Rejection
	saga3, claim3, selRes3, prepared3 := createTestSaga("req-res-3")
	resBadClaims := RequesterReservation{
		ReservationID:         "res-3",
		RequestID:             "req-res-3",
		ReceiverID:            "rec-1",
		Owner:                 "client-A",
		ContractID:            selRes3.Contract.ContractID,
		PreparedTeardownHash:  prepared3.PreparedTeardownHash,
		RequesterAllocationID: "alloc-req-1",
		ResourceClaims:        []ResourceClaim{{Kind: ResourceKindRestrictedAccessSlot, Resource: "slot-999", Quantity: 1}},
		Revision:              "rev-1",
		ExpiresAt:             now.Add(30 * time.Second),
	}
	_, err = engine.RevalidateAndTransition(ctx, saga3.SagaID, "rec-1", claim3.FencingToken, selRes3.Contract, prepared3, snapshot, proof, resBadClaims, now)
	require.Error(t, err)
	require.Contains(t, err.Error(), "reservation resource claims do not match")

	// 4. Invalid reservation claims (e.g. quantity -1) -> Explicit typed error
	saga4, claim4, selRes4, prepared4 := createTestSaga("req-res-4")
	resInvalidClaims := RequesterReservation{
		ReservationID:         "res-4",
		RequestID:             "req-res-4",
		ReceiverID:            "rec-1",
		Owner:                 "client-A",
		ContractID:            selRes4.Contract.ContractID,
		PreparedTeardownHash:  prepared4.PreparedTeardownHash,
		RequesterAllocationID: "alloc-req-1",
		ResourceClaims:        []ResourceClaim{{Kind: ResourceKindTunerSlot, Resource: "tuner-1", Quantity: -1}},
		Revision:              "rev-1",
		ExpiresAt:             now.Add(30 * time.Second),
	}
	_, err = engine.RevalidateAndTransition(ctx, saga4.SagaID, "rec-1", claim4.FencingToken, selRes4.Contract, prepared4, snapshot, proof, resInvalidClaims, now)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid reservation resource claims")
}

func TestEngine_RevalidationFailure_AbortsSagaAndReleasesClaim(t *testing.T) {
	ctx := context.Background()
	store := NewMemorySagaStore()
	engine := NewPreemptionSagaEngine(store)
	eval := NewEvaluator()
	preparer := NewTeardownPreparer()
	now := time.Now().UTC()

	req := PreemptionRequest{
		RequestID:             "req-abort",
		ReceiverID:            "rec-1",
		RequesterOwner:        "client-A",
		RequesterAllocationID: "alloc-req-1",
		RequesterRevision:     "rev-1",
		RequesterPriority:     PriorityAttributes{BasePriority: 100},
		RequestedResources: []ResourceClaim{
			{Kind: ResourceKindTunerSlot, Resource: "tuner-1", Quantity: 1},
		},
	}

	snapshot := ResourceSnapshot{
		ReceiverID:       "rec-1",
		SnapshotRevision: "rev-100",
		ObservedAt:       now,
		Allocations: []ActiveAllocation{
			{AllocationID: "alloc-1", Owner: "client-B", Revision: "alloc-rev-100", Claims: req.RequestedResources},
		},
	}

	proof := ConflictResolutionProof{
		ReceiverID:              "rec-1",
		SnapshotRevision:        "rev-100",
		HardwareProfileRevision: "hw-rev-1",
		HardwareProfileStatus:   HardwareProfileValid,
		EvidenceClassification:  EvidenceDirectObservation,
		RequestedResources:      req.RequestedResources,
		AllocationMappings: []AllocationResourceMapping{
			{AllocationID: "alloc-1", FreedResources: req.RequestedResources},
		},
	}

	selRes, _ := eval.SelectPlan(req, snapshot, proof, now)
	prepRes, _ := preparer.PrepareTeardown(selRes.Contract, snapshot, proof, now)
	prepared := prepRes.Prepared

	saga, _, _ := engine.CreateSaga(ctx, prepared, PreemptionModeEnforce, now)
	_, claim, _ := engine.ClaimExecution(ctx, saga.SagaID, "rec-1", 15*time.Second, now)

	// Expired RequesterReservation!
	expiredReservation := RequesterReservation{
		ReservationID:         "res-expired",
		RequestID:             "req-abort",
		ReceiverID:            "rec-1",
		Owner:                 "client-A",
		ContractID:            selRes.Contract.ContractID,
		PreparedTeardownHash:  prepared.PreparedTeardownHash,
		RequesterAllocationID: "alloc-req-1",
		Revision:              "rev-1",
		ResourceClaims:        req.RequestedResources,
		ExpiresAt:             now.Add(-10 * time.Second), // EXPIRED!
	}

	// Revalidation MUST fail, transition saga to FAILED_BEFORE_MUTATION, and release claim
	_, err := engine.RevalidateAndTransition(ctx, saga.SagaID, "rec-1", claim.FencingToken, selRes.Contract, prepared, snapshot, proof, expiredReservation, now)
	require.Error(t, err)

	// Verify saga state is FAILED_BEFORE_MUTATION
	updatedSaga, getErr := store.GetSaga(ctx, saga.SagaID)
	require.NoError(t, getErr)
	require.Equal(t, StateFailedBeforeMutation, updatedSaga.State)

	// Verify claim is released (UNCLAIMED)
	updatedClaim, claimErr := store.GetReceiverClaim(ctx, "rec-1")
	require.NoError(t, claimErr)
	require.Equal(t, ClaimStatusUnclaimed, updatedClaim.Status)
}

type spySagaStore struct {
	SagaStore
	calls int
}

func (s *spySagaStore) GetSaga(ctx context.Context, sagaID string) (PreemptionSagaRecord, error) {
	s.calls++
	return s.SagaStore.GetSaga(ctx, sagaID)
}

func (s *spySagaStore) CompareAndSwapState(ctx context.Context, sagaID string, expectedVersion uint64, receiverID string, fencingToken uint64, fromState PreemptionSagaState, toState PreemptionSagaState, transition SagaTransition) (PreemptionSagaRecord, error) {
	s.calls++
	return s.SagaStore.CompareAndSwapState(ctx, sagaID, expectedVersion, receiverID, fencingToken, fromState, toState, transition)
}

func TestEngine_AttemptTeardownTransition_ZeroStoreCallsSpy(t *testing.T) {
	innerStore := NewMemorySagaStore()
	spy := &spySagaStore{SagaStore: innerStore}
	engine := NewPreemptionSagaEngine(spy)

	_, err := engine.AttemptTeardownTransition(context.Background(), "saga-any", "rec-1", 1)
	require.ErrorIs(t, err, ErrMutationPhaseDisabled)
	require.Equal(t, 0, spy.calls, "AttemptTeardownTransition MUST NOT execute any store calls in E3.3a")
}

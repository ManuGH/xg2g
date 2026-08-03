// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package preemption

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// PreemptionSagaEngine orchestrates persistent preemption saga lifecycles up to TARGET_REVALIDATED.
type PreemptionSagaEngine struct {
	store    SagaStore
	preparer *TeardownPreparer
}

// NewPreemptionSagaEngine creates a new PreemptionSagaEngine instance.
func NewPreemptionSagaEngine(store SagaStore) *PreemptionSagaEngine {
	return &PreemptionSagaEngine{
		store:    store,
		preparer: NewTeardownPreparer(),
	}
}

// CreateSaga idempotently registers a new preemption saga in PREPARED state.
func (e *PreemptionSagaEngine) CreateSaga(ctx context.Context, prepared *PreparedTeardown, mode PreemptionMode, now time.Time) (PreemptionSagaRecord, bool, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}

	if err := ValidatePreparedTeardown(prepared, now); err != nil {
		return PreemptionSagaRecord{}, false, fmt.Errorf("cannot create saga from invalid prepared teardown: %w", err)
	}

	sagaID := fmt.Sprintf("saga-%s", prepared.TeardownID)
	rec := PreemptionSagaRecord{
		SagaID:               sagaID,
		ReceiverID:           prepared.TargetAllocationIDs[0], // primary target receiver
		ContractID:           prepared.ContractID,
		PreparedTeardownHash: prepared.PreparedTeardownHash,
		State:                StatePrepared,
		Mode:                 mode,
		Version:              1,
		CreatedAt:            now,
		UpdatedAt:            now,
		ExpiresAt:            prepared.ExpiresAt,
		History: []SagaTransition{
			{
				Sequence:   1,
				From:       "",
				To:         StatePrepared,
				ReasonCode: SagaReasonCreated,
				OccurredAt: now,
				Actor:      "preemption_engine",
			},
		},
	}

	// Fetch primary receiver ID from contract if available
	if len(prepared.TargetAllocationIDs) > 0 {
		rec.ReceiverID = prepared.TargetAllocationIDs[0]
	}

	return e.store.CreateSaga(ctx, rec)
}

// ClaimExecution atomically acquires receiver execution lock, increments fencing token, and transitions to EXECUTION_CLAIMED.
func (e *PreemptionSagaEngine) ClaimExecution(ctx context.Context, sagaID string, receiverID string, leaseDuration time.Duration, now time.Time) (PreemptionSagaRecord, ReceiverClaim, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if leaseDuration <= 0 {
		leaseDuration = 30 * time.Second
	}

	saga, err := e.store.GetSaga(ctx, sagaID)
	if err != nil {
		return PreemptionSagaRecord{}, ReceiverClaim{}, err
	}

	leaseUntil := now.Add(leaseDuration)
	transition := SagaTransition{
		From:       saga.State,
		To:         StateExecutionClaimed,
		ReasonCode: SagaReasonReceiverClaimed,
		OccurredAt: now,
		Actor:      "preemption_engine",
	}

	return e.store.ClaimSagaExecution(ctx, sagaID, saga.Version, receiverID, now, leaseUntil, transition)
}

// RevalidateAndTransition re-checks contract, live snapshot, proof, and requester reservation.
// If valid, transitions to TARGET_REVALIDATED. If invalid, aborts to FAILED_BEFORE_MUTATION and releases claim.
func (e *PreemptionSagaEngine) RevalidateAndTransition(
	ctx context.Context,
	sagaID string,
	receiverID string,
	fencingToken uint64,
	contract *PreemptionExecutionContract,
	prepared *PreparedTeardown,
	liveSnapshot ResourceSnapshot,
	liveProof ConflictResolutionProof,
	reservation RequesterReservation,
	now time.Time,
) (PreemptionSagaRecord, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}

	saga, err := e.store.GetSaga(ctx, sagaID)
	if err != nil {
		return PreemptionSagaRecord{}, err
	}

	if saga.State != StateExecutionClaimed {
		return PreemptionSagaRecord{}, fmt.Errorf("%w: saga state %s != expected %s", ErrInvalidStateTransition, saga.State, StateExecutionClaimed)
	}

	// 1. Validate RequesterReservation binding to Contract & Teardown
	if err := e.validateRequesterReservation(reservation, contract, prepared, now); err != nil {
		// Abort saga to FAILED_BEFORE_MUTATION & release claim
		trans := SagaTransition{
			From:       StateExecutionClaimed,
			To:         StateFailedBeforeMutation,
			ReasonCode: SagaReasonRequesterDropped,
			OccurredAt: now,
			Actor:      "preemption_engine",
			Details:    err.Error(),
		}
		aborted, _ := e.store.TransitionAndReleaseClaim(ctx, sagaID, saga.Version, receiverID, fencingToken, StateFailedBeforeMutation, trans)
		return aborted, fmt.Errorf("requester reservation invalid: %w", err)
	}

	// 2. Re-verify teardown preparation protocol against live snapshot & proof
	prepRes, err := e.preparer.PrepareTeardown(contract, liveSnapshot, liveProof, now)
	if err != nil || prepRes.Decision != DecisionApproved {
		reason := SagaReasonTargetMutated
		details := "live target state mutated or unverified"
		if prepRes.Reason == ReasonTeardownContractExpired {
			reason = SagaReasonContractExpired
			details = "contract expired before execution"
		}
		trans := SagaTransition{
			From:       StateExecutionClaimed,
			To:         StateFailedBeforeMutation,
			ReasonCode: reason,
			OccurredAt: now,
			Actor:      "preemption_engine",
			Details:    details,
		}
		aborted, _ := e.store.TransitionAndReleaseClaim(ctx, sagaID, saga.Version, receiverID, fencingToken, StateFailedBeforeMutation, trans)
		return aborted, fmt.Errorf("live revalidation failed: %s", prepRes.Reason)
	}

	// 3. Mode check: audit-only mode stops cleanly at TARGET_REVALIDATED
	trans := SagaTransition{
		From:       StateExecutionClaimed,
		To:         StateTargetRevalidated,
		ReasonCode: SagaReasonTargetRevalidated,
		OccurredAt: now,
		Actor:      "preemption_engine",
		Details:    fmt.Sprintf("mode=%s", saga.Mode),
	}

	return e.store.CompareAndSwapState(ctx, sagaID, saga.Version, receiverID, fencingToken, StateExecutionClaimed, StateTargetRevalidated, trans)
}

// AttemptTeardownTransition verifies Phase E3.3a boundary enforcement.
// ALL attempts to transition to TEARDOWN_REQUESTED or beyond are strictly rejected with ErrMutationPhaseDisabled!
func (e *PreemptionSagaEngine) AttemptTeardownTransition(ctx context.Context, sagaID string, receiverID string, fencingToken uint64) (PreemptionSagaRecord, error) {
	saga, err := e.store.GetSaga(ctx, sagaID)
	if err != nil {
		return PreemptionSagaRecord{}, err
	}

	trans := SagaTransition{
		From:       saga.State,
		To:         StateTeardownRequested,
		ReasonCode: SagaReasonPhaseNotEnabled,
		OccurredAt: time.Now().UTC(),
		Actor:      "preemption_engine",
	}

	// This must fail with ErrMutationPhaseDisabled
	_, err = e.store.CompareAndSwapState(ctx, sagaID, saga.Version, receiverID, fencingToken, saga.State, StateTeardownRequested, trans)
	if err != nil {
		return PreemptionSagaRecord{}, fmt.Errorf("%w: %v", ErrMutationPhaseDisabled, err)
	}

	return PreemptionSagaRecord{}, ErrMutationPhaseDisabled
}

func (e *PreemptionSagaEngine) validateRequesterReservation(res RequesterReservation, contract *PreemptionExecutionContract, prepared *PreparedTeardown, now time.Time) error {
	if strings.TrimSpace(res.ReservationID) == "" {
		return fmt.Errorf("empty reservation ID")
	}
	if res.ContractID != contract.ContractID {
		return fmt.Errorf("reservation contract ID '%s' != contract '%s'", res.ContractID, contract.ContractID)
	}
	if res.PreparedTeardownHash != prepared.PreparedTeardownHash {
		return fmt.Errorf("reservation prepared hash '%s' != prepared '%s'", res.PreparedTeardownHash, prepared.PreparedTeardownHash)
	}
	if !now.IsZero() && now.After(res.ExpiresAt) {
		return fmt.Errorf("reservation expired at %s", res.ExpiresAt.Format(time.RFC3339))
	}
	if len(res.ResourceClaims) == 0 {
		return fmt.Errorf("reservation contains zero resource claims")
	}
	return nil
}

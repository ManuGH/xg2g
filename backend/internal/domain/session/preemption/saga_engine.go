// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package preemption

import (
	"context"
	"errors"
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
		ReceiverID:           prepared.ReceiverID, // Strictly copy ReceiverID from PreparedTeardown!
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

	return e.store.CreateSaga(ctx, rec)
}

// ClaimExecution atomically acquires receiver execution lock, increments fencing token, and transitions to EXECUTION_CLAIMED.
func (e *PreemptionSagaEngine) ClaimExecution(ctx context.Context, sagaID string, receiverID string, leaseDuration time.Duration, now time.Time) (PreemptionSagaRecord, ReceiverClaim, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if leaseDuration <= 0 {
		leaseDuration = 15 * time.Second
	}

	saga, err := e.store.GetSaga(ctx, sagaID)
	if err != nil {
		return PreemptionSagaRecord{}, ReceiverClaim{}, err
	}

	if saga.ReceiverID != receiverID {
		return PreemptionSagaRecord{}, ReceiverClaim{}, fmt.Errorf("%w: requested receiver '%s' != saga receiver '%s'", ErrFencingTokenMismatch, receiverID, saga.ReceiverID)
	}

	leaseUntil := now.Add(leaseDuration)
	if leaseUntil.After(saga.ExpiresAt) {
		leaseUntil = saga.ExpiresAt
	}
	if !leaseUntil.After(now) {
		return PreemptionSagaRecord{}, ReceiverClaim{}, fmt.Errorf("%w: lease expiry %s must be after now %s", ErrContractExpired, leaseUntil, now)
	}

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

	if saga.ReceiverID != receiverID {
		return PreemptionSagaRecord{}, fmt.Errorf("%w: requested receiver '%s' != saga receiver '%s'", ErrFencingTokenMismatch, receiverID, saga.ReceiverID)
	}

	if saga.State != StateExecutionClaimed {
		return PreemptionSagaRecord{}, fmt.Errorf("%w: saga state %s != expected %s", ErrInvalidStateTransition, saga.State, StateExecutionClaimed)
	}

	// 1. Validate RequesterReservation binding to Contract & Teardown
	if resErr := e.validateRequesterReservation(reservation, contract, prepared, now); resErr != nil {
		// Abort saga to FAILED_BEFORE_MUTATION & release claim
		trans := SagaTransition{
			From:       StateExecutionClaimed,
			To:         StateFailedBeforeMutation,
			ReasonCode: SagaReasonRequesterDropped,
			OccurredAt: now,
			Actor:      "preemption_engine",
			Details:    resErr.Error(),
		}
		aborted, abortErr := e.store.TransitionAndReleaseClaim(ctx, sagaID, saga.Version, receiverID, fencingToken, StateFailedBeforeMutation, trans)
		if abortErr != nil {
			return aborted, errors.Join(resErr, abortErr)
		}
		return aborted, fmt.Errorf("requester reservation invalid: %w", resErr)
	}

	// 2. Re-verify teardown preparation protocol against live snapshot & proof
	prepRes, prepErr := e.preparer.PrepareTeardown(contract, liveSnapshot, liveProof, now)
	if prepErr != nil || prepRes.Decision != DecisionApproved {
		reason := SagaReasonTargetMutated
		details := "live target state mutated or unverified"
		failureErr := fmt.Errorf("live revalidation failed: %s", prepRes.Reason)
		if prepErr != nil {
			failureErr = prepErr
		} else if prepRes.Reason == ReasonTeardownContractExpired {
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
		aborted, abortErr := e.store.TransitionAndReleaseClaim(ctx, sagaID, saga.Version, receiverID, fencingToken, StateFailedBeforeMutation, trans)
		if abortErr != nil {
			return aborted, errors.Join(failureErr, abortErr)
		}
		return aborted, failureErr
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
// ALL attempts to transition to TEARDOWN_REQUESTED or beyond are strictly rejected at engine entry with ErrMutationPhaseDisabled!
func (e *PreemptionSagaEngine) AttemptTeardownTransition(ctx context.Context, sagaID string, receiverID string, fencingToken uint64) (PreemptionSagaRecord, error) {
	return PreemptionSagaRecord{}, ErrMutationPhaseDisabled
}

func (e *PreemptionSagaEngine) validateRequesterReservation(res RequesterReservation, contract *PreemptionExecutionContract, prepared *PreparedTeardown, now time.Time) error {
	if strings.TrimSpace(res.ReservationID) == "" {
		return fmt.Errorf("empty reservation ID")
	}
	if strings.TrimSpace(res.RequestID) == "" || res.RequestID != contract.RequestID {
		return fmt.Errorf("reservation request ID '%s' != contract request ID '%s'", res.RequestID, contract.RequestID)
	}
	if strings.TrimSpace(res.ReceiverID) == "" || res.ReceiverID != contract.ReceiverID {
		return fmt.Errorf("reservation receiver ID '%s' != contract receiver ID '%s'", res.ReceiverID, contract.ReceiverID)
	}
	if strings.TrimSpace(res.Owner) == "" || res.Owner != contract.RequesterOwner {
		return fmt.Errorf("reservation owner '%s' != contract owner '%s'", res.Owner, contract.RequesterOwner)
	}
	if strings.TrimSpace(res.RequesterAllocationID) == "" || res.RequesterAllocationID != contract.RequesterAllocationID {
		return fmt.Errorf("reservation requester allocation ID '%s' != contract requester allocation ID '%s'", res.RequesterAllocationID, contract.RequesterAllocationID)
	}
	if strings.TrimSpace(res.Revision) == "" || res.Revision != contract.RequesterRevision {
		return fmt.Errorf("reservation revision '%s' != contract requester revision '%s'", res.Revision, contract.RequesterRevision)
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
	resCanon, err1 := formatCanonicalClaimsStrict(res.ResourceClaims)
	if err1 != nil {
		return fmt.Errorf("invalid reservation resource claims: %w", err1)
	}
	contractCanon, err2 := formatCanonicalClaimsStrict(contract.RequestedResources)
	if err2 != nil {
		return fmt.Errorf("invalid contract requested resources: %w", err2)
	}
	if resCanon != contractCanon {
		return fmt.Errorf("reservation resource claims do not match contract requested resources")
	}
	return nil
}

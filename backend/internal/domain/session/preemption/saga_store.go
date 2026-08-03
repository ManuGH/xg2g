// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package preemption

import (
	"context"
	"errors"
	"time"
)

var (
	ErrSagaNotFound          = errors.New("preemption saga not found")
	ErrSagaAlreadyExists     = errors.New("preemption saga already exists")
	ErrSagaIdentityConflict  = errors.New("saga ID exists with conflicting contract or hash")
	ErrDuplicateSaga         = errors.New("active saga already exists for prepared teardown hash")
	ErrSagaVersionConflict   = errors.New("optimistic concurrency version conflict")
	ErrFencingTokenMismatch = errors.New("fencing token mismatch or expired claim")
	ErrReceiverClaimLocked   = errors.New("receiver is locked by another active preemption claim")
	ErrMutationPhaseDisabled = errors.New("mutation state transitions are disabled in this phase")
	ErrInvalidStateTransition = errors.New("invalid saga state transition")
)

// SagaStore defines the contract for saga record and receiver claim persistence.
type SagaStore interface {
	// CreateSaga idempotently registers a new preemption saga.
	// Returns created=true if created, created=false with existing record if duplicate, or an error.
	CreateSaga(ctx context.Context, record PreemptionSagaRecord) (createdRecord PreemptionSagaRecord, created bool, err error)

	// GetSaga retrieves a saga record by SagaID.
	GetSaga(ctx context.Context, sagaID string) (PreemptionSagaRecord, error)

	// ClaimSagaExecution atomically claims receiver execution lease, increments fencing token,
	// and transitions saga state from PREPARED -> EXECUTION_CLAIMED.
	ClaimSagaExecution(
		ctx context.Context,
		sagaID string,
		expectedVersion uint64,
		receiverID string,
		now time.Time,
		leaseUntil time.Time,
		transition SagaTransition,
	) (PreemptionSagaRecord, ReceiverClaim, error)

	// CompareAndSwapState performs a CAS state transition protected by saga version AND fencing token.
	CompareAndSwapState(
		ctx context.Context,
		sagaID string,
		expectedVersion uint64,
		receiverID string,
		fencingToken uint64,
		fromState PreemptionSagaState,
		toState PreemptionSagaState,
		transition SagaTransition,
	) (PreemptionSagaRecord, error)

	// RenewReceiverClaim extends the lease for a valid claim owner without incrementing fencing token.
	RenewReceiverClaim(
		ctx context.Context,
		receiverID string,
		sagaID string,
		fencingToken uint64,
		expectedClaimVersion uint64,
		now time.Time,
		leaseUntil time.Time,
	) (ReceiverClaim, error)

	// TransitionAndReleaseClaim atomically performs a terminal state transition and releases receiver claim (status -> UNCLAIMED).
	TransitionAndReleaseClaim(
		ctx context.Context,
		sagaID string,
		expectedVersion uint64,
		receiverID string,
		fencingToken uint64,
		toState PreemptionSagaState,
		transition SagaTransition,
	) (PreemptionSagaRecord, error)

	// QuarantineReceiverClaim sets the receiver claim status to QUARANTINED.
	QuarantineReceiverClaim(
		ctx context.Context,
		receiverID string,
		sagaID string,
		fencingToken uint64,
		reason string,
		now time.Time,
	) (ReceiverClaim, error)

	// FindActiveByPreparedHash searches for an active non-terminal saga matching preparedTeardownHash.
	FindActiveByPreparedHash(ctx context.Context, preparedHash string) (PreemptionSagaRecord, bool, error)

	// GetReceiverClaim retrieves the current claim state for a receiver.
	GetReceiverClaim(ctx context.Context, receiverID string) (ReceiverClaim, error)

	// ListNonTerminalSagas lists all sagas in non-terminal states.
	ListNonTerminalSagas(ctx context.Context) ([]PreemptionSagaRecord, error)
}

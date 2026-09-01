// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package preemption

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MemorySagaStore is an in-memory, thread-safe fake implementation of SagaStore for unit testing.
type MemorySagaStore struct {
	mu     sync.Mutex
	sagas  map[string]PreemptionSagaRecord
	claims map[string]ReceiverClaim
}

// NewMemorySagaStore creates a new MemorySagaStore instance.
func NewMemorySagaStore() *MemorySagaStore {
	return &MemorySagaStore{
		sagas:  make(map[string]PreemptionSagaRecord),
		claims: make(map[string]ReceiverClaim),
	}
}

func (m *MemorySagaStore) CreateSaga(ctx context.Context, record PreemptionSagaRecord) (PreemptionSagaRecord, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if saga with same SagaID exists
	if existing, ok := m.sagas[record.SagaID]; ok {
		if existing.ContractID == record.ContractID &&
			existing.PreparedTeardownHash == record.PreparedTeardownHash &&
			existing.ReceiverID == record.ReceiverID &&
			existing.Mode == record.Mode &&
			existing.ExpiresAt.Equal(record.ExpiresAt) {
			return existing, false, nil
		}
		return PreemptionSagaRecord{}, false, fmt.Errorf("%w: saga '%s' exists with mismatched content", ErrSagaIdentityConflict, record.SagaID)
	}

	// Check if active saga for same PreparedTeardownHash exists
	for _, existing := range m.sagas {
		if existing.PreparedTeardownHash == record.PreparedTeardownHash && !existing.State.IsTerminal() {
			return existing, false, ErrDuplicateSaga
		}
	}

	if record.Version == 0 {
		record.Version = 1
	}
	m.sagas[record.SagaID] = record
	return record, true, nil
}

func (m *MemorySagaStore) GetSaga(ctx context.Context, sagaID string) (PreemptionSagaRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	rec, ok := m.sagas[sagaID]
	if !ok {
		return PreemptionSagaRecord{}, ErrSagaNotFound
	}
	return rec, nil
}

func (m *MemorySagaStore) ClaimSagaExecution(
	ctx context.Context,
	sagaID string,
	expectedVersion uint64,
	receiverID string,
	now time.Time,
	leaseUntil time.Time,
	transition SagaTransition,
) (PreemptionSagaRecord, ReceiverClaim, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	saga, ok := m.sagas[sagaID]
	if !ok {
		return PreemptionSagaRecord{}, ReceiverClaim{}, ErrSagaNotFound
	}

	if saga.ReceiverID != receiverID {
		return PreemptionSagaRecord{}, ReceiverClaim{}, fmt.Errorf("%w: receiver '%s' != saga receiver '%s'", ErrFencingTokenMismatch, receiverID, saga.ReceiverID)
	}

	if saga.Version != expectedVersion {
		return PreemptionSagaRecord{}, ReceiverClaim{}, fmt.Errorf("%w: saga version %d != expected %d", ErrSagaVersionConflict, saga.Version, expectedVersion)
	}

	if saga.State != StatePrepared {
		return PreemptionSagaRecord{}, ReceiverClaim{}, fmt.Errorf("%w: cannot claim execution from state %s", ErrInvalidStateTransition, saga.State)
	}

	// Check current receiver claim
	currentClaim, claimExists := m.claims[receiverID]
	if claimExists {
		if currentClaim.Status == ClaimStatusQuarantined {
			return PreemptionSagaRecord{}, ReceiverClaim{}, fmt.Errorf("%w: receiver '%s' is quarantined", ErrReceiverClaimLocked, receiverID)
		}
		if currentClaim.Status == ClaimStatusClaimed && currentClaim.SagaID != sagaID {
			if !now.After(currentClaim.LeaseUntil) {
				return PreemptionSagaRecord{}, ReceiverClaim{}, fmt.Errorf("%w: receiver '%s' claimed by saga '%s' until %s", ErrReceiverClaimLocked, receiverID, currentClaim.SagaID, currentClaim.LeaseUntil)
			}
		}
	}

	newFencingToken := uint64(1)
	newClaimVersion := uint64(1)
	if claimExists {
		newFencingToken = currentClaim.FencingToken + 1
		newClaimVersion = currentClaim.ClaimVersion + 1
	}

	newClaim := ReceiverClaim{
		ReceiverID:   receiverID,
		SagaID:       sagaID,
		Status:       ClaimStatusClaimed,
		LeaseUntil:   leaseUntil,
		ClaimedAt:    now,
		ClaimVersion: newClaimVersion,
		FencingToken: newFencingToken,
	}

	// Update saga record (save original state for history)
	originalState := saga.State
	saga.State = StateExecutionClaimed
	saga.Version++
	saga.UpdatedAt = now
	transition.Sequence = nextSequence(saga.History)
	transition.From = originalState
	transition.To = StateExecutionClaimed
	transition.OccurredAt = now
	saga.History = append(saga.History, transition)

	m.sagas[sagaID] = saga
	m.claims[receiverID] = newClaim

	return saga, newClaim, nil
}

func (m *MemorySagaStore) CompareAndSwapState(
	ctx context.Context,
	sagaID string,
	expectedVersion uint64,
	receiverID string,
	fencingToken uint64,
	fromState PreemptionSagaState,
	toState PreemptionSagaState,
	transition SagaTransition,
) (PreemptionSagaRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Boundary check for Phase E3.3a: reject transitions to mutation states
	if toState == StateTeardownRequested || toState == StateTargetStopped || toState == StateResourcesReleased || toState == StateRequesterStarted || toState == StateCompleted {
		return PreemptionSagaRecord{}, ErrMutationPhaseDisabled
	}

	saga, ok := m.sagas[sagaID]
	if !ok {
		return PreemptionSagaRecord{}, ErrSagaNotFound
	}

	if saga.ReceiverID != receiverID {
		return PreemptionSagaRecord{}, fmt.Errorf("%w: receiver '%s' != saga receiver '%s'", ErrFencingTokenMismatch, receiverID, saga.ReceiverID)
	}

	if saga.Version != expectedVersion {
		return PreemptionSagaRecord{}, fmt.Errorf("%w: saga version %d != expected %d", ErrSagaVersionConflict, saga.Version, expectedVersion)
	}

	if saga.State != fromState {
		return PreemptionSagaRecord{}, fmt.Errorf("%w: current state %s != expected %s", ErrInvalidStateTransition, saga.State, fromState)
	}

	// Verify receiver claim & fencing token strictly
	claim, claimExists := m.claims[receiverID]
	if !claimExists || claim.Status != ClaimStatusClaimed || claim.SagaID != sagaID || claim.FencingToken != fencingToken {
		return PreemptionSagaRecord{}, fmt.Errorf("%w: claim mismatch for receiver '%s' (token %d)", ErrFencingTokenMismatch, receiverID, fencingToken)
	}

	originalState := saga.State
	saga.State = toState
	saga.Version++
	saga.UpdatedAt = transition.OccurredAt
	if saga.UpdatedAt.IsZero() {
		saga.UpdatedAt = time.Now().UTC()
	}
	transition.Sequence = nextSequence(saga.History)
	transition.From = originalState
	transition.To = toState
	if transition.OccurredAt.IsZero() {
		transition.OccurredAt = saga.UpdatedAt
	}
	saga.History = append(saga.History, transition)

	m.sagas[sagaID] = saga
	return saga, nil
}

func (m *MemorySagaStore) RenewReceiverClaim(
	ctx context.Context,
	receiverID string,
	sagaID string,
	fencingToken uint64,
	expectedClaimVersion uint64,
	now time.Time,
	leaseUntil time.Time,
) (ReceiverClaim, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	claim, ok := m.claims[receiverID]
	if !ok || claim.Status != ClaimStatusClaimed || claim.SagaID != sagaID || claim.FencingToken != fencingToken {
		return ReceiverClaim{}, fmt.Errorf("%w: claim mismatch for receiver '%s'", ErrFencingTokenMismatch, receiverID)
	}
	if claim.ClaimVersion != expectedClaimVersion {
		return ReceiverClaim{}, fmt.Errorf("%w: claim version %d != expected %d", ErrFencingTokenMismatch, claim.ClaimVersion, expectedClaimVersion)
	}
	if now.After(claim.LeaseUntil) {
		return ReceiverClaim{}, fmt.Errorf("%w: claim has expired at %s", ErrFencingTokenMismatch, claim.LeaseUntil)
	}

	claim.LeaseUntil = leaseUntil
	claim.ClaimVersion++
	m.claims[receiverID] = claim
	return claim, nil
}

func (m *MemorySagaStore) TransitionAndReleaseClaim(
	ctx context.Context,
	sagaID string,
	expectedVersion uint64,
	receiverID string,
	fencingToken uint64,
	toState PreemptionSagaState,
	transition SagaTransition,
) (PreemptionSagaRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	saga, ok := m.sagas[sagaID]
	if !ok {
		return PreemptionSagaRecord{}, ErrSagaNotFound
	}

	if saga.ReceiverID != receiverID {
		return PreemptionSagaRecord{}, fmt.Errorf("%w: receiver '%s' != saga receiver '%s'", ErrFencingTokenMismatch, receiverID, saga.ReceiverID)
	}

	if saga.Version != expectedVersion {
		return PreemptionSagaRecord{}, fmt.Errorf("%w: saga version %d != expected %d", ErrSagaVersionConflict, saga.Version, expectedVersion)
	}

	// Strictly verify claim ownership, status, and fencing token! If mismatch -> ZERO MUTATIONS!
	claim, claimExists := m.claims[receiverID]
	if !claimExists || claim.Status != ClaimStatusClaimed || claim.SagaID != sagaID || claim.FencingToken != fencingToken {
		return PreemptionSagaRecord{}, fmt.Errorf("%w: cannot release claim for receiver '%s' (fencing token mismatch)", ErrFencingTokenMismatch, receiverID)
	}

	// Release claim while preserving fencing_token counter
	claim.Status = ClaimStatusUnclaimed
	claim.SagaID = ""
	m.claims[receiverID] = claim

	// Record true original state in transition log
	originalState := saga.State
	saga.State = toState
	saga.Version++
	saga.UpdatedAt = transition.OccurredAt
	if saga.UpdatedAt.IsZero() {
		saga.UpdatedAt = time.Now().UTC()
	}
	transition.Sequence = nextSequence(saga.History)
	transition.From = originalState
	transition.To = toState
	if transition.OccurredAt.IsZero() {
		transition.OccurredAt = saga.UpdatedAt
	}
	saga.History = append(saga.History, transition)

	m.sagas[sagaID] = saga
	return saga, nil
}

func (m *MemorySagaStore) QuarantineReceiverClaim(
	ctx context.Context,
	receiverID string,
	sagaID string,
	fencingToken uint64,
	reason string,
	now time.Time,
) (ReceiverClaim, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	saga, ok := m.sagas[sagaID]
	if !ok || saga.ReceiverID != receiverID {
		return ReceiverClaim{}, fmt.Errorf("%w: saga '%s' mismatch for receiver '%s'", ErrSagaNotFound, sagaID, receiverID)
	}

	claim, ok := m.claims[receiverID]
	if !ok || claim.Status != ClaimStatusClaimed || claim.SagaID != sagaID || claim.FencingToken != fencingToken {
		return ReceiverClaim{}, fmt.Errorf("%w: claim mismatch for receiver '%s'", ErrFencingTokenMismatch, receiverID)
	}

	claim.Status = ClaimStatusQuarantined
	claim.ClaimedAt = now
	claim.ClaimVersion++
	m.claims[receiverID] = claim
	return claim, nil
}

func (m *MemorySagaStore) FindActiveByPreparedHash(ctx context.Context, preparedHash string) (PreemptionSagaRecord, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, saga := range m.sagas {
		if saga.PreparedTeardownHash == preparedHash && !saga.State.IsTerminal() {
			return saga, true, nil
		}
	}
	return PreemptionSagaRecord{}, false, nil
}

func (m *MemorySagaStore) GetReceiverClaim(ctx context.Context, receiverID string) (ReceiverClaim, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	claim, ok := m.claims[receiverID]
	if !ok {
		return ReceiverClaim{
			ReceiverID: receiverID,
			Status:     ClaimStatusUnclaimed,
		}, nil
	}
	return claim, nil
}

func (m *MemorySagaStore) ListNonTerminalSagas(ctx context.Context) ([]PreemptionSagaRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []PreemptionSagaRecord
	for _, saga := range m.sagas {
		if !saga.State.IsTerminal() {
			result = append(result, saga)
		}
	}
	return result, nil
}

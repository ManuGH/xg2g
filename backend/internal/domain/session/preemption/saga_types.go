// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package preemption

import (
	"time"
)

// PreemptionSagaState represents the lifecycle state machine of a preemption saga.
type PreemptionSagaState string

const (
	StatePrepared                 PreemptionSagaState = "PREPARED"
	StateExecutionClaimed         PreemptionSagaState = "EXECUTION_CLAIMED"
	StateTargetRevalidated        PreemptionSagaState = "TARGET_REVALIDATED"
	StateTeardownRequested        PreemptionSagaState = "TEARDOWN_REQUESTED"
	StateTargetStopped            PreemptionSagaState = "TARGET_STOPPED"
	StateResourcesReleased        PreemptionSagaState = "RESOURCES_RELEASED"
	StateRequesterStarted         PreemptionSagaState = "REQUESTER_STARTED"
	StateCompleted                PreemptionSagaState = "COMPLETED"
	StateFailedBeforeMutation     PreemptionSagaState = "FAILED_BEFORE_MUTATION"
	StateFailedDuringTeardown     PreemptionSagaState = "FAILED_DURING_TEARDOWN"
	StateFailedAfterTargetStop    PreemptionSagaState = "FAILED_AFTER_TARGET_STOP"
	StateCompensationRequired     PreemptionSagaState = "COMPENSATION_REQUIRED"
	StateCompensationSucceeded    PreemptionSagaState = "COMPENSATION_SUCCEEDED"
	StateCompensationFailed       PreemptionSagaState = "COMPENSATION_FAILED"
	StateManualInterventionNeeded PreemptionSagaState = "MANUAL_INTERVENTION_REQUIRED"
)

// IsTerminal returns true if the saga state is a final state.
func (s PreemptionSagaState) IsTerminal() bool {
	switch s {
	case StateCompleted, StateFailedBeforeMutation, StateCompensationSucceeded, StateCompensationFailed, StateManualInterventionNeeded:
		return true
	default:
		return false
	}
}

// ReceiverClaimStatus indicates status of the per-receiver mutation lock.
type ReceiverClaimStatus string

const (
	ClaimStatusUnclaimed   ReceiverClaimStatus = "UNCLAIMED"
	ClaimStatusClaimed     ReceiverClaimStatus = "CLAIMED"
	ClaimStatusQuarantined ReceiverClaimStatus = "QUARANTINED"
)

// ReceiverClaim represents the time-bounded lease and monotonic fencing token per receiver.
type ReceiverClaim struct {
	ReceiverID   string              `json:"receiverId"`
	SagaID       string              `json:"sagaId"`
	Status       ReceiverClaimStatus `json:"status"`
	LeaseUntil   time.Time           `json:"leaseUntil"`
	ClaimedAt    time.Time           `json:"claimedAt"`
	ClaimVersion uint64              `json:"claimVersion"`
	FencingToken uint64              `json:"fencingToken"`
}

// RequesterReservation represents a short-lived execution reservation for the requester.
type RequesterReservation struct {
	ReservationID         string          `json:"reservationId"`
	RequestID             string          `json:"requestId"`
	ReceiverID            string          `json:"receiverId"`
	Owner                 string          `json:"owner"`
	ContractID            string          `json:"contractId"`
	PreparedTeardownHash  string          `json:"preparedTeardownHash"`
	RequesterAllocationID string          `json:"requesterAllocationId"`
	ResourceClaims        []ResourceClaim `json:"resourceClaims"`
	Revision              string          `json:"revision"`
	ExpiresAt             time.Time       `json:"expiresAt"`
}

// SagaReasonCode represents domain classification for saga transitions.
type SagaReasonCode string

const (
	SagaReasonCreated                 SagaReasonCode = "SAGA_CREATED"
	SagaReasonReceiverClaimed         SagaReasonCode = "SAGA_RECEIVER_CLAIMED"
	SagaReasonTargetRevalidated       SagaReasonCode = "SAGA_TARGET_REVALIDATED"
	SagaReasonRequesterDropped        SagaReasonCode = "SAGA_REQUESTER_DROPPED"
	SagaReasonTargetMutated           SagaReasonCode = "SAGA_TARGET_MUTATED"
	SagaReasonContractExpired         SagaReasonCode = "SAGA_CONTRACT_EXPIRED"
	SagaReasonPreemptionModeAuditOnly SagaReasonCode = "SAGA_PREEMPTION_MODE_AUDIT_ONLY"
	SagaReasonPhaseNotEnabled         SagaReasonCode = "SAGA_PHASE_NOT_ENABLED"
)

// SagaTransition represents an immutable audit log entry for a state transition.
type SagaTransition struct {
	Sequence   uint64              `json:"sequence"`
	From       PreemptionSagaState `json:"from"`
	To         PreemptionSagaState `json:"to"`
	ReasonCode SagaReasonCode      `json:"reasonCode"`
	OccurredAt time.Time           `json:"occurredAt"`
	Actor      string              `json:"actor"`
	Details    string              `json:"details,omitempty"`
}

// PreemptionSagaRecord is the persisted state record of a preemption saga.
type PreemptionSagaRecord struct {
	SagaID               string              `json:"sagaId"`
	ReceiverID           string              `json:"receiverId"`
	ContractID           string              `json:"contractId"`
	PreparedTeardownHash string              `json:"preparedTeardownHash"`
	State                PreemptionSagaState `json:"state"`
	Mode                 PreemptionMode      `json:"mode"`
	Version              uint64              `json:"version"`
	CreatedAt            time.Time           `json:"createdAt"`
	UpdatedAt            time.Time           `json:"updatedAt"`
	ExpiresAt            time.Time           `json:"expiresAt"`
	History              []SagaTransition    `json:"history"`
}

// RecoveryDisposition classifies sagas during fail-closed recovery.
type RecoveryDisposition string

const (
	RecoveryBlockedActiveClaim      RecoveryDisposition = "BLOCKED_ACTIVE_CLAIM"
	RecoveryEligibleForRevalidation RecoveryDisposition = "ELIGIBLE_FOR_REVALIDATION"
	RecoveryQuarantined             RecoveryDisposition = "QUARANTINED"
	RecoveryInconsistent            RecoveryDisposition = "INCONSISTENT"
)

// nextSequence is the 1-based position of the transition about to be appended.
//
// The conversion is total and gosec cannot see it: len is non-negative by
// definition, and a slice length is bounded by addressable memory, so it is
// nowhere near what a uint64 stops holding. Written once here rather than
// annotated at each of the six append sites, where six suppressions would be
// six places for one of them to stop being true unnoticed.
func nextSequence(history []SagaTransition) uint64 {
	return uint64(len(history)) + 1 // #nosec G115 -- len is never negative.
}

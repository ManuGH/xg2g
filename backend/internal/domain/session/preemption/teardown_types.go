// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package preemption

import (
	"time"
)

// TeardownPreparationReasonCode represents domain classification for teardown preparation outcomes.
type TeardownPreparationReasonCode string

const (
	ReasonTeardownApproved                      TeardownPreparationReasonCode = "TEARDOWN_PREPARATION_APPROVED"
	ReasonTeardownContractExpired               TeardownPreparationReasonCode = "TEARDOWN_PREPARATION_CONTRACT_EXPIRED"
	ReasonTeardownContractHashInvalid           TeardownPreparationReasonCode = "TEARDOWN_PREPARATION_CONTRACT_HASH_INVALID"
	ReasonTeardownSnapshotRevisionMismatch      TeardownPreparationReasonCode = "TEARDOWN_PREPARATION_SNAPSHOT_REVISION_MISMATCH"
	ReasonTeardownHardwareRevisionMismatch      TeardownPreparationReasonCode = "TEARDOWN_PREPARATION_HARDWARE_PROFILE_REVISION_MISMATCH"
	ReasonTeardownConflictProofRevisionMismatch TeardownPreparationReasonCode = "TEARDOWN_PREPARATION_CONFLICT_PROOF_REVISION_MISMATCH"
	ReasonTeardownTargetStateMutated            TeardownPreparationReasonCode = "TEARDOWN_PREPARATION_TARGET_STATE_MUTATED"
	ReasonTeardownTargetProtected               TeardownPreparationReasonCode = "TEARDOWN_PREPARATION_TARGET_PROTECTED"
	ReasonTeardownStaleHardware                 TeardownPreparationReasonCode = "TEARDOWN_PREPARATION_STALE_HARDWARE"
	ReasonTeardownInvalidProof                  TeardownPreparationReasonCode = "TEARDOWN_PREPARATION_INVALID_PROOF"
)

// ExpectedLeaseBinding defines concrete expected lease bindings bound in E3.2.
type ExpectedLeaseBinding struct {
	LeaseKind       ResourceKind `json:"leaseKind"`
	Resource        string       `json:"resource"`
	ScopeID         string       `json:"scopeId"`
	ExpectedOwnerID string       `json:"expectedOwnerId"`
	Quantity        int          `json:"quantity"`
}

// ExpectedHardwareBinding defines concrete expected hardware bindings bound in E3.2.
type ExpectedHardwareBinding struct {
	Kind         ResourceKind `json:"kind"`
	Resource     string       `json:"resource"`
	Quantity     int          `json:"quantity"`
	AllocationID string       `json:"allocationId"`
}

// TargetExecutionDescriptor specifies immutable expected target attributes bound in E3.2.
type TargetExecutionDescriptor struct {
	AllocationID             string                    `json:"allocationId"`
	ExpectedOwner            string                    `json:"expectedOwner"`
	AllocationRevision       string                    `json:"allocationRevision"`
	SnapshotRevision         string                    `json:"snapshotRevision"`
	HardwareRevision         string                    `json:"hardwareRevision"`
	ExpectedClaims            []ResourceClaim           `json:"expectedClaims"`
	ExpectedHardwareClaims    []ResourceClaim           `json:"expectedHardwareClaims"`
	ExpectedHardwareBindings []ExpectedHardwareBinding `json:"expectedHardwareBindings"`
	ExpectedLeaseBindings    []ExpectedLeaseBinding    `json:"expectedLeaseBindings"`
	DescriptorHash           string                    `json:"descriptorHash"`
}

// PreparedTeardown is the immutable read-only protocol dataset verifying teardown readiness before mutation.
type PreparedTeardown struct {
	TeardownID              string                      `json:"teardownId"`
	ContractID              string                      `json:"contractId"`
	ReceiverID              string                      `json:"receiverId"`
	RequestID               string                      `json:"requestId"`
	ContractHash            string                      `json:"contractHash"`
	TargetAllocationIDs     []string                    `json:"targetAllocationIds"`
	TargetDescriptors       []TargetExecutionDescriptor `json:"targetDescriptors"`
	TargetDescriptorsHash  string                      `json:"targetDescriptorsHash"`
	SnapshotRevision        string                      `json:"snapshotRevision"`
	HardwareProfileRevision string                      `json:"hardwareProfileRevision"`
	ConflictProofRevision   string                      `json:"conflictProofRevision"`
	PreparedAt              time.Time                   `json:"preparedAt"`
	ExpiresAt               time.Time                   `json:"expiresAt"`
	PreparedTeardownHash    string                      `json:"preparedTeardownHash"`
}

// TeardownPreparationResult represents the domain outcome of teardown preparation.
type TeardownPreparationResult struct {
	Decision Decision                      `json:"decision"`
	Reason   TeardownPreparationReasonCode `json:"reason"`
	Prepared *PreparedTeardown             `json:"prepared,omitempty"`
}

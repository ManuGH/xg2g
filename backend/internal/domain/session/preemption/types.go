// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package preemption

import (
	"time"
)

// Decision represents the candidate selection decision outcome.
type Decision string

const (
	DecisionApproved Decision = "APPROVED"
	DecisionRejected Decision = "REJECTED"
)

// PreemptionReasonCode represents domain classification of selection outcomes.
type PreemptionReasonCode string

const (
	ReasonApproved                     PreemptionReasonCode = "PREEMPTION_APPROVED"
	ReasonRejectedUnmappedHardware     PreemptionReasonCode = "PREEMPTION_REJECTED_UNMAPPED_HARDWARE"
	ReasonRejectedNoEligibleVictim     PreemptionReasonCode = "PREEMPTION_REJECTED_NO_ELIGIBLE_VICTIM"
	ReasonRejectedProtectedTarget      PreemptionReasonCode = "PREEMPTION_REJECTED_PROTECTED_TARGET"
	ReasonRejectedInsufficientPriority PreemptionReasonCode = "PREEMPTION_REJECTED_INSUFFICIENT_PRIORITY"
	ReasonRejectedRevisionMismatch     PreemptionReasonCode = "PREEMPTION_REJECTED_REVISION_MISMATCH"
	ReasonRejectedInvalidProof         PreemptionReasonCode = "PREEMPTION_REJECTED_INVALID_PROOF"
)

// ResourceKind identifies physical or virtual capacity.
type ResourceKind string

const (
	ResourceKindRestrictedAccessSlot ResourceKind = "RESTRICTED_ACCESS_SLOT"
	ResourceKindTunerSlot            ResourceKind = "TUNER_SLOT"
	ResourceKindDemux                ResourceKind = "DEMUX"
	ResourceKindPhysicalTuner        ResourceKind = "PHYSICAL_TUNER"
)

// ResourceClaim specifies a single resource requirement.
type ResourceClaim struct {
	Kind     ResourceKind `json:"kind"`
	Resource string       `json:"resource"`
	Quantity int          `json:"quantity"`
}

// PriorityAttributes separates session importance from resource claims.
type PriorityAttributes struct {
	BasePriority   int       `json:"basePriority"`
	UserProtected  bool      `json:"userProtected"`
	Sacrosanct     bool      `json:"sacrosanct"`
	Foreground     bool      `json:"foreground"`
	StartedAt      time.Time `json:"startedAt"`
	PreemptionCost int       `json:"preemptionCost"`
}

// ActiveAllocation represents an existing active session allocation in the snapshot.
type ActiveAllocation struct {
	AllocationID string             `json:"allocationId"`
	Owner        string             `json:"owner"`
	Priority     PriorityAttributes `json:"priority"`
	Claims       []ResourceClaim    `json:"claims"`
}

// PreemptionRequest specifies the incoming session preemption request.
type PreemptionRequest struct {
	RequestID          string             `json:"requestId"`
	ReceiverID         string             `json:"receiverId"`
	RequesterOwner     string             `json:"requesterOwner"`
	RequesterPriority  PriorityAttributes `json:"requesterPriority"`
	RequestedResources []ResourceClaim    `json:"requestedResources"`
}

// ResourceSnapshot represents the immutable state snapshot.
type ResourceSnapshot struct {
	ReceiverID       string             `json:"receiverId"`
	SnapshotRevision string             `json:"snapshotRevision"`
	ObservedAt       time.Time          `json:"observedAt"`
	Allocations      []ActiveAllocation `json:"allocations"`
}

// EvidenceClassification indicates proof reliability.
type EvidenceClassification string

const (
	EvidenceDirectObservation EvidenceClassification = "DIRECT_OBSERVATION"
	EvidenceInferred          EvidenceClassification = "INFERRED"
)

// AllocationResourceMapping maps an allocation ID to physical/virtual resources.
type AllocationResourceMapping struct {
	AllocationID   string          `json:"allocationId"`
	FreedResources []ResourceClaim `json:"freedResources"`
}

// ConflictResolutionProof provides structural proof of physical resource resolution.
type ConflictResolutionProof struct {
	ReceiverID              string                      `json:"receiverId"`
	SnapshotRevision        string                      `json:"snapshotRevision"`
	HardwareProfileRevision string                      `json:"hardwareProfileRevision"`
	RequestedResources      []ResourceClaim             `json:"requestedResources"`
	AllocationMappings      []AllocationResourceMapping `json:"allocationMappings"`
	FreedResourcesByTarget  map[string][]ResourceClaim `json:"freedResourcesByTarget"`
	EvidenceClassification  EvidenceClassification      `json:"evidenceClassification"`
}

// PreemptionExecutionContract is the immutable contract for approved preemption.
type PreemptionExecutionContract struct {
	ContractID              string          `json:"contractId"`
	ReceiverID              string          `json:"receiverId"`
	RequestID               string          `json:"requestId"`
	TargetAllocationIDs     []string        `json:"targetAllocationIds"`
	RequestedResources      []ResourceClaim `json:"requestedResources"`
	ExpectedFreedResources  []ResourceClaim `json:"expectedFreedResources"`
	SnapshotRevision        string          `json:"snapshotRevision"`
	HardwareProfileRevision string          `json:"hardwareProfileRevision"`
	ConflictProofRevision   string          `json:"conflictProofRevision"`
	CreatedAt               time.Time       `json:"createdAt"`
	ExpiresAt               time.Time       `json:"expiresAt"`
	ContractHash            string          `json:"contractHash"`
}

// SelectionResult represents the domain outcome of plan selection.
type SelectionResult struct {
	Decision Decision                     `json:"decision"`
	Reason   PreemptionReasonCode         `json:"reason"`
	Contract *PreemptionExecutionContract `json:"contract,omitempty"`
}

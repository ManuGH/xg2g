// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package preemption

import (
	"time"
)

type TargetReleaseState string

const (
	TargetReleased       TargetReleaseState = "TARGET_RELEASED"
	TargetStillActive    TargetReleaseState = "TARGET_STILL_ACTIVE"
	TargetBindingRemains TargetReleaseState = "TARGET_BINDING_REMAINS"
)

type ResourceDisposition string

const (
	ResourceCurrentlyFree ResourceDisposition = "CURRENTLY_FREE"
	ResourceOtherObserved ResourceDisposition = "OTHER_OBSERVED"
	ResourceUnknown       ResourceDisposition = "UNKNOWN"
)

type ReleaseVerificationDecision string

const (
	ReleaseDecisionReleased ReleaseVerificationDecision = "RELEASE_DECISION_RELEASED"
	ReleaseDecisionRejected ReleaseVerificationDecision = "RELEASE_DECISION_REJECTED"
)

type ReleaseVerificationReason string

const (
	ReleaseVerificationReasonNone               ReleaseVerificationReason = "NONE"
	ReleaseVerificationReasonEvidenceStale      ReleaseVerificationReason = "EVIDENCE_STALE"
	ReleaseVerificationReasonEvidenceIncomplete ReleaseVerificationReason = "EVIDENCE_INCOMPLETE"
	ReleaseVerificationReasonEvidenceMalformed  ReleaseVerificationReason = "EVIDENCE_MALFORMED"
	ReleaseVerificationReasonRevisionMismatch   ReleaseVerificationReason = "EVIDENCE_REVISION_MISMATCH"
	ReleaseVerificationReasonBijectionInvalid   ReleaseVerificationReason = "EVIDENCE_BIJECTION_INVALID"
	ReleaseVerificationReasonTargetsNotReleased ReleaseVerificationReason = "TARGETS_NOT_RELEASED"
)

type ReleaseReasonCode string

const (
	ReleaseReasonNone                  ReleaseReasonCode = "NONE"
	ReleaseReasonTargetStillObserved   ReleaseReasonCode = "TARGET_STILL_OBSERVED"
	ReleaseReasonHardwareBinding       ReleaseReasonCode = "HARDWARE_BINDING_REMAINS"
	ReleaseReasonLeaseBinding          ReleaseReasonCode = "LEASE_BINDING_REMAINS"
	ReleaseReasonObservationStale      ReleaseReasonCode = "OBSERVATION_STALE"
	ReleaseReasonObservationIncomplete ReleaseReasonCode = "OBSERVATION_INCOMPLETE"
	ReleaseReasonObservationMalformed  ReleaseReasonCode = "OBSERVATION_MALFORMED"
)

type ObservedResourceBinding struct {
	Kind         ResourceKind `json:"kind"`
	Resource     string       `json:"resource"`
	Quantity     int          `json:"quantity"`
	AllocationID string       `json:"allocationId"`
}

type ObservedLeaseBinding struct {
	LeaseKind ResourceKind `json:"leaseKind"`
	Resource  string       `json:"resource"`
	ScopeID   string       `json:"scopeId"`
	OwnerID   string       `json:"ownerId"`
	Quantity  int          `json:"quantity"`
}

type ObservationCoverage struct {
	AllocationsComplete      bool `json:"allocationsComplete"`
	HardwareBindingsComplete bool `json:"hardwareBindingsComplete"`
	LeaseBindingsComplete    bool `json:"leaseBindingsComplete"`
}

type ReleaseObservation struct {
	ReceiverID               string                    `json:"receiverId"`
	ObservationRevision      string                    `json:"observationRevision"`
	AllocationSourceRevision string                    `json:"allocationSourceRevision"`
	HardwareSourceRevision   string                    `json:"hardwareSourceRevision"`
	LeaseSourceRevision      string                    `json:"leaseSourceRevision"`
	HardwareProfileRevision  string                    `json:"hardwareProfileRevision"`
	Evidence                 EvidenceClassification    `json:"evidence"`
	Coverage                 ObservationCoverage       `json:"coverage"`
	ObservedAt               time.Time                 `json:"observedAt"`
	ActiveAllocations        []ActiveAllocation        `json:"activeAllocations"`
	ActiveHardwareBindings   []ObservedResourceBinding `json:"activeHardwareBindings"`
	ActiveLeaseBindings      []ObservedLeaseBinding    `json:"activeLeaseBindings"`
	ObservationHash          string                    `json:"observationHash"`
}

type TargetTeardownEvidence struct {
	SagaID               string         `json:"sagaId"`
	PreparedTeardownHash string         `json:"preparedTeardownHash"`
	TargetAllocationID   string         `json:"targetAllocationId"`
	DescriptorHash       string         `json:"descriptorHash"`
	FencingToken         uint64         `json:"fencingToken"`
	Status               TeardownStatus `json:"status"`
	AttemptedAt          time.Time      `json:"attemptedAt"`
	AcknowledgedAt       time.Time      `json:"acknowledgedAt"`
	EvidenceHash         string         `json:"evidenceHash"`
}

type ResourceReleaseResult struct {
	Resource                   ResourceClaim       `json:"resource"`
	ExpectedQuantity           int                 `json:"expectedQuantity"`
	RemainingTargetQuantity    int                 `json:"remainingTargetQuantity"`
	ReleasedFromTargetQuantity int                 `json:"releasedFromTargetQuantity"`
	OtherObservedQuantity      int                 `json:"otherObservedQuantity"`
	CurrentlyFreeQuantity      int                 `json:"currentlyFreeQuantity,omitempty"`
	CurrentlyFreeAuthoritative bool                `json:"currentlyFreeAuthoritative"`
	Disposition                ResourceDisposition `json:"disposition"`
}

type SingleTargetReleaseResult struct {
	TargetAllocationID string                  `json:"targetAllocationId"`
	ReleaseState       TargetReleaseState      `json:"releaseState"`
	Reason             ReleaseReasonCode       `json:"reason"`
	Resources          []ResourceReleaseResult `json:"resources,omitempty"`
}

type ReleaseVerificationResult struct {
	Decision                   ReleaseVerificationDecision `json:"decision"`
	Reason                     ReleaseVerificationReason   `json:"reason"`
	TargetResults              []SingleTargetReleaseResult `json:"targetResults"`
	ReleasedFromTargetClaims   []ResourceClaim             `json:"releasedFromTargetClaims"`
	CurrentlyFreeClaims        []ResourceClaim             `json:"currentlyFreeClaims,omitempty"`
	ReassignedHardwareBindings []ObservedResourceBinding   `json:"reassignedHardwareBindings,omitempty"`
	ReassignedLeaseBindings    []ObservedLeaseBinding      `json:"reassignedLeaseBindings,omitempty"`
	ObservationRevision        string                      `json:"observationRevision,omitempty"`
	VerifiedAt                 time.Time                   `json:"verifiedAt,omitempty"`
}

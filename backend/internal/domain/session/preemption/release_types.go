// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package preemption

import (
	"time"
)

// TargetReleaseState defines the release state of a single victim allocation.
type TargetReleaseState string

const (
	TargetReleased       TargetReleaseState = "TARGET_RELEASED"
	TargetStillActive    TargetReleaseState = "TARGET_STILL_ACTIVE"
	TargetBindingRemains TargetReleaseState = "TARGET_BINDING_REMAINS"
)

func (s TargetReleaseState) IsValid() bool {
	switch s {
	case TargetReleased, TargetStillActive, TargetBindingRemains:
		return true
	default:
		return false
	}
}

// ResourceDisposition describes current post-teardown availability of a resource.
type ResourceDisposition string

const (
	ResourceCurrentlyFree ResourceDisposition = "CURRENTLY_FREE"
	ResourceReassigned    ResourceDisposition = "REASSIGNED"
	ResourceUnknown       ResourceDisposition = "UNKNOWN"
)

func (d ResourceDisposition) IsValid() bool {
	switch d {
	case ResourceCurrentlyFree, ResourceReassigned, ResourceUnknown:
		return true
	default:
		return false
	}
}

// ReleaseVerificationDecision is the top-level decision of post-teardown verification.
type ReleaseVerificationDecision string

const (
	ReleaseDecisionReleased ReleaseVerificationDecision = "RELEASE_DECISION_RELEASED"
	ReleaseDecisionRejected ReleaseVerificationDecision = "RELEASE_DECISION_REJECTED"
)

// ReleaseVerificationReason classifies global evidence or protocol verification failures.
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

func (r ReleaseVerificationReason) IsValid() bool {
	switch r {
	case ReleaseVerificationReasonNone, ReleaseVerificationReasonEvidenceStale, ReleaseVerificationReasonEvidenceIncomplete,
		ReleaseVerificationReasonEvidenceMalformed, ReleaseVerificationReasonRevisionMismatch,
		ReleaseVerificationReasonBijectionInvalid, ReleaseVerificationReasonTargetsNotReleased:
		return true
	default:
		return false
	}
}

// ReleaseReasonCode specifies single-target release diagnostic codes.
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

func (c ReleaseReasonCode) IsValid() bool {
	switch c {
	case ReleaseReasonNone, ReleaseReasonTargetStillObserved, ReleaseReasonHardwareBinding,
		ReleaseReasonLeaseBinding, ReleaseReasonObservationStale, ReleaseReasonObservationIncomplete,
		ReleaseReasonObservationMalformed:
		return true
	default:
		return false
	}
}

// ObservedResourceBinding represents a live observed hardware resource binding.
type ObservedResourceBinding struct {
	Kind         ResourceKind `json:"kind"`
	Resource     string       `json:"resource"`
	Quantity     int          `json:"quantity"`
	AllocationID string       `json:"allocationId"`
}

// ObservedLeaseBinding represents a live observed lease resource binding.
type ObservedLeaseBinding struct {
	LeaseKind ResourceKind `json:"leaseKind"`
	Resource  string       `json:"resource"`
	ScopeID   string       `json:"scopeId"`
	OwnerID   string       `json:"ownerId"`
	Quantity  int          `json:"quantity"`
}

// ObservationCoverage asserts completeness of live observation data sources.
type ObservationCoverage struct {
	AllocationsComplete      bool `json:"allocationsComplete"`
	HardwareBindingsComplete bool `json:"hardwareBindingsComplete"`
	LeaseBindingsComplete    bool `json:"leaseBindingsComplete"`
}

// ReleaseObservation represents the read-only post-teardown observation snapshot.
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

// TargetTeardownEvidence is the factual evidence of an executed E3.3b teardown attempt.
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

// ResourceReleaseResult details single-resource release state and post-teardown disposition.
type ResourceReleaseResult struct {
	Resource    ResourceClaim       `json:"resource"`
	Disposition ResourceDisposition `json:"disposition"`
}

// SingleTargetReleaseResult details release verification outcome for a single target allocation.
type SingleTargetReleaseResult struct {
	TargetAllocationID string                  `json:"targetAllocationId"`
	ReleaseState       TargetReleaseState      `json:"releaseState"`
	Reason             ReleaseReasonCode       `json:"reason"`
	Resources          []ResourceReleaseResult `json:"resources,omitempty"`
}

// ReleaseVerificationResult represents the complete domain outcome of E3.3c post-teardown verification.
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

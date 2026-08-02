package policy

import (
	"errors"
	"time"
)

// ConsumerType identifies the operational classification of a resource consumer.
type ConsumerType string

const (
	ConsumerScheduledRecording ConsumerType = "SCHEDULED_RECORDING"
	ConsumerManualRecording    ConsumerType = "MANUAL_RECORDING"
	ConsumerLiveTV             ConsumerType = "LIVE_TV"
	ConsumerRetroDVR           ConsumerType = "RETRO_DVR"
	ConsumerChannelScan        ConsumerType = "CHANNEL_SCAN"
	ConsumerBackgroundTransfer ConsumerType = "BACKGROUND_TRANSFER"
)

// AllConsumerTypes lists all supported consumer types for matrix completeness verification.
var AllConsumerTypes = []ConsumerType{
	ConsumerScheduledRecording,
	ConsumerManualRecording,
	ConsumerLiveTV,
	ConsumerRetroDVR,
	ConsumerChannelScan,
	ConsumerBackgroundTransfer,
}

// IsValid checks whether the consumer type is a recognized system consumer.
func (c ConsumerType) IsValid() bool {
	switch c {
	case ConsumerScheduledRecording, ConsumerManualRecording, ConsumerLiveTV,
		ConsumerRetroDVR, ConsumerChannelScan, ConsumerBackgroundTransfer:
		return true
	default:
		return false
	}
}

// LossClass defines a typed loss cost rank for candidate tie-breaking (lower = lower loss, preempt first).
type LossClass uint8

const (
	LossBackground LossClass = 10
	LossScan       LossClass = 20
	LossRetroDVR   LossClass = 30
	LossLiveTV     LossClass = 40
	LossManual     LossClass = 50
	LossScheduled  LossClass = 60
)

// ResourceKind identifies the discrete hardware or system resource type being evaluated.
type ResourceKind string

const (
	ResourceTuner       ResourceKind = "TUNER"
	ResourceDemuxer     ResourceKind = "DEMUXER"
	ResourceEncoderSlot ResourceKind = "ENCODER_SLOT"
	ResourceStorageIO   ResourceKind = "STORAGE_IO"
)

// IsValid checks whether the resource kind is a recognized system resource.
func (r ResourceKind) IsValid() bool {
	switch r {
	case ResourceTuner, ResourceDemuxer, ResourceEncoderSlot, ResourceStorageIO:
		return true
	default:
		return false
	}
}

// RequiresResource returns whether a consumer type competes for the specified resource kind.
func RequiresResource(consumer ConsumerType, kind ResourceKind) bool {
	switch kind {
	case ResourceTuner, ResourceDemuxer:
		// Background transfers work directly with storage and do NOT occupy tuners/demuxers
		return consumer != ConsumerBackgroundTransfer
	case ResourceEncoderSlot:
		return consumer == ConsumerLiveTV || consumer == ConsumerManualRecording || consumer == ConsumerScheduledRecording
	case ResourceStorageIO:
		return true
	default:
		return false
	}
}

// PreemptionDecision defines the action decreed by the PolicyEngine.
type PreemptionDecision string

const (
	DecisionGrant              PreemptionDecision = "GRANT"
	DecisionReject             PreemptionDecision = "REJECT"
	DecisionPreemptionRequired PreemptionDecision = "PREEMPTION_REQUIRED"
)

// ReasonCode defines typed, machine-readable audit reason codes for policy evaluations.
type ReasonCode string

const (
	ReasonPolicyGrantedResourceAvailable      ReasonCode = "POLICY_GRANTED_RESOURCE_AVAILABLE"
	ReasonPolicyRejectedProtectedActivity     ReasonCode = "POLICY_REJECTED_PROTECTED_ACTIVITY"
	ReasonPolicyRejectedEqualOrLowerPriority  ReasonCode = "POLICY_REJECTED_EQUAL_OR_LOWER_PRIORITY"
	ReasonPolicyRejectedResourceNotRequired   ReasonCode = "POLICY_REJECTED_RESOURCE_NOT_REQUIRED"
	ReasonPolicyRejectedNoCompatibleCandidate ReasonCode = "POLICY_REJECTED_NO_COMPATIBLE_CANDIDATE"
	ReasonPolicyPreemptionRequired            ReasonCode = "POLICY_PREEMPTION_REQUIRED"
	ReasonPolicyInvalidInput                  ReasonCode = "POLICY_INVALID_INPUT"
)

var (
	ErrEvaluationTimeRequired         = errors.New("evaluating timestamp (EvaluatedAt) is required")
	ErrInvalidConsumerType            = errors.New("invalid or unrecognized consumer type")
	ErrInvalidResourceKind            = errors.New("invalid or unrecognized resource kind")
	ErrResourceKindMismatch           = errors.New("request resource kind does not match snapshot resource kind")
	ErrInvalidOwner                   = errors.New("request owner cannot be empty")
	ErrInvalidCandidateScope          = errors.New("candidate scope cannot be empty")
	ErrDuplicateCandidateScope        = errors.New("duplicate candidate scope")
	ErrMissingAllocationOwner         = errors.New("allocation owner cannot be empty")
	ErrMissingAllocationScope         = errors.New("allocation scope cannot be empty")
	ErrAllocationScopeNotFound        = errors.New("allocation scope not found in snapshot candidates")
	ErrInconsistentCandidateState     = errors.New("candidate marked available despite active allocation on scope")
	ErrMultipleActiveOnExclusiveScope = errors.New("multiple active allocations on exclusive candidate scope")
	ErrZeroAcquiredAtTimestamp        = errors.New("allocation AcquiredAt timestamp cannot be zero")
	ErrTargetScopeNotFound            = errors.New("target scope not found in snapshot candidates")
	ErrTargetScopeIncompatible        = errors.New("target scope is marked incompatible")
	ErrInvalidAllocationID            = errors.New("allocation ID cannot be empty or duplicate")
	ErrInvalidSnapshotCapacity        = errors.New("invalid snapshot capacity or active count exceeds capacity")
)

// ResourceCandidate represents a physical or logical resource option.
type ResourceCandidate struct {
	Scope      string
	Compatible bool
	Available  bool
}

// ResourceAllocation captures an existing active allocation for policy evaluation.
type ResourceAllocation struct {
	AllocationID string
	Consumer     ConsumerType
	Owner        string
	Scope        string
	AcquiredAt   time.Time
	IsSacrosanct bool
	IsReleasing  bool
}

// ResourceSnapshot is an externally-assembled, typed snapshot of resource state.
type ResourceSnapshot struct {
	Kind       ResourceKind
	Capacity   int
	Candidates []ResourceCandidate
	Active     []ResourceAllocation
}

// EvaluationRequest represents an incoming request for a resource.
type EvaluationRequest struct {
	Consumer     ConsumerType
	ResourceKind ResourceKind
	Owner        string
	TargetScope  string
	EvaluatedAt  time.Time
	TTL          time.Duration
}

// EvaluationResult represents the pure decision returned by the PolicyEngine.
type EvaluationResult struct {
	Decision              PreemptionDecision
	ReasonCode            ReasonCode
	ReasonDetail          string
	SelectedScope         string   // Scope assigned if Decision == DecisionGrant or DecisionPreemptionRequired
	TargetAllocationID    string   // Allocation to be preempted if Decision == DecisionPreemptionRequired
	BlockingAllocationIDs []string // Active allocations preventing immediate grant or preemption
	EvaluatedAt           time.Time
}

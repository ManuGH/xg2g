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

// LossClass returns the relative loss cost rank for tie-breaking candidate selection (lower = lower loss, preempt first).
func (c ConsumerType) LossClass() int {
	switch c {
	case ConsumerBackgroundTransfer:
		return 10
	case ConsumerChannelScan:
		return 20
	case ConsumerRetroDVR:
		return 30
	case ConsumerLiveTV:
		return 40
	case ConsumerManualRecording:
		return 50
	case ConsumerScheduledRecording:
		return 60
	default:
		return 0
	}
}

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
	ReasonPolicyGrantedResourceAvailable     ReasonCode = "POLICY_GRANTED_RESOURCE_AVAILABLE"
	ReasonPolicyRejectedProtectedActivity    ReasonCode = "POLICY_REJECTED_PROTECTED_ACTIVITY"
	ReasonPolicyRejectedEqualOrLowerPriority ReasonCode = "POLICY_REJECTED_EQUAL_OR_LOWER_PRIORITY"
	ReasonPolicyRejectedResourceNotRequired  ReasonCode = "POLICY_REJECTED_RESOURCE_NOT_REQUIRED"
	ReasonPolicyPreemptionRequired           ReasonCode = "POLICY_PREEMPTION_REQUIRED"
	ReasonPolicyInvalidInput                 ReasonCode = "POLICY_INVALID_INPUT"
)

var (
	ErrEvaluationTimeRequired  = errors.New("evaluating timestamp (EvaluatedAt) is required")
	ErrInvalidConsumerType     = errors.New("invalid or unrecognized consumer type")
	ErrInvalidResourceKind     = errors.New("invalid or unrecognized resource kind")
	ErrInvalidOwner            = errors.New("request owner cannot be empty")
	ErrInvalidAllocationID     = errors.New("allocation ID cannot be empty or duplicate")
	ErrInvalidSnapshotCapacity = errors.New("invalid snapshot capacity or active count exceeds capacity")
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
	Decision           PreemptionDecision
	ReasonCode         ReasonCode
	ReasonDetail       string
	TargetAllocationID string // Allocation to be preempted if Decision == DecisionPreemptionRequired
	EvaluatedAt        time.Time
}

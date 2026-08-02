package policy

import (
	"errors"
	"time"
)

// ConsumerType identifies the nature and operational priority of a tuner resource consumer.
type ConsumerType string

const (
	ConsumerScheduledRecording ConsumerType = "SCHEDULED_RECORDING"
	ConsumerManualRecording    ConsumerType = "MANUAL_RECORDING"
	ConsumerLiveTV             ConsumerType = "LIVE_TV"
	ConsumerRetroDVR           ConsumerType = "RETRO_DVR"
	ConsumerChannelScan        ConsumerType = "CHANNEL_SCAN"
	ConsumerBackgroundTransfer ConsumerType = "BACKGROUND_TRANSFER"
)

// PriorityWeight returns the numeric priority weight for a consumer type (higher = higher priority).
func (c ConsumerType) PriorityWeight() int {
	switch c {
	case ConsumerScheduledRecording:
		return 60
	case ConsumerManualRecording:
		return 50
	case ConsumerLiveTV:
		return 40
	case ConsumerRetroDVR:
		return 30
	case ConsumerChannelScan:
		return 20
	case ConsumerBackgroundTransfer:
		return 10
	default:
		return 0
	}
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

// PreemptionDecision defines the action decreed by the PolicyEngine.
type PreemptionDecision string

const (
	DecisionGrant            PreemptionDecision = "GRANT"
	DecisionPreempt          PreemptionDecision = "PREEMPT"
	DecisionReject           PreemptionDecision = "REJECT"
	DecisionOfferAlternative PreemptionDecision = "OFFER_ALTERNATIVE"
)

// Policy Reason Codes for Audit Governance
const (
	ReasonPolicyGrantedAvailable      = "POLICY_GRANTED_AVAILABLE_TUNER"
	ReasonPolicyGrantedPreempted      = "POLICY_GRANTED_PREEMPTED_LOWER_PRIORITY"
	ReasonPolicyRejectedHigherActive  = "POLICY_REJECTED_HIGHER_PRIORITY_ACTIVE"
	ReasonPolicyRejectedSamePriority  = "POLICY_REJECTED_SAME_PRIORITY_ACTIVE"
	ReasonPolicyOfferedAlternative    = "POLICY_OFFERED_ALTERNATIVE_STREAM"
	ReasonPreemptionEvictionInitiated = "PREEMPTION_EVICTION_INITIATED"
	ReasonPreemptionEvictionCompleted = "PREEMPTION_EVICTION_COMPLETED"
	ReasonPreemptionFailedRollback    = "PREEMPTION_FAILED_ROLLBACK_SUCCESSFUL"
)

var (
	ErrInvalidConsumerType  = errors.New("invalid or unrecognized consumer type")
	ErrNilEvaluationRequest = errors.New("evaluation request cannot be nil")
)

// ActiveLeaseState captures the current state of an active lease for policy evaluation.
type ActiveLeaseState struct {
	LeaseID      string
	Consumer     ConsumerType
	Owner        string
	Scope        string
	AcquiredAt   time.Time
	ExpiresAt    time.Time
	IsSacrosanct bool
	IsReleasing  bool
}

// EvaluationRequest represents an incoming request for a tuner resource.
type EvaluationRequest struct {
	Consumer    ConsumerType
	Owner       string
	TargetScope string
	RequestedAt time.Time
	TTL         time.Duration
}

// EvaluationResult represents the pure decision returned by the PolicyEngine.
type EvaluationResult struct {
	Decision         PreemptionDecision
	ReasonCode       string
	ReasonDetail     string
	TargetLeaseID    string // Lease to be preempted if Decision == DecisionPreempt
	AlternativeScope string // Alternative tuner/stream scope if Decision == DecisionOfferAlternative
	EvaluatedAt      time.Time
}

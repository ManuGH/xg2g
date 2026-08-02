package policy

import (
	"context"
	"errors"
	"time"

	domainPolicy "github.com/ManuGH/xg2g/internal/domain/policy"
)

// PreemptionMode defines the operational mode for resource policy conflict evaluation.
type PreemptionMode string

const (
	PreemptionModeDisabled  PreemptionMode = "disabled"
	PreemptionModeAuditOnly PreemptionMode = "audit-only"
)

// IsValid checks whether the preemption mode is recognized for Step E2.
// Note: "enforce" is explicitly invalid in Step E2 and triggers startup error.
func (m PreemptionMode) IsValid() bool {
	switch m {
	case PreemptionModeDisabled, PreemptionModeAuditOnly:
		return true
	default:
		return false
	}
}

// Config holds preemption policy evaluation settings.
type Config struct {
	Mode PreemptionMode
}

// AllocationMetadata captures explicit typed allocation metadata from the production store.
type AllocationMetadata struct {
	AllocationID string
	Consumer     domainPolicy.ConsumerType
	Owner        string
	Scope        string
	AcquiredAt   time.Time
	Sacrosanct   bool
	Releasing    bool
}

// CandidateProvider provides physical hardware scopes and capability status.
type CandidateProvider interface {
	Candidates(ctx context.Context, req domainPolicy.EvaluationRequest) ([]domainPolicy.ResourceCandidate, time.Time, error)
}

// AllocationProvider provides active tuner allocations with explicit consumer metadata.
type AllocationProvider interface {
	ActiveAllocations(ctx context.Context, kind domainPolicy.ResourceKind) ([]AllocationMetadata, time.Time, error)
}

// AuditLogger emits structured evaluation audit events.
type AuditLogger interface {
	Emit(ctx context.Context, event EvaluationAuditEvent) error
}

// IDGenerator generates unique string identifiers for audit events.
type IDGenerator func() (string, error)

// AuditEvaluator evaluates tuner conflicts and emits structured audit events without mutating state.
type AuditEvaluator interface {
	EvaluateConflict(ctx context.Context, requestID string, req domainPolicy.EvaluationRequest, origErr error) (EvaluationAuditEvent, error)
}

var (
	ErrInvalidPreemptionMode         = errors.New("invalid or unsupported preemption mode for Step E2")
	ErrSnapshotBuildFailed           = errors.New("failed to build policy resource snapshot")
	ErrConsumerMetadataUnavailable   = errors.New("explicit consumer metadata is missing or unavailable for allocation")
	ErrCandidateProviderUnavailable  = errors.New("candidate provider is missing or returned an error")
	ErrAllocationProviderUnavailable = errors.New("allocation provider is missing or returned an error")
	ErrInconsistentSnapshotState     = errors.New("snapshot observation timestamps differ beyond consistency tolerance threshold")
	ErrPolicyEvaluationFailed        = errors.New("policy engine evaluation failed")
	ErrAuditEmissionFailed           = errors.New("failed to emit structured evaluation audit event")
	ErrInvalidEventID                = errors.New("event ID generator produced an empty or invalid event ID")
	ErrMissingRequestID              = errors.New("request ID cannot be empty for evaluation audit event")
)

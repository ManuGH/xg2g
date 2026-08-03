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

// Clock defines a deterministic time provider interface.
type Clock interface {
	Now() time.Time
}

// IDGenerator generates unique string identifiers for audit events.
type IDGenerator interface {
	NewID() (string, error)
}

// AuditLogger emits structured evaluation audit events.
type AuditLogger interface {
	Emit(ctx context.Context, event EvaluationAuditEvent) error
}

// ConflictAuditRequest contains the facts for a conflict evaluation without error pointers.
type ConflictAuditRequest struct {
	RequestID    string
	Consumer     domainPolicy.ConsumerType
	ResourceKind domainPolicy.ResourceKind
	Owner        string
	TargetScope  string
	ScopeMode    domainPolicy.ScopeSelectionMode
}

// ConflictAuditor evaluates tuner conflicts and emits structured audit events without mutating state or receiving business error pointers.
type ConflictAuditor interface {
	AuditTunerConflict(ctx context.Context, req ConflictAuditRequest) error
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

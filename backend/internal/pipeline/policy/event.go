package policy

import (
	"time"

	domainPolicy "github.com/ManuGH/xg2g/internal/domain/policy"
)

// EvaluationAuditEvent captures a structured, machine-readable audit record of a policy decision.
type EvaluationAuditEvent struct {
	EventID               string                          `json:"event_id"`
	EvaluatedAt           time.Time                       `json:"evaluated_at"`
	RequestID             string                          `json:"request_id"`
	RequestOwner          string                          `json:"request_owner"`
	Consumer              domainPolicy.ConsumerType       `json:"consumer"`
	ResourceKind          domainPolicy.ResourceKind       `json:"resource_kind"`
	ScopeMode             domainPolicy.ScopeSelectionMode `json:"scope_mode"`
	TargetScope           string                          `json:"target_scope"`
	Decision              domainPolicy.PreemptionDecision `json:"decision"`
	ReasonCode            domainPolicy.ReasonCode         `json:"reason_code"`
	SelectedScope         string                          `json:"selected_scope,omitempty"`
	TargetAllocationID    string                          `json:"target_allocation_id,omitempty"`
	BlockingAllocationIDs []string                        `json:"blocking_allocation_ids,omitempty"`
	EnforcementMode       PreemptionMode                  `json:"enforcement_mode"`
	SnapshotRevision      string                          `json:"snapshot_revision"`
	EvaluationSucceeded   bool                            `json:"evaluation_succeeded"`
	ErrorMessage          string                          `json:"error_message,omitempty"` // Strictly diagnostic message
}

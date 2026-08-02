package policy

import (
	"fmt"
	"sort"
)

// PolicyEngine provides pure, stateless conflict evaluation and preemption decisions.
type PolicyEngine struct{}

// NewPolicyEngine initializes a stateless PolicyEngine.
func NewPolicyEngine() *PolicyEngine {
	return &PolicyEngine{}
}

// Evaluate performs a pure functional decision evaluation for an incoming resource request.
// It returns an EvaluationResult and does NOT mutate state, perform network I/O, or execute side effects.
// Pure function guarantee: For identical inputs (req, snapshot), it produces 100% byte-for-byte identical results.
func (e *PolicyEngine) Evaluate(req EvaluationRequest, snapshot ResourceSnapshot) (EvaluationResult, error) {
	// 1. Strict Input & Snapshot Validation
	if req.EvaluatedAt.IsZero() {
		return EvaluationResult{
			Decision:     DecisionReject,
			ReasonCode:   ReasonPolicyInvalidInput,
			ReasonDetail: "EvaluatedAt timestamp is required",
		}, ErrEvaluationTimeRequired
	}

	if !req.Consumer.IsValid() {
		return EvaluationResult{
			Decision:     DecisionReject,
			ReasonCode:   ReasonPolicyInvalidInput,
			ReasonDetail: fmt.Sprintf("invalid incoming consumer type %q", req.Consumer),
			EvaluatedAt:  req.EvaluatedAt,
		}, ErrInvalidConsumerType
	}

	if req.Owner == "" {
		return EvaluationResult{
			Decision:     DecisionReject,
			ReasonCode:   ReasonPolicyInvalidInput,
			ReasonDetail: "request owner cannot be empty",
			EvaluatedAt:  req.EvaluatedAt,
		}, ErrInvalidOwner
	}

	if !snapshot.Kind.IsValid() {
		return EvaluationResult{
			Decision:     DecisionReject,
			ReasonCode:   ReasonPolicyInvalidInput,
			ReasonDetail: fmt.Sprintf("invalid snapshot resource kind %q", snapshot.Kind),
			EvaluatedAt:  req.EvaluatedAt,
		}, ErrInvalidResourceKind
	}

	if snapshot.Capacity <= 0 || len(snapshot.Active) > snapshot.Capacity {
		return EvaluationResult{
			Decision:     DecisionReject,
			ReasonCode:   ReasonPolicyInvalidInput,
			ReasonDetail: fmt.Sprintf("invalid capacity %d for active count %d", snapshot.Capacity, len(snapshot.Active)),
			EvaluatedAt:  req.EvaluatedAt,
		}, ErrInvalidSnapshotCapacity
	}

	seenAllocIDs := make(map[string]bool, len(snapshot.Active))
	for _, alloc := range snapshot.Active {
		if alloc.AllocationID == "" || seenAllocIDs[alloc.AllocationID] {
			return EvaluationResult{
				Decision:     DecisionReject,
				ReasonCode:   ReasonPolicyInvalidInput,
				ReasonDetail: fmt.Sprintf("invalid or duplicate allocation ID %q", alloc.AllocationID),
				EvaluatedAt:  req.EvaluatedAt,
			}, ErrInvalidAllocationID
		}
		seenAllocIDs[alloc.AllocationID] = true

		if !alloc.Consumer.IsValid() {
			return EvaluationResult{
				Decision:     DecisionReject,
				ReasonCode:   ReasonPolicyInvalidInput,
				ReasonDetail: fmt.Sprintf("invalid active allocation consumer type %q for ID %s", alloc.Consumer, alloc.AllocationID),
				EvaluatedAt:  req.EvaluatedAt,
			}, ErrInvalidConsumerType
		}
	}

	// 2. Check if the incoming consumer actually requires this resource kind
	if !RequiresResource(req.Consumer, snapshot.Kind) {
		return EvaluationResult{
			Decision:     DecisionReject,
			ReasonCode:   ReasonPolicyRejectedResourceNotRequired,
			ReasonDetail: fmt.Sprintf("consumer %s does not require resource %s", req.Consumer, snapshot.Kind),
			EvaluatedAt:  req.EvaluatedAt,
		}, nil
	}

	// 3. Capacity Check: Available capacity exists
	if len(snapshot.Active) < snapshot.Capacity {
		return EvaluationResult{
			Decision:     DecisionGrant,
			ReasonCode:   ReasonPolicyGrantedResourceAvailable,
			ReasonDetail: fmt.Sprintf("resource %s available (active: %d, capacity: %d)", snapshot.Kind, len(snapshot.Active), snapshot.Capacity),
			EvaluatedAt:  req.EvaluatedAt,
		}, nil
	}

	// 4. Capacity Exhausted -> Evaluate Conflict Matrix for Preemptible Candidates
	var candidates []ResourceAllocation
	hasProtectedActivity := false

	for _, alloc := range snapshot.Active {
		if alloc.IsSacrosanct || alloc.IsReleasing || alloc.Consumer == ConsumerScheduledRecording {
			hasProtectedActivity = true
			continue
		}

		rule, exists := DefaultConflictMatrix[alloc.Consumer][req.Consumer]
		if exists && rule.Preemptible {
			candidates = append(candidates, alloc)
		}
	}

	// 5. If no candidates match, determine rejection reason code
	if len(candidates) == 0 {
		reasonCode := ReasonPolicyRejectedEqualOrLowerPriority
		if hasProtectedActivity {
			reasonCode = ReasonPolicyRejectedProtectedActivity
		}

		return EvaluationResult{
			Decision:     DecisionReject,
			ReasonCode:   reasonCode,
			ReasonDetail: fmt.Sprintf("all %d allocations protected or equal/higher priority for resource %s", len(snapshot.Active), snapshot.Kind),
			EvaluatedAt:  req.EvaluatedAt,
		}, nil
	}

	// 6. Candidates Found -> Perform Deterministic 3-Tier Tie-Breaking
	// Sort Order:
	// Tier 1: Lowest LossClass (preempt lowest loss class first)
	// Tier 2: Oldest AcquiredAt (preempt oldest allocation first)
	// Tier 3: Lexicographical AllocationID (absolute deterministic tie-breaker)
	sort.Slice(candidates, func(i, j int) bool {
		lcI := candidates[i].Consumer.LossClass()
		lcJ := candidates[j].Consumer.LossClass()
		if lcI != lcJ {
			return lcI < lcJ
		}
		if !candidates[i].AcquiredAt.Equal(candidates[j].AcquiredAt) {
			return candidates[i].AcquiredAt.Before(candidates[j].AcquiredAt)
		}
		return candidates[i].AllocationID < candidates[j].AllocationID
	})

	target := candidates[0]

	return EvaluationResult{
		Decision:           DecisionPreemptionRequired,
		ReasonCode:         ReasonPolicyPreemptionRequired,
		ReasonDetail:       fmt.Sprintf("preemption required for allocation %s (consumer: %s, loss_class: %d)", target.AllocationID, target.Consumer, target.Consumer.LossClass()),
		TargetAllocationID: target.AllocationID,
		EvaluatedAt:        req.EvaluatedAt,
	}, nil
}

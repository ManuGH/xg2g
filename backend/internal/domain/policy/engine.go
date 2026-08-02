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

type preemptibleMatch struct {
	alloc ResourceAllocation
	rule  ConflictRule
}

// Evaluate performs a pure functional decision evaluation for an incoming resource request.
// It returns an EvaluationResult and does NOT mutate state, perform network I/O, or execute side effects.
// Pure function guarantee: For identical inputs (req, snapshot), it produces 100% byte-for-byte identical results.
func (e *PolicyEngine) Evaluate(req EvaluationRequest, snapshot ResourceSnapshot) (EvaluationResult, error) {
	// 1. Request & ScopeSelectionMode Validation
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

	if !req.ResourceKind.IsValid() {
		return EvaluationResult{
			Decision:     DecisionReject,
			ReasonCode:   ReasonPolicyInvalidInput,
			ReasonDetail: fmt.Sprintf("invalid request resource kind %q", req.ResourceKind),
			EvaluatedAt:  req.EvaluatedAt,
		}, ErrInvalidResourceKind
	}

	if !snapshot.Kind.IsValid() {
		return EvaluationResult{
			Decision:     DecisionReject,
			ReasonCode:   ReasonPolicyInvalidInput,
			ReasonDetail: fmt.Sprintf("invalid snapshot resource kind %q", snapshot.Kind),
			EvaluatedAt:  req.EvaluatedAt,
		}, ErrInvalidResourceKind
	}

	if req.ResourceKind != snapshot.Kind {
		return EvaluationResult{
			Decision:     DecisionReject,
			ReasonCode:   ReasonPolicyInvalidInput,
			ReasonDetail: fmt.Sprintf("request resource kind %q does not match snapshot kind %q", req.ResourceKind, snapshot.Kind),
			EvaluatedAt:  req.EvaluatedAt,
		}, ErrResourceKindMismatch
	}

	// Discrete exclusive pool limitation: ResourceStorageIO is a quantitative shared resource
	if req.ResourceKind == ResourceStorageIO || snapshot.Kind == ResourceStorageIO {
		return EvaluationResult{
			Decision:     DecisionReject,
			ReasonCode:   ReasonPolicyInvalidInput,
			ReasonDetail: "ResourceStorageIO uses a shared quantitative capacity model not supported in Step E1 discrete evaluation",
			EvaluatedAt:  req.EvaluatedAt,
		}, ErrUnsupportedResourceModel
	}

	if !req.ScopeMode.IsValid() {
		return EvaluationResult{
			Decision:     DecisionReject,
			ReasonCode:   ReasonPolicyInvalidInput,
			ReasonDetail: fmt.Sprintf("invalid scope selection mode %q", req.ScopeMode),
			EvaluatedAt:  req.EvaluatedAt,
		}, ErrInvalidScopeSelectionMode
	}

	if req.ScopeMode == ScopeSelectionExact && req.TargetScope == "" {
		return EvaluationResult{
			Decision:     DecisionReject,
			ReasonCode:   ReasonPolicyInvalidInput,
			ReasonDetail: "TargetScope cannot be empty when ScopeMode is EXACT",
			EvaluatedAt:  req.EvaluatedAt,
		}, ErrExactScopeRequired
	}

	if req.ScopeMode == ScopeSelectionAnyCompatible && req.TargetScope != "" {
		return EvaluationResult{
			Decision:     DecisionReject,
			ReasonCode:   ReasonPolicyInvalidInput,
			ReasonDetail: fmt.Sprintf("TargetScope %q must be empty when ScopeMode is ANY_COMPATIBLE", req.TargetScope),
			EvaluatedAt:  req.EvaluatedAt,
		}, ErrScopeModeConflict
	}

	if req.Owner == "" {
		return EvaluationResult{
			Decision:     DecisionReject,
			ReasonCode:   ReasonPolicyInvalidInput,
			ReasonDetail: "request owner cannot be empty",
			EvaluatedAt:  req.EvaluatedAt,
		}, ErrInvalidOwner
	}

	if snapshot.Capacity <= 0 || len(snapshot.Active) > snapshot.Capacity {
		return EvaluationResult{
			Decision:     DecisionReject,
			ReasonCode:   ReasonPolicyInvalidInput,
			ReasonDetail: fmt.Sprintf("invalid capacity %d for active count %d", snapshot.Capacity, len(snapshot.Active)),
			EvaluatedAt:  req.EvaluatedAt,
		}, ErrInvalidSnapshotCapacity
	}

	// Discrete Pool Invariant: snapshot.Capacity MUST equal len(snapshot.Candidates)
	if len(snapshot.Candidates) == 0 || snapshot.Capacity != len(snapshot.Candidates) {
		return EvaluationResult{
			Decision:     DecisionReject,
			ReasonCode:   ReasonPolicyInvalidInput,
			ReasonDetail: fmt.Sprintf("snapshot capacity %d does not match candidate count %d", snapshot.Capacity, len(snapshot.Candidates)),
			EvaluatedAt:  req.EvaluatedAt,
		}, ErrCandidateCapacityMismatch
	}

	// 2. Candidate Structural Validation
	candidateMap := make(map[string]ResourceCandidate, len(snapshot.Candidates))
	for _, cand := range snapshot.Candidates {
		if cand.Scope == "" {
			return EvaluationResult{
				Decision:     DecisionReject,
				ReasonCode:   ReasonPolicyInvalidInput,
				ReasonDetail: "candidate scope cannot be empty",
				EvaluatedAt:  req.EvaluatedAt,
			}, ErrInvalidCandidateScope
		}
		if _, exists := candidateMap[cand.Scope]; exists {
			return EvaluationResult{
				Decision:     DecisionReject,
				ReasonCode:   ReasonPolicyInvalidInput,
				ReasonDetail: fmt.Sprintf("duplicate candidate scope %q", cand.Scope),
				EvaluatedAt:  req.EvaluatedAt,
			}, ErrDuplicateCandidateScope
		}
		candidateMap[cand.Scope] = cand
	}

	if req.TargetScope != "" {
		cand, exists := candidateMap[req.TargetScope]
		if !exists {
			return EvaluationResult{
				Decision:     DecisionReject,
				ReasonCode:   ReasonPolicyInvalidInput,
				ReasonDetail: fmt.Sprintf("target scope %q not found in snapshot candidates", req.TargetScope),
				EvaluatedAt:  req.EvaluatedAt,
			}, ErrTargetScopeNotFound
		}
		if !cand.Compatible {
			return EvaluationResult{
				Decision:     DecisionReject,
				ReasonCode:   ReasonPolicyInvalidInput,
				ReasonDetail: fmt.Sprintf("target scope %q is marked incompatible", req.TargetScope),
				EvaluatedAt:  req.EvaluatedAt,
			}, ErrTargetScopeIncompatible
		}
	}

	// 3. Active Allocation Validation & Exclusive Scope Counting Invariant Check
	seenAllocIDs := make(map[string]bool, len(snapshot.Active))
	activeCountByScope := make(map[string]int, len(snapshot.Active))

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

		if alloc.Owner == "" {
			return EvaluationResult{
				Decision:     DecisionReject,
				ReasonCode:   ReasonPolicyInvalidInput,
				ReasonDetail: fmt.Sprintf("allocation %s owner cannot be empty", alloc.AllocationID),
				EvaluatedAt:  req.EvaluatedAt,
			}, ErrMissingAllocationOwner
		}

		if alloc.Scope == "" {
			return EvaluationResult{
				Decision:     DecisionReject,
				ReasonCode:   ReasonPolicyInvalidInput,
				ReasonDetail: fmt.Sprintf("allocation %s scope cannot be empty", alloc.AllocationID),
				EvaluatedAt:  req.EvaluatedAt,
			}, ErrMissingAllocationScope
		}

		cand, exists := candidateMap[alloc.Scope]
		if len(snapshot.Candidates) > 0 && !exists {
			return EvaluationResult{
				Decision:     DecisionReject,
				ReasonCode:   ReasonPolicyInvalidInput,
				ReasonDetail: fmt.Sprintf("allocation %s scope %q not found in candidates", alloc.AllocationID, alloc.Scope),
				EvaluatedAt:  req.EvaluatedAt,
			}, ErrAllocationScopeNotFound
		}

		if alloc.AcquiredAt.IsZero() {
			return EvaluationResult{
				Decision:     DecisionReject,
				ReasonCode:   ReasonPolicyInvalidInput,
				ReasonDetail: fmt.Sprintf("allocation %s AcquiredAt timestamp cannot be zero", alloc.AllocationID),
				EvaluatedAt:  req.EvaluatedAt,
			}, ErrZeroAcquiredAtTimestamp
		}

		// Strict Invariant: Solange eine Allocation (auch IsReleasing == true) im Snapshot existiert, ist ihr Candidate NICHT verfügbar
		if len(snapshot.Candidates) > 0 && cand.Available {
			return EvaluationResult{
				Decision:     DecisionReject,
				ReasonCode:   ReasonPolicyInvalidInput,
				ReasonDetail: fmt.Sprintf("candidate scope %q marked available despite active allocation %s (releasing: %v)", alloc.Scope, alloc.AllocationID, alloc.IsReleasing),
				EvaluatedAt:  req.EvaluatedAt,
			}, ErrInconsistentCandidateState
		}

		// Count ALL allocations per scope for exclusivity invariant (including releasing allocations)
		activeCountByScope[alloc.Scope]++
		if activeCountByScope[alloc.Scope] > 1 {
			return EvaluationResult{
				Decision:     DecisionReject,
				ReasonCode:   ReasonPolicyInvalidInput,
				ReasonDetail: fmt.Sprintf("multiple active allocations on exclusive candidate scope %q", alloc.Scope),
				EvaluatedAt:  req.EvaluatedAt,
			}, ErrMultipleActiveOnExclusiveScope
		}
	}

	// 4. Resource Requirement Check
	if !RequiresResource(req.Consumer, snapshot.Kind) {
		return EvaluationResult{
			Decision:     DecisionReject,
			ReasonCode:   ReasonPolicyRejectedResourceNotRequired,
			ReasonDetail: fmt.Sprintf("consumer %s does not require resource %s", req.Consumer, snapshot.Kind),
			EvaluatedAt:  req.EvaluatedAt,
		}, nil
	}

	// 5. Available Capacity Check with Candidate Evaluation
	var eligibleFreeCandidates []ResourceCandidate
	for _, cand := range snapshot.Candidates {
		if cand.Available && cand.Compatible {
			if req.ScopeMode == ScopeSelectionExact && cand.Scope != req.TargetScope {
				continue
			}
			eligibleFreeCandidates = append(eligibleFreeCandidates, cand)
		}
	}

	sort.Slice(eligibleFreeCandidates, func(i, j int) bool {
		return eligibleFreeCandidates[i].Scope < eligibleFreeCandidates[j].Scope
	})

	if len(snapshot.Active) < snapshot.Capacity {
		if len(eligibleFreeCandidates) > 0 {
			selected := eligibleFreeCandidates[0]
			return EvaluationResult{
				Decision:      DecisionGrant,
				ReasonCode:    ReasonPolicyGrantedResourceAvailable,
				ReasonDetail:  fmt.Sprintf("resource %s scope %q available (active: %d, capacity: %d)", snapshot.Kind, selected.Scope, len(snapshot.Active), snapshot.Capacity),
				SelectedScope: selected.Scope,
				EvaluatedAt:   req.EvaluatedAt,
			}, nil
		}

		// Unallocated capacity exists, but no matching available/compatible candidate exists for the scope mode
		return EvaluationResult{
			Decision:     DecisionReject,
			ReasonCode:   ReasonPolicyRejectedNoCompatibleCandidate,
			ReasonDetail: fmt.Sprintf("free capacity exists on %s (%d/%d), but no compatible available candidate was found for scope filter", snapshot.Kind, len(snapshot.Active), snapshot.Capacity),
			EvaluatedAt:  req.EvaluatedAt,
		}, nil
	}

	// 6. Capacity Exhausted -> Evaluate Conflict Matrix for Active Allocations
	var preemptibleCandidates []preemptibleMatch
	var blockingAllocations []string
	hasProtectedActivity := false

	for _, alloc := range snapshot.Active {
		// Strict Exact-Target Rule: In EXACT mode, ONLY evaluate the allocation occupying req.TargetScope
		if req.ScopeMode == ScopeSelectionExact && alloc.Scope != req.TargetScope {
			continue
		}

		rule, ok := ConflictRuleFor(alloc.Consumer, req.Consumer)
		if !ok {
			return EvaluationResult{
				Decision:     DecisionReject,
				ReasonCode:   ReasonPolicyInvalidInput,
				ReasonDetail: fmt.Sprintf("unknown conflict rule for (%s, %s)", alloc.Consumer, req.Consumer),
				EvaluatedAt:  req.EvaluatedAt,
			}, ErrInvalidConsumerType
		}

		if alloc.IsSacrosanct || alloc.IsReleasing || alloc.Consumer == ConsumerScheduledRecording || rule.Decision != DecisionPreemptionRequired {
			hasProtectedActivity = hasProtectedActivity || alloc.IsSacrosanct || alloc.Consumer == ConsumerScheduledRecording
			blockingAllocations = append(blockingAllocations, alloc.AllocationID)
		} else {
			preemptibleCandidates = append(preemptibleCandidates, preemptibleMatch{alloc: alloc, rule: rule})
		}
	}

	sort.Strings(blockingAllocations)

	if len(preemptibleCandidates) == 0 {
		primaryReason := ReasonPolicyRejectedEqualOrLowerPriority
		if hasProtectedActivity {
			primaryReason = ReasonPolicyRejectedProtectedActivity
		}

		return EvaluationResult{
			Decision:              DecisionReject,
			ReasonCode:            primaryReason,
			ReasonDetail:          fmt.Sprintf("active allocations protected or equal/higher priority for resource %s", snapshot.Kind),
			BlockingAllocationIDs: blockingAllocations,
			EvaluatedAt:           req.EvaluatedAt,
		}, nil
	}

	// 7. Deterministic 3-Tier Tie-Breaking for Preemption Target Selection
	// Tier 1: Lowest LossClass (preempt lowest loss class first)
	// Tier 2: Oldest AcquiredAt timestamp (preempt oldest active allocation first)
	// Tier 3: Lexicographical AllocationID (absolute deterministic tie-breaker)
	sort.Slice(preemptibleCandidates, func(i, j int) bool {
		lcI := preemptibleCandidates[i].rule.LossClass
		lcJ := preemptibleCandidates[j].rule.LossClass
		if lcI != lcJ {
			return lcI < lcJ
		}
		if !preemptibleCandidates[i].alloc.AcquiredAt.Equal(preemptibleCandidates[j].alloc.AcquiredAt) {
			return preemptibleCandidates[i].alloc.AcquiredAt.Before(preemptibleCandidates[j].alloc.AcquiredAt)
		}
		return preemptibleCandidates[i].alloc.AllocationID < preemptibleCandidates[j].alloc.AllocationID
	})

	target := preemptibleCandidates[0]

	return EvaluationResult{
		Decision:              DecisionPreemptionRequired,
		ReasonCode:            ReasonPolicyPreemptionRequired,
		ReasonDetail:          fmt.Sprintf("preemption required for allocation %s (consumer: %s, loss_class: %d)", target.alloc.AllocationID, target.alloc.Consumer, target.rule.LossClass),
		SelectedScope:         target.alloc.Scope,
		TargetAllocationID:    target.alloc.AllocationID,
		BlockingAllocationIDs: blockingAllocations,
		EvaluatedAt:           req.EvaluatedAt,
	}, nil
}

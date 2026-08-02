package policy

import (
	"fmt"
	"sort"
	"time"
)

// PolicyEngine provides pure, stateless conflict evaluation and preemption decisions.
type PolicyEngine struct{}

// NewPolicyEngine initializes a stateless PolicyEngine.
func NewPolicyEngine() *PolicyEngine {
	return &PolicyEngine{}
}

// Evaluate performs a pure functional decision evaluation for an incoming tuner request.
// It returns an EvaluationResult and does NOT mutate state, perform network I/O, or execute side effects.
func (e *PolicyEngine) Evaluate(req EvaluationRequest, activeLeases []ActiveLeaseState, totalTunerSlots int) (EvaluationResult, error) {
	now := req.RequestedAt
	if now.IsZero() {
		now = time.Now()
	}

	if !req.Consumer.IsValid() {
		return EvaluationResult{
			Decision:     DecisionReject,
			ReasonCode:   ReasonPolicyRejectedHigherActive,
			ReasonDetail: fmt.Sprintf("invalid consumer type %q", req.Consumer),
			EvaluatedAt:  now,
		}, ErrInvalidConsumerType
	}

	// 1. Check if an available tuner slot exists
	if len(activeLeases) < totalTunerSlots {
		return EvaluationResult{
			Decision:     DecisionGrant,
			ReasonCode:   ReasonPolicyGrantedAvailable,
			ReasonDetail: fmt.Sprintf("tuner slot available (active: %d, capacity: %d)", len(activeLeases), totalTunerSlots),
			EvaluatedAt:  now,
		}, nil
	}

	// 2. Tuners exhausted -> Search for lower-priority preemptible candidate
	reqWeight := req.Consumer.PriorityWeight()
	var candidates []ActiveLeaseState

	for _, lease := range activeLeases {
		if lease.IsSacrosanct || lease.IsReleasing {
			continue
		}
		// Scheduled recordings are sacrosanct by default invariant
		if lease.Consumer == ConsumerScheduledRecording {
			continue
		}

		if lease.Consumer.PriorityWeight() < reqWeight {
			candidates = append(candidates, lease)
		}
	}

	if len(candidates) == 0 {
		// Determine if rejection is due to higher or same priority active leases
		samePriorityCount := 0
		for _, lease := range activeLeases {
			if lease.Consumer.PriorityWeight() == reqWeight {
				samePriorityCount++
			}
		}

		reasonCode := ReasonPolicyRejectedHigherActive
		reasonDetail := fmt.Sprintf("all %d tuners occupied by equal or higher priority consumers", len(activeLeases))
		if samePriorityCount > 0 && samePriorityCount == len(activeLeases) {
			reasonCode = ReasonPolicyRejectedSamePriority
			reasonDetail = fmt.Sprintf("all %d tuners occupied by same priority consumer (%s)", len(activeLeases), req.Consumer)
		}

		return EvaluationResult{
			Decision:     DecisionReject,
			ReasonCode:   reasonCode,
			ReasonDetail: reasonDetail,
			EvaluatedAt:  now,
		}, nil
	}

	// Sort candidates to select the optimal preemptible target:
	// Primary sort: Lowest Priority Weight (preempt lowest first)
	// Secondary sort: Longest active duration / oldest AcquiredAt (preempt oldest background task first)
	sort.Slice(candidates, func(i, j int) bool {
		wI := candidates[i].Consumer.PriorityWeight()
		wJ := candidates[j].Consumer.PriorityWeight()
		if wI != wJ {
			return wI < wJ
		}
		return candidates[i].AcquiredAt.Before(candidates[j].AcquiredAt)
	})

	target := candidates[0]

	return EvaluationResult{
		Decision:      DecisionPreempt,
		ReasonCode:    ReasonPolicyGrantedPreempted,
		ReasonDetail:  fmt.Sprintf("preempting lower-priority consumer %s (lease: %s) for incoming %s", target.Consumer, target.LeaseID, req.Consumer),
		TargetLeaseID: target.LeaseID,
		EvaluatedAt:   now,
	}, nil
}

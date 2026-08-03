// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package preemption

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEvaluator_RevisionMismatch_RejectsWithDomainReason(t *testing.T) {
	eval := NewEvaluator()
	now := time.Now().UTC()

	req := PreemptionRequest{
		RequestID:         "req-1",
		ReceiverID:        "rec-1",
		RequesterOwner:    "client-A",
		RequesterPriority: PriorityAttributes{BasePriority: 100},
		RequestedResources: []ResourceClaim{
			{Kind: ResourceKindTunerSlot, Resource: "tuner-1", Quantity: 1},
		},
	}

	snapshot := ResourceSnapshot{
		ReceiverID:       "rec-1",
		SnapshotRevision: "rev-100",
		ObservedAt:       now,
		Allocations: []ActiveAllocation{
			{
				AllocationID: "alloc-1",
				Owner:        "client-B",
				Priority:     PriorityAttributes{BasePriority: 50},
			},
		},
	}

	proof := ConflictResolutionProof{
		ReceiverID:              "rec-1",
		SnapshotRevision:        "rev-999", // Mismatch
		HardwareProfileRevision: "hw-rev-1",
		RequestedResources:      req.RequestedResources,
		AllocationMappings: []AllocationResourceMapping{
			{
				AllocationID:   "alloc-1",
				FreedResources: req.RequestedResources,
			},
		},
	}

	res, err := eval.SelectPlan(req, snapshot, proof, now)
	require.NoError(t, err, "domain rejection must return error == nil")
	require.Equal(t, DecisionRejected, res.Decision)
	require.Equal(t, ReasonRejectedRevisionMismatch, res.Reason)
	require.Nil(t, res.Contract)
}

func TestEvaluator_ProtectedOrSacrosanct_ExcludedFromVictims(t *testing.T) {
	eval := NewEvaluator()
	now := time.Now().UTC()

	req := PreemptionRequest{
		RequestID:         "req-2",
		ReceiverID:        "rec-1",
		RequesterOwner:    "client-A",
		RequesterPriority: PriorityAttributes{BasePriority: 100},
		RequestedResources: []ResourceClaim{
			{Kind: ResourceKindTunerSlot, Resource: "tuner-1", Quantity: 1},
		},
	}

	snapshot := ResourceSnapshot{
		ReceiverID:       "rec-1",
		SnapshotRevision: "rev-100",
		ObservedAt:       now,
		Allocations: []ActiveAllocation{
			{
				AllocationID: "alloc-protected",
				Owner:        "client-recording",
				Priority:     PriorityAttributes{BasePriority: 10, Sacrosanct: true},
			},
		},
	}

	proof := ConflictResolutionProof{
		ReceiverID:              "rec-1",
		SnapshotRevision:        "rev-100",
		HardwareProfileRevision: "hw-rev-1",
		RequestedResources:      req.RequestedResources,
		AllocationMappings: []AllocationResourceMapping{
			{
				AllocationID:   "alloc-protected",
				FreedResources: req.RequestedResources,
			},
		},
	}

	res, err := eval.SelectPlan(req, snapshot, proof, now)
	require.NoError(t, err)
	require.Equal(t, DecisionRejected, res.Decision)
	require.Equal(t, ReasonRejectedNoEligibleVictim, res.Reason)
}

func TestEvaluator_MultiVictimSelection_SatisfiesCombinedResources(t *testing.T) {
	eval := NewEvaluator()
	now := time.Now().UTC()

	req := PreemptionRequest{
		RequestID:         "req-3",
		ReceiverID:        "rec-1",
		RequesterOwner:    "client-A",
		RequesterPriority: PriorityAttributes{BasePriority: 100},
		RequestedResources: []ResourceClaim{
			{Kind: ResourceKindRestrictedAccessSlot, Resource: "slot-1", Quantity: 1},
			{Kind: ResourceKindPhysicalTuner, Resource: "tuner-root-A", Quantity: 1},
		},
	}

	snapshot := ResourceSnapshot{
		ReceiverID:       "rec-1",
		SnapshotRevision: "rev-100",
		ObservedAt:       now,
		Allocations: []ActiveAllocation{
			{
				AllocationID: "alloc-A",
				Owner:        "client-B",
				Priority:     PriorityAttributes{BasePriority: 30, StartedAt: now.Add(-10 * time.Minute)},
			},
			{
				AllocationID: "alloc-B",
				Owner:        "client-C",
				Priority:     PriorityAttributes{BasePriority: 40, StartedAt: now.Add(-5 * time.Minute)},
			},
		},
	}

	// alloc-A frees slot-1, alloc-B frees tuner-root-A
	proof := ConflictResolutionProof{
		ReceiverID:              "rec-1",
		SnapshotRevision:        "rev-100",
		HardwareProfileRevision: "hw-rev-1",
		RequestedResources:      req.RequestedResources,
		AllocationMappings: []AllocationResourceMapping{
			{
				AllocationID: "alloc-A",
				FreedResources: []ResourceClaim{
					{Kind: ResourceKindRestrictedAccessSlot, Resource: "slot-1", Quantity: 1},
				},
			},
			{
				AllocationID: "alloc-B",
				FreedResources: []ResourceClaim{
					{Kind: ResourceKindPhysicalTuner, Resource: "tuner-root-A", Quantity: 1},
				},
			},
		},
	}

	res, err := eval.SelectPlan(req, snapshot, proof, now)
	require.NoError(t, err)
	require.Equal(t, DecisionApproved, res.Decision)
	require.Equal(t, ReasonApproved, res.Reason)
	require.NotNil(t, res.Contract)
	require.Equal(t, []string{"alloc-A", "alloc-B"}, res.Contract.TargetAllocationIDs)

	// Validate contract structure & hash integrity
	err = ValidateContract(res.Contract, now)
	require.NoError(t, err)
}

func TestEvaluator_InsufficientPriority_Rejects(t *testing.T) {
	eval := NewEvaluator()
	now := time.Now().UTC()

	req := PreemptionRequest{
		RequestID:         "req-4",
		ReceiverID:        "rec-1",
		RequesterOwner:    "client-low-prio",
		RequesterPriority: PriorityAttributes{BasePriority: 20}, // Lower than victim
		RequestedResources: []ResourceClaim{
			{Kind: ResourceKindTunerSlot, Resource: "tuner-1", Quantity: 1},
		},
	}

	snapshot := ResourceSnapshot{
		ReceiverID:       "rec-1",
		SnapshotRevision: "rev-100",
		ObservedAt:       now,
		Allocations: []ActiveAllocation{
			{
				AllocationID: "alloc-high-prio",
				Owner:        "client-high",
				Priority:     PriorityAttributes{BasePriority: 80},
			},
		},
	}

	proof := ConflictResolutionProof{
		ReceiverID:              "rec-1",
		SnapshotRevision:        "rev-100",
		HardwareProfileRevision: "hw-rev-1",
		RequestedResources:      req.RequestedResources,
		AllocationMappings: []AllocationResourceMapping{
			{
				AllocationID:   "alloc-high-prio",
				FreedResources: req.RequestedResources,
			},
		},
	}

	res, err := eval.SelectPlan(req, snapshot, proof, now)
	require.NoError(t, err)
	require.Equal(t, DecisionRejected, res.Decision)
	require.Equal(t, ReasonRejectedInsufficientPriority, res.Reason)
}

func TestEvaluator_DeterministicSelection_TieBreaker(t *testing.T) {
	eval := NewEvaluator()
	now := time.Now().UTC()

	req := PreemptionRequest{
		RequestID:         "req-5",
		ReceiverID:        "rec-1",
		RequesterOwner:    "client-A",
		RequesterPriority: PriorityAttributes{BasePriority: 100},
		RequestedResources: []ResourceClaim{
			{Kind: ResourceKindTunerSlot, Resource: "tuner-1", Quantity: 1},
		},
	}

	snapshot := ResourceSnapshot{
		ReceiverID:       "rec-1",
		SnapshotRevision: "rev-100",
		ObservedAt:       now,
		Allocations: []ActiveAllocation{
			{
				AllocationID: "alloc-Z",
				Owner:        "client-Z",
				Priority:     PriorityAttributes{BasePriority: 50, StartedAt: now},
			},
			{
				AllocationID: "alloc-A",
				Owner:        "client-A",
				Priority:     PriorityAttributes{BasePriority: 50, StartedAt: now},
			},
		},
	}

	proof := ConflictResolutionProof{
		ReceiverID:              "rec-1",
		SnapshotRevision:        "rev-100",
		HardwareProfileRevision: "hw-rev-1",
		RequestedResources:      req.RequestedResources,
		AllocationMappings: []AllocationResourceMapping{
			{AllocationID: "alloc-Z", FreedResources: req.RequestedResources},
			{AllocationID: "alloc-A", FreedResources: req.RequestedResources},
		},
	}

	// Both alloc-Z and alloc-A have identical priority & startedAt.
	// Lexicographical tie-breaker must pick alloc-A.
	res1, err := eval.SelectPlan(req, snapshot, proof, now)
	require.NoError(t, err)
	require.Equal(t, []string{"alloc-A"}, res1.Contract.TargetAllocationIDs)

	res2, err := eval.SelectPlan(req, snapshot, proof, now)
	require.NoError(t, err)
	require.Equal(t, res1.Contract.ContractHash, res2.Contract.ContractHash)
}

// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package preemption

import (
	"encoding/json"
	"fmt"
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
				Claims:       []ResourceClaim{{Kind: ResourceKindTunerSlot, Resource: "tuner-1", Quantity: 1}},
			},
		},
	}

	proof := ConflictResolutionProof{
		ReceiverID:              "rec-1",
		SnapshotRevision:        "rev-999", // Mismatch
		HardwareProfileRevision: "hw-rev-1",
		HardwareProfileStatus:   HardwareProfileValid,
		EvidenceClassification:  EvidenceDirectObservation,
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

func TestSelectPlan_MinimalVictimCountWinsBeforeLowerPrioritySum(t *testing.T) {
	eval := NewEvaluator()
	now := time.Now().UTC()

	req := PreemptionRequest{
		RequestID:         "req-min-victim",
		ReceiverID:        "rec-1",
		RequesterOwner:    "client-A",
		RequesterPriority: PriorityAttributes{BasePriority: 100},
		RequestedResources: []ResourceClaim{
			{Kind: ResourceKindTunerSlot, Resource: "tuner-1", Quantity: 1},
			{Kind: ResourceKindRestrictedAccessSlot, Resource: "slot-1", Quantity: 1},
		},
	}

	snapshot := ResourceSnapshot{
		ReceiverID:       "rec-1",
		SnapshotRevision: "rev-100",
		ObservedAt:       now,
		Allocations: []ActiveAllocation{
			{
				AllocationID: "alloc-single-victim",
				Owner:        "client-single",
				Priority:     PriorityAttributes{BasePriority: 20}, // Single victim priority = 20
				Claims: []ResourceClaim{
					{Kind: ResourceKindTunerSlot, Resource: "tuner-1", Quantity: 1},
					{Kind: ResourceKindRestrictedAccessSlot, Resource: "slot-1", Quantity: 1},
				},
			},
			{
				AllocationID: "alloc-two-1",
				Owner:        "client-two-1",
				Priority:     PriorityAttributes{BasePriority: 5}, // Two victims priority sum = 10
				Claims:       []ResourceClaim{{Kind: ResourceKindTunerSlot, Resource: "tuner-1", Quantity: 1}},
			},
			{
				AllocationID: "alloc-two-2",
				Owner:        "client-two-2",
				Priority:     PriorityAttributes{BasePriority: 5},
				Claims:       []ResourceClaim{{Kind: ResourceKindRestrictedAccessSlot, Resource: "slot-1", Quantity: 1}},
			},
		},
	}

	proof := ConflictResolutionProof{
		ReceiverID:              "rec-1",
		SnapshotRevision:        "rev-100",
		HardwareProfileRevision: "hw-rev-1",
		HardwareProfileStatus:   HardwareProfileValid,
		EvidenceClassification:  EvidenceDirectObservation,
		RequestedResources:      req.RequestedResources,
		AllocationMappings: []AllocationResourceMapping{
			{
				AllocationID: "alloc-single-victim",
				FreedResources: []ResourceClaim{
					{Kind: ResourceKindTunerSlot, Resource: "tuner-1", Quantity: 1},
					{Kind: ResourceKindRestrictedAccessSlot, Resource: "slot-1", Quantity: 1},
				},
			},
			{
				AllocationID:   "alloc-two-1",
				FreedResources: []ResourceClaim{{Kind: ResourceKindTunerSlot, Resource: "tuner-1", Quantity: 1}},
			},
			{
				AllocationID:   "alloc-two-2",
				FreedResources: []ResourceClaim{{Kind: ResourceKindRestrictedAccessSlot, Resource: "slot-1", Quantity: 1}},
			},
		},
	}

	res, err := eval.SelectPlan(req, snapshot, proof, now)
	require.NoError(t, err)
	require.Equal(t, DecisionApproved, res.Decision)
	require.NotNil(t, res.Contract)
	// Single victim MUST win over two victims, even though 20 > (5 + 5)
	require.Equal(t, []string{"alloc-single-victim"}, res.Contract.TargetAllocationIDs)
}

func TestSelectPlan_RejectsStaleHardwareProfile(t *testing.T) {
	eval := NewEvaluator()
	now := time.Now().UTC()

	req := PreemptionRequest{
		RequestID:         "req-stale",
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
				Claims:       req.RequestedResources,
			},
		},
	}

	proof := ConflictResolutionProof{
		ReceiverID:              "rec-1",
		SnapshotRevision:        "rev-100",
		HardwareProfileRevision: "hw-rev-1",
		HardwareProfileStatus:   HardwareProfileStale, // STALE!
		EvidenceClassification:  EvidenceDirectObservation,
		RequestedResources:      req.RequestedResources,
		AllocationMappings: []AllocationResourceMapping{
			{AllocationID: "alloc-1", FreedResources: req.RequestedResources},
		},
	}

	res, err := eval.SelectPlan(req, snapshot, proof, now)
	require.NoError(t, err)
	require.Equal(t, DecisionRejected, res.Decision)
	require.Equal(t, ReasonRejectedStaleHardware, res.Reason)
}

func TestSelectPlan_RejectsUnverifiedEvidence(t *testing.T) {
	eval := NewEvaluator()
	now := time.Now().UTC()

	req := PreemptionRequest{
		RequestID:         "req-unverified",
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
			{AllocationID: "alloc-1", Claims: req.RequestedResources},
		},
	}

	proof := ConflictResolutionProof{
		ReceiverID:              "rec-1",
		SnapshotRevision:        "rev-100",
		HardwareProfileRevision: "hw-rev-1",
		HardwareProfileStatus:   HardwareProfileValid,
		EvidenceClassification:  EvidenceClassification("UNVERIFIED_GUESS"),
		RequestedResources:      req.RequestedResources,
		AllocationMappings: []AllocationResourceMapping{
			{AllocationID: "alloc-1", FreedResources: req.RequestedResources},
		},
	}

	res, err := eval.SelectPlan(req, snapshot, proof, now)
	require.NoError(t, err)
	require.Equal(t, DecisionRejected, res.Decision)
	require.Equal(t, ReasonRejectedInvalidProof, res.Reason)
}

func TestSelectPlan_RejectsInferredEvidenceForExecutablePreemption(t *testing.T) {
	eval := NewEvaluator()
	now := time.Now().UTC()

	req := PreemptionRequest{
		RequestID:         "req-inferred",
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
			{AllocationID: "alloc-1", Claims: req.RequestedResources},
		},
	}

	// INFERRED evidence classification MUST be rejected for executable preemption contracts
	proof := ConflictResolutionProof{
		ReceiverID:              "rec-1",
		SnapshotRevision:        "rev-100",
		HardwareProfileRevision: "hw-rev-1",
		HardwareProfileStatus:   HardwareProfileValid,
		EvidenceClassification:  EvidenceInferred,
		RequestedResources:      req.RequestedResources,
		AllocationMappings: []AllocationResourceMapping{
			{AllocationID: "alloc-1", FreedResources: req.RequestedResources},
		},
	}

	res, err := eval.SelectPlan(req, snapshot, proof, now)
	require.NoError(t, err)
	require.Equal(t, DecisionRejected, res.Decision)
	require.Equal(t, ReasonRejectedInvalidProof, res.Reason)
}

func TestSelectPlan_RejectsProofMappingForInactiveAllocation(t *testing.T) {
	eval := NewEvaluator()
	now := time.Now().UTC()

	req := PreemptionRequest{
		RequestID:         "req-unmapped",
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
		Allocations:      []ActiveAllocation{}, // Zero active allocations
	}

	proof := ConflictResolutionProof{
		ReceiverID:              "rec-1",
		SnapshotRevision:        "rev-100",
		HardwareProfileRevision: "hw-rev-1",
		HardwareProfileStatus:   HardwareProfileValid,
		EvidenceClassification:  EvidenceDirectObservation,
		RequestedResources:      req.RequestedResources,
		AllocationMappings: []AllocationResourceMapping{
			{AllocationID: "alloc-inactive-ghost", FreedResources: req.RequestedResources},
		},
	}

	res, err := eval.SelectPlan(req, snapshot, proof, now)
	require.NoError(t, err)
	require.Equal(t, DecisionRejected, res.Decision)
	require.Equal(t, ReasonRejectedInvalidProof, res.Reason)
}

func TestSelectPlan_RejectsFreedResourcesNotOwnedByTarget(t *testing.T) {
	eval := NewEvaluator()
	now := time.Now().UTC()

	req := PreemptionRequest{
		RequestID:         "req-not-owned",
		ReceiverID:        "rec-1",
		RequesterOwner:    "client-A",
		RequesterPriority: PriorityAttributes{BasePriority: 100},
		RequestedResources: []ResourceClaim{
			{Kind: ResourceKindRestrictedAccessSlot, Resource: "slot-55", Quantity: 1},
		},
	}

	snapshot := ResourceSnapshot{
		ReceiverID:       "rec-1",
		SnapshotRevision: "rev-100",
		ObservedAt:       now,
		Allocations: []ActiveAllocation{
			{
				AllocationID: "alloc-1",
				Claims:       []ResourceClaim{{Kind: ResourceKindTunerSlot, Resource: "tuner-1", Quantity: 1}}, // Only owns tuner-1
			},
		},
	}

	proof := ConflictResolutionProof{
		ReceiverID:              "rec-1",
		SnapshotRevision:        "rev-100",
		HardwareProfileRevision: "hw-rev-1",
		HardwareProfileStatus:   HardwareProfileValid,
		EvidenceClassification:  EvidenceDirectObservation,
		RequestedResources:      req.RequestedResources,
		AllocationMappings: []AllocationResourceMapping{
			// Falsely claims alloc-1 frees slot-55 which alloc-1 does NOT own!
			{AllocationID: "alloc-1", FreedResources: []ResourceClaim{{Kind: ResourceKindRestrictedAccessSlot, Resource: "slot-55", Quantity: 1}}},
		},
	}

	res, err := eval.SelectPlan(req, snapshot, proof, now)
	require.NoError(t, err)
	require.Equal(t, DecisionRejected, res.Decision)
	require.Equal(t, ReasonRejectedInvalidProof, res.Reason)
}

func TestSelectPlan_CandidateBoundExceeded(t *testing.T) {
	eval := NewEvaluator()
	now := time.Now().UTC()

	req := PreemptionRequest{
		RequestID:         "req-bound",
		ReceiverID:        "rec-1",
		RequesterOwner:    "client-A",
		RequesterPriority: PriorityAttributes{BasePriority: 100},
		RequestedResources: []ResourceClaim{
			{Kind: ResourceKindTunerSlot, Resource: "tuner-1", Quantity: 1},
		},
	}

	// 17 allocations (> MaxCandidateAllocations = 16)
	allocs := make([]ActiveAllocation, 17)
	mappings := make([]AllocationResourceMapping, 17)
	for i := 0; i < 17; i++ {
		id := fmt.Sprintf("alloc-%02d", i)
		allocs[i] = ActiveAllocation{
			AllocationID: id,
			Priority:     PriorityAttributes{BasePriority: 10},
			Claims:       req.RequestedResources,
		}
		mappings[i] = AllocationResourceMapping{AllocationID: id, FreedResources: req.RequestedResources}
	}

	snapshot := ResourceSnapshot{
		ReceiverID:       "rec-1",
		SnapshotRevision: "rev-100",
		ObservedAt:       now,
		Allocations:      allocs,
	}

	proof := ConflictResolutionProof{
		ReceiverID:              "rec-1",
		SnapshotRevision:        "rev-100",
		HardwareProfileRevision: "hw-rev-1",
		HardwareProfileStatus:   HardwareProfileValid,
		EvidenceClassification:  EvidenceDirectObservation,
		RequestedResources:      req.RequestedResources,
		AllocationMappings:      mappings,
	}

	res, err := eval.SelectPlan(req, snapshot, proof, now)
	require.NoError(t, err)
	require.Equal(t, DecisionRejected, res.Decision)
	require.Equal(t, ReasonRejectedBoundsExceeded, res.Reason)
}

func TestContractHashChangesWhenAnyRelevantFieldChanges(t *testing.T) {
	now := time.Now().UTC()
	baseContract := PreemptionExecutionContract{
		ContractID:              "c-1",
		ReceiverID:              "rec-1",
		RequestID:               "req-1",
		TargetAllocationIDs:     []string{"alloc-1", "alloc-2"},
		SnapshotRevision:        "rev-1",
		HardwareProfileRevision: "hw-1",
		ConflictProofRevision:   "rev-1",
		CreatedAt:               now,
		ExpiresAt:               now.Add(30 * time.Second),
	}

	baseHash, err := ComputeContractHash(&baseContract)
	require.NoError(t, err)

	// Modify target allocation
	mod1 := baseContract
	mod1.TargetAllocationIDs = []string{"alloc-1", "alloc-3"}
	hash1, err := ComputeContractHash(&mod1)
	require.NoError(t, err)
	require.NotEqual(t, baseHash, hash1)

	// Modify snapshot revision
	mod2 := baseContract
	mod2.SnapshotRevision = "rev-2"
	hash2, err := ComputeContractHash(&mod2)
	require.NoError(t, err)
	require.NotEqual(t, baseHash, hash2)
}

func TestContractRejectsExpiredContract(t *testing.T) {
	now := time.Now().UTC()
	reqClaim := []ResourceClaim{{Kind: ResourceKindTunerSlot, Resource: "tuner-1", Quantity: 1}}
	contract := PreemptionExecutionContract{
		ContractID:              "c-1",
		ReceiverID:              "rec-1",
		RequestID:               "req-1",
		RequesterOwner:          "owner-1",
		RequesterAllocationID:   "alloc-req-1",
		RequesterRevision:       "rev-req-1",
		TargetAllocationIDs:     []string{"alloc-1"},
		RequestedResources:      reqClaim,
		ExpectedFreedResources:  reqClaim,
		SnapshotRevision:        "rev-1",
		HardwareProfileRevision: "hw-1",
		ConflictProofRevision:   "rev-1",
		CreatedAt:               now.Add(-60 * time.Second),
		ExpiresAt:               now.Add(-10 * time.Second), // Expired!
	}
	hash, err := ComputeContractHash(&contract)
	require.NoError(t, err)
	contract.ContractHash = hash

	err = ValidateContract(&contract, now)
	require.ErrorIs(t, err, ErrContractExpired)
}

func TestContractRejectsUnsortedOrDuplicateTargets(t *testing.T) {
	now := time.Now().UTC()
	reqClaim := []ResourceClaim{{Kind: ResourceKindTunerSlot, Resource: "tuner-1", Quantity: 1}}

	// Unsorted targets
	contractUnsorted := PreemptionExecutionContract{
		ContractID:              "c-1",
		ReceiverID:              "rec-1",
		RequestID:               "req-1",
		RequesterOwner:          "owner-1",
		RequesterAllocationID:   "alloc-req-1",
		RequesterRevision:       "rev-req-1",
		TargetAllocationIDs:     []string{"alloc-B", "alloc-A"}, // Unsorted
		RequestedResources:      reqClaim,
		ExpectedFreedResources:  reqClaim,
		SnapshotRevision:        "rev-1",
		HardwareProfileRevision: "hw-1",
		ConflictProofRevision:   "rev-1",
		CreatedAt:               now,
		ExpiresAt:               now.Add(30 * time.Second),
	}
	hash1, err := ComputeContractHash(&contractUnsorted)
	require.NoError(t, err)
	contractUnsorted.ContractHash = hash1

	err = ValidateContract(&contractUnsorted, now)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not canonically sorted")

	// Duplicate targets
	contractDuplicate := PreemptionExecutionContract{
		ContractID:              "c-2",
		ReceiverID:              "rec-1",
		RequestID:               "req-1",
		RequesterOwner:          "owner-1",
		RequesterAllocationID:   "alloc-req-1",
		RequesterRevision:       "rev-req-1",
		TargetAllocationIDs:     []string{"alloc-A", "alloc-A"}, // Duplicate
		RequestedResources:      reqClaim,
		ExpectedFreedResources:  reqClaim,
		SnapshotRevision:        "rev-1",
		HardwareProfileRevision: "hw-1",
		ConflictProofRevision:   "rev-1",
		CreatedAt:               now,
		ExpiresAt:               now.Add(30 * time.Second),
	}
	hash2, err := ComputeContractHash(&contractDuplicate)
	require.NoError(t, err)
	contractDuplicate.ContractHash = hash2

	err = ValidateContract(&contractDuplicate, now)
	require.ErrorIs(t, err, ErrContractDuplicateTargets)
}

func TestSelectPlan_DoesNotMutateInputData(t *testing.T) {
	eval := NewEvaluator()
	now := time.Now().UTC()

	req := PreemptionRequest{
		RequestID:         "req-mutate",
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
			{AllocationID: "alloc-2", Claims: req.RequestedResources},
			{AllocationID: "alloc-1", Claims: req.RequestedResources},
		},
	}

	proof := ConflictResolutionProof{
		ReceiverID:              "rec-1",
		SnapshotRevision:        "rev-100",
		HardwareProfileRevision: "hw-rev-1",
		HardwareProfileStatus:   HardwareProfileValid,
		EvidenceClassification:  EvidenceDirectObservation,
		RequestedResources:      req.RequestedResources,
		AllocationMappings: []AllocationResourceMapping{
			{AllocationID: "alloc-2", FreedResources: req.RequestedResources},
			{AllocationID: "alloc-1", FreedResources: req.RequestedResources},
		},
	}

	reqJSONBefore, _ := json.Marshal(req)
	snapshotJSONBefore, _ := json.Marshal(snapshot)
	proofJSONBefore, _ := json.Marshal(proof)

	res, err := eval.SelectPlan(req, snapshot, proof, now)
	require.NoError(t, err)
	require.Equal(t, DecisionApproved, res.Decision)

	reqJSONAfter, _ := json.Marshal(req)
	snapshotJSONAfter, _ := json.Marshal(snapshot)
	proofJSONAfter, _ := json.Marshal(proof)

	require.Equal(t, string(reqJSONBefore), string(reqJSONAfter), "PreemptionRequest must not be mutated")
	require.Equal(t, string(snapshotJSONBefore), string(snapshotJSONAfter), "ResourceSnapshot must not be mutated")
	require.Equal(t, string(proofJSONBefore), string(proofJSONAfter), "ConflictResolutionProof must not be mutated")
}

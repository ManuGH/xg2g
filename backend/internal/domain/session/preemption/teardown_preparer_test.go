// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package preemption

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTeardownPreparer_ValidPreparation_ClampsTTLAndHashes(t *testing.T) {
	eval := NewEvaluator()
	preparer := NewTeardownPreparer()
	now := time.Now().UTC()

	req := PreemptionRequest{
		RequestID:         "req-prep-1",
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
		HardwareProfileStatus:   HardwareProfileValid,
		EvidenceClassification:  EvidenceDirectObservation,
		RequestedResources:      req.RequestedResources,
		AllocationMappings: []AllocationResourceMapping{
			{AllocationID: "alloc-1", FreedResources: req.RequestedResources},
		},
	}

	// 1. Select plan (E3.1)
	selRes, err := eval.SelectPlan(req, snapshot, proof, now)
	require.NoError(t, err)
	require.Equal(t, DecisionApproved, selRes.Decision)

	// 2. Prepare teardown (E3.2)
	prepRes, err := preparer.PrepareTeardown(selRes.Contract, snapshot, proof, now)
	require.NoError(t, err)
	require.Equal(t, DecisionApproved, prepRes.Decision)
	require.Equal(t, ReasonTeardownApproved, prepRes.Reason)
	require.NotNil(t, prepRes.Prepared)

	// 3. Verify PreparedTeardown fields
	p := prepRes.Prepared
	require.Equal(t, selRes.Contract.ContractID, p.ContractID)
	require.Equal(t, selRes.Contract.ContractHash, p.ContractHash)
	require.Equal(t, []string{"alloc-1"}, p.TargetAllocationIDs)

	// Verify expiration clamping (min(contract.ExpiresAt, now + 15s))
	require.True(t, p.ExpiresAt.Equal(now.Add(15*time.Second)) || p.ExpiresAt.Before(selRes.Contract.ExpiresAt))

	// Verify hash
	err = ValidatePreparedTeardown(p, now)
	require.NoError(t, err)
}

func TestTeardownPreparer_RejectsInferredEvidence(t *testing.T) {
	eval := NewEvaluator()
	preparer := NewTeardownPreparer()
	now := time.Now().UTC()

	req := PreemptionRequest{
		RequestID:         "req-prep-2",
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
		EvidenceClassification:  EvidenceDirectObservation,
		RequestedResources:      req.RequestedResources,
		AllocationMappings: []AllocationResourceMapping{
			{AllocationID: "alloc-1", FreedResources: req.RequestedResources},
		},
	}

	selRes, err := eval.SelectPlan(req, snapshot, proof, now)
	require.NoError(t, err)

	// Mutate proof to INFERRED
	proofInferred := proof
	proofInferred.EvidenceClassification = EvidenceInferred

	prepRes, err := preparer.PrepareTeardown(selRes.Contract, snapshot, proofInferred, now)
	require.NoError(t, err)
	require.Equal(t, DecisionRejected, prepRes.Decision)
	require.Equal(t, ReasonTeardownInvalidProof, prepRes.Reason)
}

func TestTeardownPreparer_RejectsStaleHardwareStatus(t *testing.T) {
	eval := NewEvaluator()
	preparer := NewTeardownPreparer()
	now := time.Now().UTC()

	req := PreemptionRequest{
		RequestID:         "req-prep-3",
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
		EvidenceClassification:  EvidenceDirectObservation,
		RequestedResources:      req.RequestedResources,
		AllocationMappings: []AllocationResourceMapping{
			{AllocationID: "alloc-1", FreedResources: req.RequestedResources},
		},
	}

	selRes, err := eval.SelectPlan(req, snapshot, proof, now)
	require.NoError(t, err)

	// Mutate proof to STALE
	proofStale := proof
	proofStale.HardwareProfileStatus = HardwareProfileStale

	prepRes, err := preparer.PrepareTeardown(selRes.Contract, snapshot, proofStale, now)
	require.NoError(t, err)
	require.Equal(t, DecisionRejected, prepRes.Decision)
	require.Equal(t, ReasonTeardownStaleHardware, prepRes.Reason)
}

func TestTeardownPreparer_FineGrainedRevisionMismatches(t *testing.T) {
	eval := NewEvaluator()
	preparer := NewTeardownPreparer()
	now := time.Now().UTC()

	req := PreemptionRequest{
		RequestID:         "req-rev",
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
		EvidenceClassification:  EvidenceDirectObservation,
		RequestedResources:      req.RequestedResources,
		AllocationMappings: []AllocationResourceMapping{
			{AllocationID: "alloc-1", FreedResources: req.RequestedResources},
		},
	}

	selRes, err := eval.SelectPlan(req, snapshot, proof, now)
	require.NoError(t, err)

	// 1. Snapshot revision drift
	snapDrift := snapshot
	snapDrift.SnapshotRevision = "rev-101"
	res1, err := preparer.PrepareTeardown(selRes.Contract, snapDrift, proof, now)
	require.NoError(t, err)
	require.Equal(t, ReasonTeardownSnapshotRevisionMismatch, res1.Reason)

	// 2. Hardware profile revision drift
	proofHwDrift := proof
	proofHwDrift.HardwareProfileRevision = "hw-rev-99"
	res2, err := preparer.PrepareTeardown(selRes.Contract, snapshot, proofHwDrift, now)
	require.NoError(t, err)
	require.Equal(t, ReasonTeardownHardwareRevisionMismatch, res2.Reason)

	// 3. Conflict proof revision drift
	proofProofDrift := proof
	proofProofDrift.SnapshotRevision = "rev-999"
	res3, err := preparer.PrepareTeardown(selRes.Contract, snapshot, proofProofDrift, now)
	require.NoError(t, err)
	require.Equal(t, ReasonTeardownConflictProofRevisionMismatch, res3.Reason)
}

func TestTeardownPreparer_AllOrNothingTargetValidation(t *testing.T) {
	eval := NewEvaluator()
	preparer := NewTeardownPreparer()
	now := time.Now().UTC()

	req := PreemptionRequest{
		RequestID:         "req-multi",
		ReceiverID:        "rec-1",
		RequesterOwner:    "client-A",
		RequesterPriority: PriorityAttributes{BasePriority: 100},
		RequestedResources: []ResourceClaim{
			{Kind: ResourceKindRestrictedAccessSlot, Resource: "slot-1", Quantity: 1},
			{Kind: ResourceKindTunerSlot, Resource: "tuner-1", Quantity: 1},
		},
	}

	snapshot := ResourceSnapshot{
		ReceiverID:       "rec-1",
		SnapshotRevision: "rev-100",
		ObservedAt:       now,
		Allocations: []ActiveAllocation{
			{AllocationID: "alloc-A", Claims: []ResourceClaim{{Kind: ResourceKindRestrictedAccessSlot, Resource: "slot-1", Quantity: 1}}},
			{AllocationID: "alloc-B", Claims: []ResourceClaim{{Kind: ResourceKindTunerSlot, Resource: "tuner-1", Quantity: 1}}},
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
			{AllocationID: "alloc-A", FreedResources: []ResourceClaim{{Kind: ResourceKindRestrictedAccessSlot, Resource: "slot-1", Quantity: 1}}},
			{AllocationID: "alloc-B", FreedResources: []ResourceClaim{{Kind: ResourceKindTunerSlot, Resource: "tuner-1", Quantity: 1}}},
		},
	}

	selRes, err := eval.SelectPlan(req, snapshot, proof, now)
	require.NoError(t, err)
	require.Equal(t, []string{"alloc-A", "alloc-B"}, selRes.Contract.TargetAllocationIDs)

	// Mutate snapshot: alloc-B is missing!
	snapMutated := snapshot
	snapMutated.Allocations = []ActiveAllocation{
		{AllocationID: "alloc-A", Claims: []ResourceClaim{{Kind: ResourceKindRestrictedAccessSlot, Resource: "slot-1", Quantity: 1}}},
	}

	// Entire preparation MUST be rejected (zero partial teardown!)
	res, err := preparer.PrepareTeardown(selRes.Contract, snapMutated, proof, now)
	require.NoError(t, err)
	require.Equal(t, DecisionRejected, res.Decision)
	require.Equal(t, ReasonTeardownTargetStateMutated, res.Reason)

	// Mutate snapshot: alloc-B becomes Sacrosanct!
	snapProtected := snapshot
	snapProtected.Allocations[1].Priority.Sacrosanct = true

	resProt, err := preparer.PrepareTeardown(selRes.Contract, snapProtected, proof, now)
	require.NoError(t, err)
	require.Equal(t, DecisionRejected, resProt.Decision)
	require.Equal(t, ReasonTeardownTargetProtected, resProt.Reason)
}

func TestValidatePreparedTeardown_HashSensitivityAndUnsortedTargets(t *testing.T) {
	now := time.Now().UTC()

	basePrep := PreparedTeardown{
		TeardownID:              "t-1",
		ContractID:              "c-1",
		ReceiverID:              "rec-1",
		RequestID:               "req-1",
		ContractHash:            "chash-1",
		TargetAllocationIDs:     []string{"alloc-1", "alloc-2"},
		SnapshotRevision:        "rev-1",
		HardwareProfileRevision: "hw-1",
		ConflictProofRevision:   "rev-1",
		PreparedAt:              now,
		ExpiresAt:               now.Add(15 * time.Second),
	}

	hash, err := ComputePreparedTeardownHash(&basePrep)
	require.NoError(t, err)
	basePrep.PreparedTeardownHash = hash

	// Valid hash & targets
	err = ValidatePreparedTeardown(&basePrep, now)
	require.NoError(t, err)

	// Hash sensitivity on Target change
	modPrep := basePrep
	modPrep.TargetAllocationIDs = []string{"alloc-1", "alloc-3"}
	err = ValidatePreparedTeardown(&modPrep, now)
	require.ErrorIs(t, err, ErrPreparedTeardownHashMismatch)

	// Unsorted targets rejection
	unsortedPrep := basePrep
	unsortedPrep.TargetAllocationIDs = []string{"alloc-2", "alloc-1"}
	uHash, _ := ComputePreparedTeardownHash(&unsortedPrep)
	unsortedPrep.PreparedTeardownHash = uHash
	err = ValidatePreparedTeardown(&unsortedPrep, now)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not canonically sorted")
}

func TestPrepareTeardown_DoesNotMutateInputData(t *testing.T) {
	eval := NewEvaluator()
	preparer := NewTeardownPreparer()
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

	selRes, err := eval.SelectPlan(req, snapshot, proof, now)
	require.NoError(t, err)

	contractJSONBefore, _ := json.Marshal(selRes.Contract)
	snapshotJSONBefore, _ := json.Marshal(snapshot)
	proofJSONBefore, _ := json.Marshal(proof)

	res, err := preparer.PrepareTeardown(selRes.Contract, snapshot, proof, now)
	require.NoError(t, err)
	require.Equal(t, DecisionApproved, res.Decision)

	contractJSONAfter, _ := json.Marshal(selRes.Contract)
	snapshotJSONAfter, _ := json.Marshal(snapshot)
	proofJSONAfter, _ := json.Marshal(proof)

	require.Equal(t, string(contractJSONBefore), string(contractJSONAfter), "Contract must not be mutated")
	require.Equal(t, string(snapshotJSONBefore), string(snapshotJSONAfter), "ResourceSnapshot must not be mutated")
	require.Equal(t, string(proofJSONBefore), string(proofJSONAfter), "ConflictResolutionProof must not be mutated")
}

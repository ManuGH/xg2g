// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package preemption

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValidateConflictProof_RejectsDuplicateSnapshotAllocations(t *testing.T) {
	req := PreemptionRequest{
		RequestID:             "req-1",
		ReceiverID:            "rec-1",
		RequesterOwner:        "client-A",
		RequesterAllocationID: "alloc-req-1",
		RequesterRevision:     "rev-1",
		RequestedResources: []ResourceClaim{
			{Kind: ResourceKindTunerSlot, Resource: "tuner-1", Quantity: 1},
		},
	}

	snapshot := ResourceSnapshot{
		ReceiverID:       "rec-1",
		SnapshotRevision: "rev-100",
		ObservedAt:       time.Now().UTC(),
		Allocations: []ActiveAllocation{
			{AllocationID: "alloc-1", Claims: req.RequestedResources},
			{AllocationID: "alloc-1", Claims: req.RequestedResources}, // DUPLICATE in snapshot!
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

	err := ValidateConflictProof(req, snapshot, proof)
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate allocation ID 'alloc-1' in snapshot allocations")
}

func TestValidateConflictProof_StrictBijectionFreedResourcesByTarget(t *testing.T) {
	req := PreemptionRequest{
		RequestID:             "req-1",
		ReceiverID:            "rec-1",
		RequesterOwner:        "client-A",
		RequesterAllocationID: "alloc-req-1",
		RequesterRevision:     "rev-1",
		RequestedResources: []ResourceClaim{
			{Kind: ResourceKindTunerSlot, Resource: "tuner-1", Quantity: 1},
		},
	}

	snapshot := ResourceSnapshot{
		ReceiverID:       "rec-1",
		SnapshotRevision: "rev-100",
		ObservedAt:       time.Now().UTC(),
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

	// 1. Missing target key in FreedResourcesByTarget
	proofMissing := proof
	proofMissing.FreedResourcesByTarget = map[string][]ResourceClaim{}
	err := ValidateConflictProof(req, snapshot, proofMissing)
	require.Error(t, err)
	require.Contains(t, err.Error(), "FreedResourcesByTarget map length")

	// 2. Extra target key in FreedResourcesByTarget
	proofExtra := proof
	proofExtra.FreedResourcesByTarget = map[string][]ResourceClaim{
		"alloc-1": req.RequestedResources,
		"alloc-2": req.RequestedResources,
	}
	err = ValidateConflictProof(req, snapshot, proofExtra)
	require.Error(t, err)
	require.Contains(t, err.Error(), "FreedResourcesByTarget map length")

	// 3. Contradictory claims in FreedResourcesByTarget
	proofContradict := proof
	proofContradict.FreedResourcesByTarget = map[string][]ResourceClaim{
		"alloc-1": {{Kind: ResourceKindDemux, Resource: "demux-7", Quantity: 1}},
	}
	err = ValidateConflictProof(req, snapshot, proofContradict)
	require.Error(t, err)
	require.Contains(t, err.Error(), "contradicts AllocationMappings")
}

func TestFormatCanonicalClaims_AggregationAndOverflow(t *testing.T) {
	// 1. Verify [{tuner:A, 1}, {tuner:A, 1}] is identical to [{tuner:A, 2}]
	splitClaims := []ResourceClaim{
		{Kind: ResourceKindTunerSlot, Resource: "tuner-A", Quantity: 1},
		{Kind: ResourceKindTunerSlot, Resource: "tuner-A", Quantity: 1},
	}
	combinedClaim := []ResourceClaim{
		{Kind: ResourceKindTunerSlot, Resource: "tuner-A", Quantity: 2},
	}

	strSplit, err1 := formatCanonicalClaimsStrict(splitClaims)
	require.NoError(t, err1)
	strCombined, err2 := formatCanonicalClaimsStrict(combinedClaim)
	require.NoError(t, err2)

	require.Equal(t, "TUNER_SLOT:tuner-A:2", strSplit)
	require.Equal(t, strCombined, strSplit)

	// 2. Reject negative quantities
	negativeClaims := []ResourceClaim{
		{Kind: ResourceKindTunerSlot, Resource: "tuner-A", Quantity: -1},
	}
	_, errNeg := formatCanonicalClaimsStrict(negativeClaims)
	require.Error(t, errNeg)

	// 3. Reject quantity overflow
	overflowClaims := []ResourceClaim{
		{Kind: ResourceKindTunerSlot, Resource: "tuner-A", Quantity: MaxClaimQuantitySum + 1},
	}
	_, errOverflow := formatCanonicalClaimsStrict(overflowClaims)
	require.Error(t, errOverflow)
}

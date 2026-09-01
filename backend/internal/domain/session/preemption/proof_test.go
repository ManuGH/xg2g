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

	// 3. Reject quantity overflow (single claim)
	overflowClaims := []ResourceClaim{
		{Kind: ResourceKindTunerSlot, Resource: "tuner-A", Quantity: MaxClaimQuantitySum + 1},
	}
	_, errOverflow := formatCanonicalClaimsStrict(overflowClaims)
	require.Error(t, errOverflow)

	// 4. Reject sum-based quantity overflow (each claim <= MaxClaimQuantitySum, but sum > MaxClaimQuantitySum)
	sumOverflowClaims := []ResourceClaim{
		{Kind: ResourceKindTunerSlot, Resource: "tuner-A", Quantity: 600000},
		{Kind: ResourceKindTunerSlot, Resource: "tuner-A", Quantity: 500001},
	}
	_, errSumOverflow := formatCanonicalClaimsStrict(sumOverflowClaims)
	require.Error(t, errSumOverflow)
	require.Contains(t, errSumOverflow.Error(), "overflow")
}

// Demand split across entries is still demand.
//
// Split claims for one resource are a supported representation in this package -
// formatCanonicalClaimsStrict reads [{tuner-1,1},{tuner-1,1}] as [{tuner-1,2}],
// and TestFormatCanonicalClaims_AggregationAndOverflow pins that. SatisfiesResources
// aggregated only the freed side and compared each requested entry against that
// total, so two units of demand read as one and a plan freeing half of what was
// asked for was approved. It is called from both Evaluator.SelectPlan and
// ValidateContract, so the answer decides whether a preemption runs.
func TestSatisfiesResources_SplitRequestedClaimsAreTotalled(t *testing.T) {
	requested := []ResourceClaim{
		{Kind: ResourceKindPhysicalTuner, Resource: "tuner-1", Quantity: 1},
		{Kind: ResourceKindPhysicalTuner, Resource: "tuner-1", Quantity: 1},
	}
	freed := []ResourceClaim{
		{Kind: ResourceKindPhysicalTuner, Resource: "tuner-1", Quantity: 1},
	}

	require.False(t, SatisfiesResources(requested, freed),
		"two units of demand against one unit freed must not be satisfied")

	// And the same demand written as one entry, so the test cannot pass by
	// rejecting everything.
	require.True(t, SatisfiesResources(
		[]ResourceClaim{{Kind: ResourceKindPhysicalTuner, Resource: "tuner-1", Quantity: 2}},
		[]ResourceClaim{{Kind: ResourceKindPhysicalTuner, Resource: "tuner-1", Quantity: 2}},
	), "demand equal to supply must still be satisfied")

	// Split on both sides, adding up: also satisfied.
	require.True(t, SatisfiesResources(requested, []ResourceClaim{
		{Kind: ResourceKindPhysicalTuner, Resource: "tuner-1", Quantity: 1},
		{Kind: ResourceKindPhysicalTuner, Resource: "tuner-1", Quantity: 1},
	}), "split demand covered by split supply must be satisfied")
}

// A proof may not free more of a resource than the allocation owns, however the
// freeing is written down.
//
// The ownership check compared each FreedResources entry against the aggregated
// allocClaimMap, so splitting one resource across two entries let a proof claim
// to free two units of a tuner the allocation holds one of.
func TestValidateConflictProof_SplitFreedClaimsCannotExceedOwnership(t *testing.T) {
	oneTuner := []ResourceClaim{{Kind: ResourceKindTunerSlot, Resource: "tuner-1", Quantity: 1}}

	req := PreemptionRequest{
		RequestID:             "req-split",
		ReceiverID:            "rec-1",
		RequesterOwner:        "client-A",
		RequesterAllocationID: "alloc-req-1",
		RequesterRevision:     "rev-1",
		RequestedResources:    oneTuner,
	}
	snapshot := ResourceSnapshot{
		ReceiverID:       "rec-1",
		SnapshotRevision: "rev-100",
		ObservedAt:       time.Now().UTC(),
		Allocations: []ActiveAllocation{
			{AllocationID: "alloc-1", Claims: oneTuner}, // owns exactly one
		},
	}
	proof := ConflictResolutionProof{
		ReceiverID:              "rec-1",
		SnapshotRevision:        "rev-100",
		HardwareProfileRevision: "hw-rev-1",
		HardwareProfileStatus:   HardwareProfileValid,
		EvidenceClassification:  EvidenceDirectObservation,
		RequestedResources:      oneTuner,
		AllocationMappings: []AllocationResourceMapping{{
			AllocationID: "alloc-1",
			// Two entries of one: two units, from an allocation owning one.
			FreedResources: []ResourceClaim{
				{Kind: ResourceKindTunerSlot, Resource: "tuner-1", Quantity: 1},
				{Kind: ResourceKindTunerSlot, Resource: "tuner-1", Quantity: 1},
			},
		}},
	}

	err := ValidateConflictProof(req, snapshot, proof)
	require.Error(t, err, "freeing two units from an allocation owning one must be rejected")
	require.Contains(t, err.Error(), "not owned by target allocation")
}

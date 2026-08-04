// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package preemption

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func setupReleaseTestContext(t *testing.T) (context.Context, *PreparedTeardown, TargetTeardownEvidence, ReleaseObservation, time.Time) {
	now := time.Now().UTC()
	attemptAt := now.Add(-2 * time.Second)
	ackAt := now.Add(-1 * time.Second)
	obsAt := now.Add(-500 * time.Millisecond)

	hwClaim := []ResourceClaim{{Kind: ResourceKindPhysicalTuner, Resource: "tuner-1", Quantity: 1}}

	contract := &PreemptionExecutionContract{
		ContractID:              "c-1",
		ReceiverID:              "rec-1",
		RequestID:               "req-1",
		RequesterOwner:          "client-A",
		RequesterAllocationID:   "alloc-req-1",
		RequesterRevision:       "rev-req-1",
		TargetAllocationIDs:     []string{"alloc-1"},
		RequestedResources:      hwClaim,
		ExpectedFreedResources:  hwClaim,
		SnapshotRevision:        "rev-100",
		HardwareProfileRevision: "hw-rev-1",
		ConflictProofRevision:   "rev-100",
		CreatedAt:               now.Add(-10 * time.Second),
		ExpiresAt:               now.Add(30 * time.Second),
	}
	cHash, err := ComputeContractHash(contract)
	require.NoError(t, err)
	contract.ContractHash = cHash

	snapshot := ResourceSnapshot{
		ReceiverID:       "rec-1",
		SnapshotRevision: "rev-100",
		ObservedAt:       now.Add(-5 * time.Second),
		Allocations: []ActiveAllocation{
			{
				AllocationID: "alloc-1",
				Owner:        "client-B",
				Revision:     "alloc-rev-100",
				Priority:     PriorityAttributes{BasePriority: 50},
				Claims:       hwClaim,
			},
		},
	}

	proof := ConflictResolutionProof{
		ReceiverID:              "rec-1",
		SnapshotRevision:        "rev-100",
		HardwareProfileRevision: "hw-rev-1",
		HardwareProfileStatus:   HardwareProfileValid,
		EvidenceClassification:  EvidenceDirectObservation,
		RequestedResources:      hwClaim,
		AllocationMappings: []AllocationResourceMapping{
			{AllocationID: "alloc-1", FreedResources: hwClaim},
		},
	}

	preparer := NewTeardownPreparer()
	prepRes, err := preparer.PrepareTeardown(contract, snapshot, proof, now.Add(-5*time.Second))
	require.NoError(t, err)
	require.Equal(t, DecisionApproved, prepRes.Decision)

	prepared := prepRes.Prepared

	// Set HardwareBindingsAuthoritative = true for successful empirical verification in setup context
	prepared.TargetDescriptors[0].Coverage.HardwareBindingsAuthoritative = true
	descHash, err := ComputeDescriptorHash(prepared.TargetDescriptors[0])
	require.NoError(t, err)
	prepared.TargetDescriptors[0].DescriptorHash = descHash

	descHashHeader, err := ComputeTargetDescriptorsHash(prepared.TargetDescriptors)
	require.NoError(t, err)
	prepared.TargetDescriptorsHash = descHashHeader

	prepHash, err := ComputePreparedTeardownHash(prepared)
	require.NoError(t, err)
	prepared.PreparedTeardownHash = prepHash

	ev := TargetTeardownEvidence{
		SagaID:               "saga-1",
		PreparedTeardownHash: prepared.PreparedTeardownHash,
		TargetAllocationID:   "alloc-1",
		DescriptorHash:       prepared.TargetDescriptors[0].DescriptorHash,
		FencingToken:         10,
		Status:               TeardownStatusStopConfirmed,
		AttemptedAt:          attemptAt,
		AcknowledgedAt:       ackAt,
	}
	evHash, err := ComputeEvidenceHash(ev)
	require.NoError(t, err)
	ev.EvidenceHash = evHash

	obs := ReleaseObservation{
		ReceiverID:               "rec-1",
		ObservationRevision:      "obs-rev-1",
		AllocationSourceRevision: "alloc-src-1",
		HardwareSourceRevision:   "hw-src-1",
		LeaseSourceRevision:      "lease-src-1",
		HardwareProfileRevision:  "hw-rev-1",
		Evidence:                 EvidenceDirectObservation,
		Coverage: ObservationCoverage{
			AllocationsComplete:      true,
			HardwareBindingsComplete: true,
			LeaseBindingsComplete:    true,
		},
		ObservedAt:             obsAt,
		ActiveAllocations:      []ActiveAllocation{},        // alloc-1 is GONE!
		ActiveHardwareBindings: []ObservedResourceBinding{}, // hardware binding is GONE!
		ActiveLeaseBindings:    []ObservedLeaseBinding{},    // lease binding is GONE!
	}
	obsHash, err := ComputeObservationHash(obs)
	require.NoError(t, err)
	obs.ObservationHash = obsHash

	return context.Background(), prepared, ev, obs, now
}

func TestVerifyRelease_EmpiricallyConfirmed(t *testing.T) {
	_, prepared, ev, obs, now := setupReleaseTestContext(t)
	verifier := NewResourceReleaseVerifier()

	res, err := verifier.VerifyRelease(prepared, []TargetTeardownEvidence{ev}, obs, now)
	require.NoError(t, err)
	require.Equal(t, ReleaseDecisionReleased, res.Decision)
	require.Equal(t, ReleaseVerificationReasonNone, res.Reason)
	require.False(t, res.VerifiedAt.IsZero())
	require.Len(t, res.TargetResults, 1)
	require.Equal(t, TargetReleased, res.TargetResults[0].ReleaseState)
	require.Equal(t, ReleaseReasonNone, res.TargetResults[0].Reason)
	require.Len(t, res.ReleasedFromTargetClaims, 1)
	require.Len(t, res.CurrentlyFreeClaims, 1)
	require.Equal(t, 1, res.TargetResults[0].Resources[0].ExpectedQuantity)
	require.Equal(t, 0, res.TargetResults[0].Resources[0].RemainingTargetQuantity)
	require.Equal(t, 1, res.TargetResults[0].Resources[0].ReleasedFromTargetQuantity)
}

func TestVerifyRelease_HardwareClaimWithoutAuthoritativeCoverageRejection(t *testing.T) {
	_, prepared, ev, obs, now := setupReleaseTestContext(t)
	verifier := NewResourceReleaseVerifier()

	// HardwareBindingsAuthoritative = false -> MUST reject fail-closed!
	prepared.TargetDescriptors[0].Coverage.HardwareBindingsAuthoritative = false
	descHash, err := ComputeDescriptorHash(prepared.TargetDescriptors[0])
	require.NoError(t, err)
	prepared.TargetDescriptors[0].DescriptorHash = descHash

	descHashHeader, err := ComputeTargetDescriptorsHash(prepared.TargetDescriptors)
	require.NoError(t, err)
	prepared.TargetDescriptorsHash = descHashHeader

	prepHash, err := ComputePreparedTeardownHash(prepared)
	require.NoError(t, err)
	prepared.PreparedTeardownHash = prepHash

	ev.PreparedTeardownHash = prepared.PreparedTeardownHash
	ev.DescriptorHash = prepared.TargetDescriptors[0].DescriptorHash
	evHash, err := ComputeEvidenceHash(ev)
	require.NoError(t, err)
	ev.EvidenceHash = evHash

	res, err := verifier.VerifyRelease(prepared, []TargetTeardownEvidence{ev}, obs, now)
	require.NoError(t, err)
	require.Equal(t, ReleaseDecisionRejected, res.Decision)
	require.Equal(t, ReleaseVerificationReasonEvidenceIncomplete, res.Reason, "MUST reject fail-closed when hardware claim lacks authoritative coverage")
}

func TestVerifyRelease_LeaseClaimWithoutAuthoritativeCoverageRejection(t *testing.T) {
	_, prepared, ev, obs, now := setupReleaseTestContext(t)
	verifier := NewResourceReleaseVerifier()

	// Add lease claim (TUNER_SLOT) but keep LeaseBindingsAuthoritative = false
	prepared.TargetDescriptors[0].ExpectedClaims = append(prepared.TargetDescriptors[0].ExpectedClaims, ResourceClaim{Kind: ResourceKindTunerSlot, Resource: "slot-1", Quantity: 1})
	prepared.TargetDescriptors[0].Coverage.LeaseBindingsAuthoritative = false

	descHash, err := ComputeDescriptorHash(prepared.TargetDescriptors[0])
	require.NoError(t, err)
	prepared.TargetDescriptors[0].DescriptorHash = descHash

	descHashHeader, err := ComputeTargetDescriptorsHash(prepared.TargetDescriptors)
	require.NoError(t, err)
	prepared.TargetDescriptorsHash = descHashHeader

	prepHash, err := ComputePreparedTeardownHash(prepared)
	require.NoError(t, err)
	prepared.PreparedTeardownHash = prepHash

	ev.PreparedTeardownHash = prepared.PreparedTeardownHash
	ev.DescriptorHash = prepared.TargetDescriptors[0].DescriptorHash
	evHash, err := ComputeEvidenceHash(ev)
	require.NoError(t, err)
	ev.EvidenceHash = evHash

	res, err := verifier.VerifyRelease(prepared, []TargetTeardownEvidence{ev}, obs, now)
	require.NoError(t, err)
	require.Equal(t, ReleaseDecisionRejected, res.Decision)
	require.Equal(t, ReleaseVerificationReasonEvidenceIncomplete, res.Reason, "MUST reject fail-closed when lease claim lacks authoritative coverage")
}

func TestVerifyRelease_PreExistingOtherUsageIsOtherObserved(t *testing.T) {
	_, prepared, ev, obs, now := setupReleaseTestContext(t)
	verifier := NewResourceReleaseVerifier()

	obs.ActiveHardwareBindings = []ObservedResourceBinding{
		{Kind: ResourceKindPhysicalTuner, Resource: "tuner-1", Quantity: 1, AllocationID: "alloc-C"},
	}
	obsHash, err := ComputeObservationHash(obs)
	require.NoError(t, err)
	obs.ObservationHash = obsHash

	res, err := verifier.VerifyRelease(prepared, []TargetTeardownEvidence{ev}, obs, now)
	require.NoError(t, err)
	require.Equal(t, ReleaseDecisionReleased, res.Decision)
	require.Equal(t, ReleaseVerificationReasonNone, res.Reason)
	require.Equal(t, ResourceOtherObserved, res.TargetResults[0].Resources[0].Disposition, "MUST be reported as OTHER_OBSERVED, not REASSIGNED")
	require.Equal(t, 1, res.TargetResults[0].Resources[0].OtherObservedQuantity)
}

func TestVerifyRelease_OtherUsageExceedingReleasedQuantity(t *testing.T) {
	_, prepared, ev, obs, now := setupReleaseTestContext(t)
	verifier := NewResourceReleaseVerifier()

	obs.ActiveHardwareBindings = []ObservedResourceBinding{
		{Kind: ResourceKindPhysicalTuner, Resource: "tuner-1", Quantity: 4, AllocationID: "alloc-C"},
	}
	obsHash, err := ComputeObservationHash(obs)
	require.NoError(t, err)
	obs.ObservationHash = obsHash

	res, err := verifier.VerifyRelease(prepared, []TargetTeardownEvidence{ev}, obs, now)
	require.NoError(t, err)
	require.Equal(t, ReleaseDecisionReleased, res.Decision)
	require.Equal(t, 1, res.TargetResults[0].Resources[0].ReleasedFromTargetQuantity)
	require.Equal(t, 4, res.TargetResults[0].Resources[0].OtherObservedQuantity)
	require.Equal(t, 0, res.TargetResults[0].Resources[0].CurrentlyFreeQuantity, "Free quantity MUST NOT become negative")
}

func TestVerifyRelease_MultipleTargetsAggregatedOutput(t *testing.T) {
	now := time.Now().UTC()
	attemptAt := now.Add(-2 * time.Second)
	ackAt := now.Add(-1 * time.Second)

	hwClaim1 := []ResourceClaim{{Kind: ResourceKindPhysicalTuner, Resource: "tuner-1", Quantity: 1}}
	hwClaim2 := []ResourceClaim{{Kind: ResourceKindPhysicalTuner, Resource: "tuner-1", Quantity: 1}}

	contract := &PreemptionExecutionContract{
		ContractID:              "c-multi",
		ReceiverID:              "rec-1",
		RequestID:               "req-1",
		RequesterOwner:          "client-A",
		RequesterAllocationID:   "alloc-req-1",
		RequesterRevision:       "rev-req-1",
		TargetAllocationIDs:     []string{"alloc-1", "alloc-2"},
		RequestedResources:      []ResourceClaim{{Kind: ResourceKindPhysicalTuner, Resource: "tuner-1", Quantity: 2}},
		ExpectedFreedResources:  []ResourceClaim{{Kind: ResourceKindPhysicalTuner, Resource: "tuner-1", Quantity: 2}},
		SnapshotRevision:        "rev-100",
		HardwareProfileRevision: "hw-rev-1",
		ConflictProofRevision:   "rev-100",
		CreatedAt:               now.Add(-10 * time.Second),
		ExpiresAt:               now.Add(30 * time.Second),
	}
	cHash, err := ComputeContractHash(contract)
	require.NoError(t, err)
	contract.ContractHash = cHash

	snapshot := ResourceSnapshot{
		ReceiverID:       "rec-1",
		SnapshotRevision: "rev-100",
		ObservedAt:       now.Add(-5 * time.Second),
		Allocations: []ActiveAllocation{
			{AllocationID: "alloc-1", Owner: "client-B", Revision: "rev-1", Priority: PriorityAttributes{BasePriority: 50}, Claims: hwClaim1},
			{AllocationID: "alloc-2", Owner: "client-C", Revision: "rev-2", Priority: PriorityAttributes{BasePriority: 40}, Claims: hwClaim2},
		},
	}

	proof := ConflictResolutionProof{
		ReceiverID:              "rec-1",
		SnapshotRevision:        "rev-100",
		HardwareProfileRevision: "hw-rev-1",
		HardwareProfileStatus:   HardwareProfileValid,
		EvidenceClassification:  EvidenceDirectObservation,
		RequestedResources:      []ResourceClaim{{Kind: ResourceKindPhysicalTuner, Resource: "tuner-1", Quantity: 2}},
		AllocationMappings: []AllocationResourceMapping{
			{AllocationID: "alloc-1", FreedResources: hwClaim1},
			{AllocationID: "alloc-2", FreedResources: hwClaim2},
		},
	}

	preparer := NewTeardownPreparer()
	prepRes, err := preparer.PrepareTeardown(contract, snapshot, proof, now.Add(-5*time.Second))
	require.NoError(t, err)
	prepared := prepRes.Prepared

	for i := range prepared.TargetDescriptors {
		prepared.TargetDescriptors[i].Coverage.HardwareBindingsAuthoritative = true
		descHash, dErr := ComputeDescriptorHash(prepared.TargetDescriptors[i])
		require.NoError(t, dErr)
		prepared.TargetDescriptors[i].DescriptorHash = descHash
	}
	descHashHeader, err := ComputeTargetDescriptorsHash(prepared.TargetDescriptors)
	require.NoError(t, err)
	prepared.TargetDescriptorsHash = descHashHeader
	prepHash, err := ComputePreparedTeardownHash(prepared)
	require.NoError(t, err)
	prepared.PreparedTeardownHash = prepHash

	ev1 := TargetTeardownEvidence{
		SagaID:               "saga-1",
		PreparedTeardownHash: prepared.PreparedTeardownHash,
		TargetAllocationID:   "alloc-1",
		DescriptorHash:       prepared.TargetDescriptors[0].DescriptorHash,
		FencingToken:         10,
		Status:               TeardownStatusStopConfirmed,
		AttemptedAt:          attemptAt,
		AcknowledgedAt:       ackAt,
	}
	ev1Hash, err := ComputeEvidenceHash(ev1)
	require.NoError(t, err)
	ev1.EvidenceHash = ev1Hash

	ev2 := TargetTeardownEvidence{
		SagaID:               "saga-1",
		PreparedTeardownHash: prepared.PreparedTeardownHash,
		TargetAllocationID:   "alloc-2",
		DescriptorHash:       prepared.TargetDescriptors[1].DescriptorHash,
		FencingToken:         10,
		Status:               TeardownStatusStopConfirmed,
		AttemptedAt:          attemptAt,
		AcknowledgedAt:       ackAt,
	}
	ev2Hash, err := ComputeEvidenceHash(ev2)
	require.NoError(t, err)
	ev2.EvidenceHash = ev2Hash

	obs := ReleaseObservation{
		ReceiverID:               "rec-1",
		ObservationRevision:      "obs-rev-1",
		AllocationSourceRevision: "alloc-src-1",
		HardwareSourceRevision:   "hw-src-1",
		LeaseSourceRevision:      "lease-src-1",
		HardwareProfileRevision:  "hw-rev-1",
		Evidence:                 EvidenceDirectObservation,
		Coverage: ObservationCoverage{
			AllocationsComplete:      true,
			HardwareBindingsComplete: true,
			LeaseBindingsComplete:    true,
		},
		ObservedAt:             now.Add(-500 * time.Millisecond),
		ActiveAllocations:      []ActiveAllocation{},
		ActiveHardwareBindings: []ObservedResourceBinding{},
		ActiveLeaseBindings:    []ObservedLeaseBinding{},
	}
	obsHash, err := ComputeObservationHash(obs)
	require.NoError(t, err)
	obs.ObservationHash = obsHash

	verifier := NewResourceReleaseVerifier()
	res, err := verifier.VerifyRelease(prepared, []TargetTeardownEvidence{ev1, ev2}, obs, now)
	require.NoError(t, err)
	require.Equal(t, ReleaseDecisionReleased, res.Decision)
	require.Len(t, res.ReleasedFromTargetClaims, 1, "Multiple victim claims MUST be aggregated into a single claim entry")
	require.Equal(t, 2, res.ReleasedFromTargetClaims[0].Quantity)
}

func TestVerifyRelease_EquivalentSplitBindingsIdenticalHash(t *testing.T) {
	obs1 := ReleaseObservation{
		ReceiverID:               "rec-1",
		ObservationRevision:      "obs-rev-1",
		AllocationSourceRevision: "alloc-src-1",
		HardwareSourceRevision:   "hw-src-1",
		LeaseSourceRevision:      "lease-src-1",
		HardwareProfileRevision:  "hw-rev-1",
		Evidence:                 EvidenceDirectObservation,
		ObservedAt:               time.Now().UTC(),
		ActiveHardwareBindings: []ObservedResourceBinding{
			{Kind: ResourceKindPhysicalTuner, Resource: "tuner-1", Quantity: 1, AllocationID: "alloc-1"},
			{Kind: ResourceKindPhysicalTuner, Resource: "tuner-1", Quantity: 1, AllocationID: "alloc-1"},
		},
	}
	obs2 := ReleaseObservation{
		ReceiverID:               "rec-1",
		ObservationRevision:      "obs-rev-1",
		AllocationSourceRevision: "alloc-src-1",
		HardwareSourceRevision:   "hw-src-1",
		LeaseSourceRevision:      "lease-src-1",
		HardwareProfileRevision:  "hw-rev-1",
		Evidence:                 EvidenceDirectObservation,
		ObservedAt:               obs1.ObservedAt,
		ActiveHardwareBindings: []ObservedResourceBinding{
			{Kind: ResourceKindPhysicalTuner, Resource: "tuner-1", Quantity: 2, AllocationID: "alloc-1"},
		},
	}

	hash1, err1 := ComputeObservationHash(obs1)
	require.NoError(t, err1)
	hash2, err2 := ComputeObservationHash(obs2)
	require.NoError(t, err2)

	require.Equal(t, hash1, hash2, "ObservationHash MUST be identical for quantity-aggregated split bindings")
}

func TestVerifyRelease_SumQuantityOverflowRejection(t *testing.T) {
	obs := ReleaseObservation{
		ReceiverID:               "rec-1",
		ObservationRevision:      "obs-rev-1",
		AllocationSourceRevision: "alloc-src-1",
		HardwareSourceRevision:   "hw-src-1",
		LeaseSourceRevision:      "lease-src-1",
		HardwareProfileRevision:  "hw-rev-1",
		Evidence:                 EvidenceDirectObservation,
		ObservedAt:               time.Now().UTC(),
		ActiveHardwareBindings: []ObservedResourceBinding{
			{Kind: ResourceKindPhysicalTuner, Resource: "tuner-1", Quantity: MaxClaimQuantitySum, AllocationID: "alloc-1"},
			{Kind: ResourceKindPhysicalTuner, Resource: "tuner-1", Quantity: 1, AllocationID: "alloc-1"},
		},
	}
	_, err := ComputeObservationHash(obs)
	require.Error(t, err, "ComputeObservationHash MUST fail when quantity sum exceeds MaxClaimQuantitySum")
}

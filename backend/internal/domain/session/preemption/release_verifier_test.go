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
	require.Equal(t, 1, res.TargetResults[0].Resources[0].ReleasedQuantity)
	require.Equal(t, 1, res.TargetResults[0].Resources[0].CurrentlyFreeQuantity)
}

func TestVerifyRelease_ExpectedLeaseBindingsEmptyByDefault(t *testing.T) {
	_, prepared, _, _, _ := setupReleaseTestContext(t)
	require.Empty(t, prepared.TargetDescriptors[0].ExpectedLeaseBindings, "ExpectedLeaseBindings MUST remain empty until authoritative lease store is integrated")
}

func TestVerifyRelease_QuantityAggregatedCanonicalization(t *testing.T) {
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

	require.Equal(t, hash1, hash2, "ObservationHash MUST be identical for quantity-aggregated bindings")
}

func TestVerifyRelease_ObservationFailsOnEmptyOwnerOrRevision(t *testing.T) {
	obs := ReleaseObservation{
		ReceiverID:               "rec-1",
		ObservationRevision:      "obs-rev-1",
		AllocationSourceRevision: "alloc-src-1",
		HardwareSourceRevision:   "hw-src-1",
		LeaseSourceRevision:      "lease-src-1",
		HardwareProfileRevision:  "hw-rev-1",
		Evidence:                 EvidenceDirectObservation,
		ObservedAt:               time.Now().UTC(),
		ActiveAllocations: []ActiveAllocation{
			{AllocationID: "alloc-1", Owner: "", Revision: "rev-1"},
		},
	}
	_, err := ComputeObservationHash(obs)
	require.Error(t, err, "ComputeObservationHash MUST fail if owner is empty")
}

func TestVerifyRelease_IncompleteCoverageRejection(t *testing.T) {
	_, prepared, ev, obs, now := setupReleaseTestContext(t)
	verifier := NewResourceReleaseVerifier()

	obs.Coverage.HardwareBindingsComplete = false
	obsHash, err := ComputeObservationHash(obs)
	require.NoError(t, err)
	obs.ObservationHash = obsHash

	res, err := verifier.VerifyRelease(prepared, []TargetTeardownEvidence{ev}, obs, now)
	require.NoError(t, err)
	require.Equal(t, ReleaseDecisionRejected, res.Decision)
	require.Equal(t, ReleaseVerificationReasonEvidenceIncomplete, res.Reason)
	require.True(t, res.VerifiedAt.IsZero())
}

func TestVerifyRelease_ResourceReassigned(t *testing.T) {
	_, prepared, ev, obs, now := setupReleaseTestContext(t)
	verifier := NewResourceReleaseVerifier()

	// Reassign tuner-1 to alloc-C
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
	require.Len(t, res.TargetResults, 1)
	require.Equal(t, TargetReleased, res.TargetResults[0].ReleaseState)
	require.Len(t, res.ReassignedHardwareBindings, 1)
	require.Equal(t, "alloc-C", res.ReassignedHardwareBindings[0].AllocationID)
	require.Equal(t, 1, res.TargetResults[0].Resources[0].ReassignedQuantity)
	require.Equal(t, 0, res.TargetResults[0].Resources[0].CurrentlyFreeQuantity)
}

func TestVerifyRelease_TargetStillActive(t *testing.T) {
	_, prepared, ev, obs, now := setupReleaseTestContext(t)
	verifier := NewResourceReleaseVerifier()

	obs.ActiveAllocations = []ActiveAllocation{
		{AllocationID: "alloc-1", Owner: "client-B", Revision: "alloc-rev-100"},
	}
	obsHash, err := ComputeObservationHash(obs)
	require.NoError(t, err)
	obs.ObservationHash = obsHash

	res, err := verifier.VerifyRelease(prepared, []TargetTeardownEvidence{ev}, obs, now)
	require.NoError(t, err)
	require.Equal(t, ReleaseDecisionRejected, res.Decision)
	require.Equal(t, ReleaseVerificationReasonTargetsNotReleased, res.Reason)
	require.Equal(t, TargetStillActive, res.TargetResults[0].ReleaseState)
	require.Equal(t, ReleaseReasonTargetStillObserved, res.TargetResults[0].Reason)
	require.Equal(t, ResourceUnknown, res.TargetResults[0].Resources[0].Disposition)
}

func TestVerifyRelease_TargetBindingRemains(t *testing.T) {
	_, prepared, ev, obs, now := setupReleaseTestContext(t)
	verifier := NewResourceReleaseVerifier()

	obs.ActiveHardwareBindings = []ObservedResourceBinding{
		{Kind: ResourceKindPhysicalTuner, Resource: "tuner-1", Quantity: 1, AllocationID: "alloc-1"},
	}
	obsHash, err := ComputeObservationHash(obs)
	require.NoError(t, err)
	obs.ObservationHash = obsHash

	res, err := verifier.VerifyRelease(prepared, []TargetTeardownEvidence{ev}, obs, now)
	require.NoError(t, err)
	require.Equal(t, ReleaseDecisionRejected, res.Decision)
	require.Equal(t, ReleaseVerificationReasonTargetsNotReleased, res.Reason)
	require.Equal(t, TargetBindingRemains, res.TargetResults[0].ReleaseState)
	require.Equal(t, ReleaseReasonHardwareBinding, res.TargetResults[0].Reason)
	require.Equal(t, ResourceUnknown, res.TargetResults[0].Resources[0].Disposition)
}

func TestVerifyRelease_FencingTokenMismatchInEvidence(t *testing.T) {
	now := time.Now().UTC()
	attemptAt := now.Add(-2 * time.Second)
	ackAt := now.Add(-1 * time.Second)

	hwClaim1 := []ResourceClaim{{Kind: ResourceKindPhysicalTuner, Resource: "tuner-1", Quantity: 1}}
	hwClaim2 := []ResourceClaim{{Kind: ResourceKindPhysicalTuner, Resource: "tuner-2", Quantity: 1}}
	bothClaims := append(append([]ResourceClaim{}, hwClaim1...), hwClaim2...)

	contract := &PreemptionExecutionContract{
		ContractID:              "c-multi",
		ReceiverID:              "rec-1",
		RequestID:               "req-1",
		RequesterOwner:          "client-A",
		RequesterAllocationID:   "alloc-req-1",
		RequesterRevision:       "rev-req-1",
		TargetAllocationIDs:     []string{"alloc-1", "alloc-2"},
		RequestedResources:      bothClaims,
		ExpectedFreedResources:  bothClaims,
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
		RequestedResources:      bothClaims,
		AllocationMappings: []AllocationResourceMapping{
			{AllocationID: "alloc-1", FreedResources: hwClaim1},
			{AllocationID: "alloc-2", FreedResources: hwClaim2},
		},
	}

	preparer := NewTeardownPreparer()
	prepRes, err := preparer.PrepareTeardown(contract, snapshot, proof, now.Add(-5*time.Second))
	require.NoError(t, err)
	prepared := prepRes.Prepared

	ev1 := TargetTeardownEvidence{
		SagaID:               "saga-1",
		PreparedTeardownHash: prepared.PreparedTeardownHash,
		TargetAllocationID:   "alloc-1",
		DescriptorHash:       prepared.TargetDescriptors[0].DescriptorHash,
		FencingToken:         10, // Fencing Token 10
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
		FencingToken:         11, // Fencing Token 11 -> MISMATCH!
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
	require.Equal(t, ReleaseDecisionRejected, res.Decision)
	require.Equal(t, ReleaseVerificationReasonBijectionInvalid, res.Reason, "Fencing tokens across evidence items MUST match")
}

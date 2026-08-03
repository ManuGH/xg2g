// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package preemption

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrInvalidProof               = errors.New("invalid conflict resolution proof")
	ErrProofReceiverMismatch      = errors.New("proof receiver ID does not match request")
	ErrProofRevisionMismatch      = errors.New("proof snapshot revision does not match snapshot")
	ErrProofStaleHardware         = errors.New("proof hardware profile is stale or unverified")
	ErrProofUnmappedAllocation    = errors.New("proof target allocation not active in snapshot")
	ErrProofIncompleteFreedClaims = errors.New("freed resources do not satisfy requested resources")
)

// ValidateConflictProof checks whether proof matches request & snapshot and proves resource coverage.
func ValidateConflictProof(req PreemptionRequest, snapshot ResourceSnapshot, proof ConflictResolutionProof) error {
	if strings.TrimSpace(proof.ReceiverID) == "" || proof.ReceiverID != req.ReceiverID {
		return fmt.Errorf("%w: proof receiver '%s' != request receiver '%s'", ErrProofReceiverMismatch, proof.ReceiverID, req.ReceiverID)
	}
	if strings.TrimSpace(proof.SnapshotRevision) == "" || proof.SnapshotRevision != snapshot.SnapshotRevision {
		return fmt.Errorf("%w: proof revision '%s' != snapshot revision '%s'", ErrProofRevisionMismatch, proof.SnapshotRevision, snapshot.SnapshotRevision)
	}
	if strings.TrimSpace(proof.HardwareProfileRevision) == "" || proof.HardwareProfileStatus != HardwareProfileValid {
		return fmt.Errorf("%w: status '%s'", ErrProofStaleHardware, proof.HardwareProfileStatus)
	}
	if proof.EvidenceClassification != EvidenceDirectObservation && proof.EvidenceClassification != EvidenceInferred {
		return fmt.Errorf("%w: unverified evidence classification '%s'", ErrInvalidProof, proof.EvidenceClassification)
	}
	if len(proof.RequestedResources) == 0 {
		return fmt.Errorf("%w: proof contains zero requested resources", ErrInvalidProof)
	}

	// Verify all target allocation mappings correspond to active allocations in snapshot
	activeMap := make(map[string]ActiveAllocation, len(snapshot.Allocations))
	for _, alloc := range snapshot.Allocations {
		activeMap[alloc.AllocationID] = alloc
	}

	for _, mapping := range proof.AllocationMappings {
		if strings.TrimSpace(mapping.AllocationID) == "" {
			return fmt.Errorf("%w: empty allocation ID in proof mapping", ErrInvalidProof)
		}
		alloc, ok := activeMap[mapping.AllocationID]
		if !ok {
			return fmt.Errorf("%w: allocation '%s' in proof is not active in snapshot", ErrProofUnmappedAllocation, mapping.AllocationID)
		}
		if len(mapping.FreedResources) == 0 {
			return fmt.Errorf("%w: mapping for allocation '%s' contains zero freed resources", ErrInvalidProof, mapping.AllocationID)
		}
		allocClaimMap := make(map[string]struct{})
		for _, claim := range alloc.Claims {
			allocClaimMap[fmt.Sprintf("%s:%s", claim.Kind, claim.Resource)] = struct{}{}
		}
		for _, freed := range mapping.FreedResources {
			key := fmt.Sprintf("%s:%s", freed.Kind, freed.Resource)
			if _, hasClaim := allocClaimMap[key]; !hasClaim && freed.Kind != ResourceKindPhysicalTuner && freed.Kind != ResourceKindDemux {
				return fmt.Errorf("%w: freed claim '%s' not owned by target allocation '%s'", ErrInvalidProof, key, mapping.AllocationID)
			}
		}
	}

	return nil
}

// SatisfiesResources checks if freedClaims cover requestedClaims completely.
func SatisfiesResources(requested []ResourceClaim, freed []ResourceClaim) bool {
	freedMap := make(map[string]int)
	for _, claim := range freed {
		key := fmt.Sprintf("%s:%s", claim.Kind, claim.Resource)
		freedMap[key] += claim.Quantity
	}

	for _, claim := range requested {
		key := fmt.Sprintf("%s:%s", claim.Kind, claim.Resource)
		if freedMap[key] < claim.Quantity {
			return false
		}
	}
	return true
}

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
	if strings.TrimSpace(snapshot.ReceiverID) == "" || snapshot.ReceiverID != req.ReceiverID {
		return fmt.Errorf("%w: snapshot receiver '%s' != request receiver '%s'", ErrProofReceiverMismatch, snapshot.ReceiverID, req.ReceiverID)
	}
	if strings.TrimSpace(proof.SnapshotRevision) == "" || proof.SnapshotRevision != snapshot.SnapshotRevision {
		return fmt.Errorf("%w: proof revision '%s' != snapshot revision '%s'", ErrProofRevisionMismatch, proof.SnapshotRevision, snapshot.SnapshotRevision)
	}
	if strings.TrimSpace(proof.HardwareProfileRevision) == "" || proof.HardwareProfileStatus != HardwareProfileValid {
		return fmt.Errorf("%w: status '%s'", ErrProofStaleHardware, proof.HardwareProfileStatus)
	}
	if proof.EvidenceClassification != EvidenceDirectObservation {
		return fmt.Errorf("%w: evidence classification '%s' is not direct observation", ErrInvalidProof, proof.EvidenceClassification)
	}
	if len(proof.RequestedResources) == 0 {
		return fmt.Errorf("%w: proof contains zero requested resources", ErrInvalidProof)
	}

	for i, claim := range proof.RequestedResources {
		if err := ValidateResourceClaim(claim); err != nil {
			return fmt.Errorf("%w: proof requested claim at index %d invalid: %v", ErrInvalidProof, i, err)
		}
	}

	// Verify proof requested resources canonically match request requested resources
	if formatCanonicalClaims(proof.RequestedResources) != formatCanonicalClaims(req.RequestedResources) {
		return fmt.Errorf("%w: proof requested resources do not match request requested resources", ErrInvalidProof)
	}

	// Verify snapshot allocations have NO DUPLICATES!
	activeMap := make(map[string]ActiveAllocation, len(snapshot.Allocations))
	for _, alloc := range snapshot.Allocations {
		if strings.TrimSpace(alloc.AllocationID) == "" {
			return fmt.Errorf("%w: empty allocation ID in snapshot", ErrInvalidProof)
		}
		if _, duplicate := activeMap[alloc.AllocationID]; duplicate {
			return fmt.Errorf("%w: duplicate allocation ID '%s' in snapshot allocations", ErrInvalidProof, alloc.AllocationID)
		}
		activeMap[alloc.AllocationID] = alloc
	}

	seenAllocIDs := make(map[string]struct{}, len(proof.AllocationMappings))
	for _, mapping := range proof.AllocationMappings {
		if strings.TrimSpace(mapping.AllocationID) == "" {
			return fmt.Errorf("%w: empty allocation ID in proof mapping", ErrInvalidProof)
		}
		if _, duplicate := seenAllocIDs[mapping.AllocationID]; duplicate {
			return fmt.Errorf("%w: duplicate allocation ID '%s' in proof mappings", ErrInvalidProof, mapping.AllocationID)
		}
		seenAllocIDs[mapping.AllocationID] = struct{}{}

		alloc, ok := activeMap[mapping.AllocationID]
		if !ok {
			return fmt.Errorf("%w: allocation '%s' in proof is not active in snapshot", ErrProofUnmappedAllocation, mapping.AllocationID)
		}
		if len(mapping.FreedResources) == 0 {
			return fmt.Errorf("%w: mapping for allocation '%s' contains zero freed resources", ErrInvalidProof, mapping.AllocationID)
		}

		// Strictly check target allocation claims ownership (NO physical tuner / demux bypass!)
		allocClaimMap := make(map[string]int)
		for _, claim := range alloc.Claims {
			if err := ValidateResourceClaim(claim); err != nil {
				return fmt.Errorf("%w: allocation '%s' claim invalid: %v", ErrInvalidProof, mapping.AllocationID, err)
			}
			key := fmt.Sprintf("%s:%s", claim.Kind, claim.Resource)
			allocClaimMap[key] += claim.Quantity
		}
		for _, freed := range mapping.FreedResources {
			if err := ValidateResourceClaim(freed); err != nil {
				return fmt.Errorf("%w: freed claim for '%s' invalid: %v", ErrInvalidProof, mapping.AllocationID, err)
			}
			key := fmt.Sprintf("%s:%s", freed.Kind, freed.Resource)
			ownedQty, hasClaim := allocClaimMap[key]
			if !hasClaim || ownedQty < freed.Quantity {
				return fmt.Errorf("%w: freed claim '%s' (qty %d) not owned by target allocation '%s' (owned %d)", ErrInvalidProof, key, freed.Quantity, mapping.AllocationID, ownedQty)
			}
		}
	}

	// Consolidate and cross-validate FreedResourcesByTarget (strict 1:1 bijection with AllocationMappings!)
	if proof.FreedResourcesByTarget != nil {
		if len(proof.FreedResourcesByTarget) != len(proof.AllocationMappings) {
			return fmt.Errorf("%w: FreedResourcesByTarget map length %d != AllocationMappings length %d", ErrInvalidProof, len(proof.FreedResourcesByTarget), len(proof.AllocationMappings))
		}
		for _, mapping := range proof.AllocationMappings {
			freedClaims, exists := proof.FreedResourcesByTarget[mapping.AllocationID]
			if !exists {
				return fmt.Errorf("%w: FreedResourcesByTarget missing target '%s'", ErrInvalidProof, mapping.AllocationID)
			}
			if formatCanonicalClaims(freedClaims) != formatCanonicalClaims(mapping.FreedResources) {
				return fmt.Errorf("%w: FreedResourcesByTarget for '%s' contradicts AllocationMappings", ErrInvalidProof, mapping.AllocationID)
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

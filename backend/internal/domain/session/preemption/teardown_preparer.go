// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package preemption

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	ErrInvalidPreparedTeardown      = errors.New("invalid prepared teardown")
	ErrPreparedTeardownExpired      = errors.New("prepared teardown has expired")
	ErrPreparedTeardownHashMismatch = errors.New("prepared teardown hash verification failed")
)

// TeardownPreparer provides pure, read-only teardown protocol preparation without side effects.
type TeardownPreparer struct {
	DefaultPreparationTTL time.Duration
}

// NewTeardownPreparer creates a new TeardownPreparer instance.
func NewTeardownPreparer() *TeardownPreparer {
	return &TeardownPreparer{
		DefaultPreparationTTL: 15 * time.Second,
	}
}

// PrepareTeardown re-evaluates the contract, live snapshot, and conflict proof, returning TeardownPreparationResult.
func (p *TeardownPreparer) PrepareTeardown(contract *PreemptionExecutionContract, snapshot ResourceSnapshot, proof ConflictResolutionProof, now time.Time) (TeardownPreparationResult, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}

	// 1. Validate contract integrity and expiration
	if err := ValidateContract(contract, now); err != nil {
		if errors.Is(err, ErrContractExpired) {
			return TeardownPreparationResult{
				Decision: DecisionRejected,
				Reason:   ReasonTeardownContractExpired,
			}, nil
		}
		return TeardownPreparationResult{
			Decision: DecisionRejected,
			Reason:   ReasonTeardownContractHashInvalid,
		}, nil
	}

	// Receiver ID alignment check
	if contract.ReceiverID != snapshot.ReceiverID || contract.ReceiverID != proof.ReceiverID {
		return TeardownPreparationResult{
			Decision: DecisionRejected,
			Reason:   ReasonTeardownInvalidProof,
		}, nil
	}

	// 2. Validate current snapshot & proof revisions against contract
	if contract.SnapshotRevision != snapshot.SnapshotRevision {
		return TeardownPreparationResult{
			Decision: DecisionRejected,
			Reason:   ReasonTeardownSnapshotRevisionMismatch,
		}, nil
	}
	if contract.HardwareProfileRevision != proof.HardwareProfileRevision {
		return TeardownPreparationResult{
			Decision: DecisionRejected,
			Reason:   ReasonTeardownHardwareRevisionMismatch,
		}, nil
	}
	if contract.ConflictProofRevision != proof.SnapshotRevision {
		return TeardownPreparationResult{
			Decision: DecisionRejected,
			Reason:   ReasonTeardownConflictProofRevisionMismatch,
		}, nil
	}

	// 3. Re-verify hardware proof status and evidence classification (DIRECT_OBSERVATION required!)
	if proof.HardwareProfileStatus != HardwareProfileValid {
		return TeardownPreparationResult{
			Decision: DecisionRejected,
			Reason:   ReasonTeardownStaleHardware,
		}, nil
	}
	if proof.EvidenceClassification != EvidenceDirectObservation {
		return TeardownPreparationResult{
			Decision: DecisionRejected,
			Reason:   ReasonTeardownInvalidProof,
		}, nil
	}

	// 4. Deep all-or-nothing validation of target active allocations in current snapshot
	activeMap := make(map[string]ActiveAllocation, len(snapshot.Allocations))
	for _, alloc := range snapshot.Allocations {
		activeMap[alloc.AllocationID] = alloc
	}

	freedByAllocation := make(map[string][]ResourceClaim)
	for _, mapping := range proof.AllocationMappings {
		freedByAllocation[mapping.AllocationID] = mapping.FreedResources
	}

	var combinedFreedClaims []ResourceClaim

	for _, targetID := range contract.TargetAllocationIDs {
		alloc, ok := activeMap[targetID]
		if !ok {
			// Target allocation is no longer active in snapshot!
			return TeardownPreparationResult{
				Decision: DecisionRejected,
				Reason:   ReasonTeardownTargetStateMutated,
			}, nil
		}

		// Target allocation must not have become Sacrosanct or UserProtected
		if alloc.Priority.Sacrosanct || alloc.Priority.UserProtected {
			return TeardownPreparationResult{
				Decision: DecisionRejected,
				Reason:   ReasonTeardownTargetProtected,
			}, nil
		}

		// Re-verify freed resources claimed for targetID exist and are owned by alloc
		freed := freedByAllocation[targetID]
		if len(freed) == 0 {
			return TeardownPreparationResult{
				Decision: DecisionRejected,
				Reason:   ReasonTeardownTargetStateMutated,
			}, nil
		}

		// Check alloc owns freed claims
		allocClaimMap := make(map[string]int)
		for _, claim := range alloc.Claims {
			key := fmt.Sprintf("%s:%s", claim.Kind, claim.Resource)
			allocClaimMap[key] += claim.Quantity
		}
		for _, fClaim := range freed {
			key := fmt.Sprintf("%s:%s", fClaim.Kind, fClaim.Resource)
			if allocClaimMap[key] < fClaim.Quantity {
				return TeardownPreparationResult{
					Decision: DecisionRejected,
					Reason:   ReasonTeardownTargetStateMutated,
				}, nil
			}
		}

		combinedFreedClaims = append(combinedFreedClaims, freed...)
	}

	// Verify combined freed claims strictly match contract ExpectedFreedResources
	if formatCanonicalClaims(combinedFreedClaims) != formatCanonicalClaims(contract.ExpectedFreedResources) {
		return TeardownPreparationResult{
			Decision: DecisionRejected,
			Reason:   ReasonTeardownTargetStateMutated,
		}, nil
	}

	// 5. Expiration time clamping: min(contract.ExpiresAt, now + TTL)
	prepTTL := p.DefaultPreparationTTL
	if prepTTL <= 0 {
		prepTTL = 15 * time.Second
	}
	maxExpires := now.Add(prepTTL)
	expiresAt := contract.ExpiresAt
	if maxExpires.Before(expiresAt) {
		expiresAt = maxExpires
	}

	sortedTargets := append([]string(nil), contract.TargetAllocationIDs...)
	sort.Strings(sortedTargets)

	teardownID := generateTeardownID(contract.ContractID, now)
	prepared := &PreparedTeardown{
		TeardownID:              teardownID,
		ContractID:              contract.ContractID,
		ReceiverID:              contract.ReceiverID,
		RequestID:               contract.RequestID,
		ContractHash:            contract.ContractHash,
		TargetAllocationIDs:     sortedTargets,
		SnapshotRevision:        snapshot.SnapshotRevision,
		HardwareProfileRevision: proof.HardwareProfileRevision,
		ConflictProofRevision:   proof.SnapshotRevision,
		PreparedAt:              now,
		ExpiresAt:               expiresAt,
	}

	hash, err := ComputePreparedTeardownHash(prepared)
	if err != nil {
		return TeardownPreparationResult{}, fmt.Errorf("failed to compute prepared teardown hash: %w", err)
	}
	prepared.PreparedTeardownHash = hash

	return TeardownPreparationResult{
		Decision: DecisionApproved,
		Reason:   ReasonTeardownApproved,
		Prepared: prepared,
	}, nil
}

// ValidatePreparedTeardown verifies structural integrity, canonical targets, TTL expiration, and hash match.
func ValidatePreparedTeardown(p *PreparedTeardown, now time.Time) error {
	if p == nil {
		return fmt.Errorf("%w: prepared teardown is nil", ErrInvalidPreparedTeardown)
	}
	if strings.TrimSpace(p.TeardownID) == "" {
		return fmt.Errorf("%w: empty teardown ID", ErrInvalidPreparedTeardown)
	}
	if strings.TrimSpace(p.ContractID) == "" {
		return fmt.Errorf("%w: empty contract ID", ErrInvalidPreparedTeardown)
	}
	if strings.TrimSpace(p.ReceiverID) == "" {
		return fmt.Errorf("%w: empty receiver ID", ErrInvalidPreparedTeardown)
	}
	if strings.TrimSpace(p.RequestID) == "" {
		return fmt.Errorf("%w: empty request ID", ErrInvalidPreparedTeardown)
	}
	if strings.TrimSpace(p.ContractHash) == "" {
		return fmt.Errorf("%w: empty contract hash", ErrInvalidPreparedTeardown)
	}
	if len(p.TargetAllocationIDs) == 0 {
		return fmt.Errorf("%w: empty target allocation IDs", ErrInvalidPreparedTeardown)
	}
	if !p.PreparedAt.Before(p.ExpiresAt) {
		return fmt.Errorf("%w: PreparedAt %s must be before ExpiresAt %s", ErrInvalidPreparedTeardown, p.PreparedAt, p.ExpiresAt)
	}

	// Check canonical sorting & uniqueness
	seen := make(map[string]struct{}, len(p.TargetAllocationIDs))
	for i, target := range p.TargetAllocationIDs {
		if strings.TrimSpace(target) == "" {
			return fmt.Errorf("%w: empty target allocation ID at index %d", ErrInvalidPreparedTeardown, i)
		}
		if _, exists := seen[target]; exists {
			return fmt.Errorf("%w: duplicate target allocation ID '%s'", ErrInvalidPreparedTeardown, target)
		}
		seen[target] = struct{}{}
		if i > 0 && p.TargetAllocationIDs[i-1] > p.TargetAllocationIDs[i] {
			return fmt.Errorf("%w: target allocation IDs are not canonically sorted", ErrInvalidPreparedTeardown)
		}
	}

	if !now.IsZero() && now.After(p.ExpiresAt) {
		return fmt.Errorf("%w: expired at %s (now: %s)", ErrPreparedTeardownExpired, p.ExpiresAt.Format(time.RFC3339), now.Format(time.RFC3339))
	}

	computedHash, err := ComputePreparedTeardownHash(p)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPreparedTeardown, err)
	}
	if p.PreparedTeardownHash != computedHash {
		return fmt.Errorf("%w: computed '%s' != stored '%s'", ErrPreparedTeardownHashMismatch, computedHash, p.PreparedTeardownHash)
	}

	return nil
}

// ComputePreparedTeardownHash generates SHA-256 hash covering all execution-relevant fields.
func ComputePreparedTeardownHash(p *PreparedTeardown) (string, error) {
	if p == nil {
		return "", ErrInvalidPreparedTeardown
	}

	sortedTargets := append([]string(nil), p.TargetAllocationIDs...)
	sort.Strings(sortedTargets)

	h := sha256.New()
	_, _ = fmt.Fprintf(h, "teardown_id=%s\n", p.TeardownID)
	_, _ = fmt.Fprintf(h, "contract_id=%s\n", p.ContractID)
	_, _ = fmt.Fprintf(h, "receiver_id=%s\n", p.ReceiverID)
	_, _ = fmt.Fprintf(h, "request_id=%s\n", p.RequestID)
	_, _ = fmt.Fprintf(h, "contract_hash=%s\n", p.ContractHash)
	_, _ = fmt.Fprintf(h, "targets=%s\n", strings.Join(sortedTargets, ","))
	_, _ = fmt.Fprintf(h, "snapshot_rev=%s\n", p.SnapshotRevision)
	_, _ = fmt.Fprintf(h, "hardware_rev=%s\n", p.HardwareProfileRevision)
	_, _ = fmt.Fprintf(h, "proof_rev=%s\n", p.ConflictProofRevision)
	_, _ = fmt.Fprintf(h, "prepared_at=%s\n", p.PreparedAt.UTC().Format(time.RFC3339Nano))
	_, _ = fmt.Fprintf(h, "expires_at=%s\n", p.ExpiresAt.UTC().Format(time.RFC3339Nano))

	return hex.EncodeToString(h.Sum(nil)), nil
}

func generateTeardownID(contractID string, t time.Time) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s:%d", contractID, t.UnixNano())
	return fmt.Sprintf("teardown-%s", hex.EncodeToString(h.Sum(nil))[:16])
}

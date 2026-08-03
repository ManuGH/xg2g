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
	ErrInvalidContract          = errors.New("invalid preemption execution contract")
	ErrContractExpired          = errors.New("preemption execution contract has expired")
	ErrContractHashMismatch     = errors.New("contract hash verification failed")
	ErrContractEmptyTargets     = errors.New("contract contains zero target allocation IDs")
	ErrContractDuplicateTargets = errors.New("contract contains duplicate target allocation IDs")
)

// ValidateResourceClaim verifies structural and semantic validity of a ResourceClaim.
func ValidateResourceClaim(claim ResourceClaim) error {
	switch claim.Kind {
	case ResourceKindRestrictedAccessSlot, ResourceKindTunerSlot, ResourceKindDemux, ResourceKindPhysicalTuner, ResourceKindStorageIO:
		// Valid kind
	default:
		return fmt.Errorf("invalid resource kind '%s'", claim.Kind)
	}

	if strings.TrimSpace(claim.Resource) == "" {
		return fmt.Errorf("empty resource identifier")
	}

	if claim.Quantity <= 0 {
		return fmt.Errorf("non-positive quantity %d", claim.Quantity)
	}

	return nil
}

// ValidateContract verifies structural integrity, target uniqueness, non-expired TTL, and SHA-256 hash match.
func ValidateContract(c *PreemptionExecutionContract, now time.Time) error {
	if c == nil {
		return fmt.Errorf("%w: contract is nil", ErrInvalidContract)
	}
	if strings.TrimSpace(c.ContractID) == "" {
		return fmt.Errorf("%w: empty contract ID", ErrInvalidContract)
	}
	if strings.TrimSpace(c.ReceiverID) == "" {
		return fmt.Errorf("%w: empty receiver ID", ErrInvalidContract)
	}
	if strings.TrimSpace(c.RequestID) == "" {
		return fmt.Errorf("%w: empty request ID", ErrInvalidContract)
	}
	if strings.TrimSpace(c.RequesterOwner) == "" {
		return fmt.Errorf("%w: empty requester owner", ErrInvalidContract)
	}
	if strings.TrimSpace(c.RequesterAllocationID) == "" {
		return fmt.Errorf("%w: empty requester allocation ID", ErrInvalidContract)
	}
	if strings.TrimSpace(c.RequesterRevision) == "" {
		return fmt.Errorf("%w: empty requester revision", ErrInvalidContract)
	}
	if strings.TrimSpace(c.SnapshotRevision) == "" {
		return fmt.Errorf("%w: empty snapshot revision", ErrInvalidContract)
	}
	if strings.TrimSpace(c.HardwareProfileRevision) == "" {
		return fmt.Errorf("%w: empty hardware profile revision", ErrInvalidContract)
	}
	if strings.TrimSpace(c.ConflictProofRevision) == "" {
		return fmt.Errorf("%w: empty conflict proof revision", ErrInvalidContract)
	}
	if len(c.TargetAllocationIDs) == 0 {
		return ErrContractEmptyTargets
	}
	if len(c.RequestedResources) == 0 {
		return fmt.Errorf("%w: zero requested resources", ErrInvalidContract)
	}
	if len(c.ExpectedFreedResources) == 0 {
		return fmt.Errorf("%w: zero expected freed resources", ErrInvalidContract)
	}

	for i, claim := range c.RequestedResources {
		if err := ValidateResourceClaim(claim); err != nil {
			return fmt.Errorf("%w: requested claim at index %d invalid: %v", ErrInvalidContract, i, err)
		}
	}
	for i, claim := range c.ExpectedFreedResources {
		if err := ValidateResourceClaim(claim); err != nil {
			return fmt.Errorf("%w: expected freed claim at index %d invalid: %v", ErrInvalidContract, i, err)
		}
	}

	// Verify ExpectedFreedResources actually satisfies RequestedResources!
	if !SatisfiesResources(c.RequestedResources, c.ExpectedFreedResources) {
		return fmt.Errorf("%w: expected freed resources do not satisfy requested resources", ErrInvalidContract)
	}

	if !c.CreatedAt.Before(c.ExpiresAt) {
		return fmt.Errorf("%w: CreatedAt %s must be before ExpiresAt %s", ErrInvalidContract, c.CreatedAt, c.ExpiresAt)
	}

	// Check canonical sorting & uniqueness of targets
	seen := make(map[string]struct{}, len(c.TargetAllocationIDs))
	for i, target := range c.TargetAllocationIDs {
		if strings.TrimSpace(target) == "" {
			return fmt.Errorf("%w: empty target allocation ID at index %d", ErrInvalidContract, i)
		}
		if _, exists := seen[target]; exists {
			return fmt.Errorf("%w: duplicate target allocation ID '%s'", ErrContractDuplicateTargets, target)
		}
		seen[target] = struct{}{}
		if i > 0 && c.TargetAllocationIDs[i-1] > c.TargetAllocationIDs[i] {
			return fmt.Errorf("%w: target allocation IDs are not canonically sorted", ErrInvalidContract)
		}
	}

	if !now.IsZero() && now.After(c.ExpiresAt) {
		return fmt.Errorf("%w: expired at %s (now: %s)", ErrContractExpired, c.ExpiresAt.Format(time.RFC3339), now.Format(time.RFC3339))
	}

	computedHash, err := ComputeContractHash(c)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidContract, err)
	}
	if c.ContractHash != computedHash {
		return fmt.Errorf("%w: computed '%s' != stored '%s'", ErrContractHashMismatch, computedHash, c.ContractHash)
	}

	return nil
}

// ComputeContractHash generates a deterministic SHA-256 hash covering all decision-relevant fields.
func ComputeContractHash(c *PreemptionExecutionContract) (string, error) {
	if c == nil {
		return "", ErrInvalidContract
	}

	sortedTargets := append([]string(nil), c.TargetAllocationIDs...)
	sort.Strings(sortedTargets)

	canonicalReqClaims := formatCanonicalClaims(c.RequestedResources)
	canonicalFreedClaims := formatCanonicalClaims(c.ExpectedFreedResources)

	h := sha256.New()
	_, _ = fmt.Fprintf(h, "contract_id=%s\n", c.ContractID)
	_, _ = fmt.Fprintf(h, "receiver_id=%s\n", c.ReceiverID)
	_, _ = fmt.Fprintf(h, "request_id=%s\n", c.RequestID)
	_, _ = fmt.Fprintf(h, "requester_owner=%s\n", c.RequesterOwner)
	_, _ = fmt.Fprintf(h, "requester_alloc_id=%s\n", c.RequesterAllocationID)
	_, _ = fmt.Fprintf(h, "requester_rev=%s\n", c.RequesterRevision)
	_, _ = fmt.Fprintf(h, "targets=%s\n", strings.Join(sortedTargets, ","))
	_, _ = fmt.Fprintf(h, "requested_claims=%s\n", canonicalReqClaims)
	_, _ = fmt.Fprintf(h, "expected_freed_claims=%s\n", canonicalFreedClaims)
	_, _ = fmt.Fprintf(h, "snapshot_rev=%s\n", c.SnapshotRevision)
	_, _ = fmt.Fprintf(h, "hardware_rev=%s\n", c.HardwareProfileRevision)
	_, _ = fmt.Fprintf(h, "proof_rev=%s\n", c.ConflictProofRevision)
	_, _ = fmt.Fprintf(h, "created_at=%s\n", c.CreatedAt.UTC().Format(time.RFC3339Nano))
	_, _ = fmt.Fprintf(h, "expires_at=%s\n", c.ExpiresAt.UTC().Format(time.RFC3339Nano))

	return hex.EncodeToString(h.Sum(nil)), nil
}

func formatCanonicalClaims(claims []ResourceClaim) string {
	formatted := make([]string, len(claims))
	for i, claim := range claims {
		formatted[i] = fmt.Sprintf("%s:%s:%d", claim.Kind, claim.Resource, claim.Quantity)
	}
	sort.Strings(formatted)
	return strings.Join(formatted, ",")
}

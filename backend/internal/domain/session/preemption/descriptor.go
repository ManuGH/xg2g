// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package preemption

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrInvalidDescriptor      = errors.New("invalid target execution descriptor")
	ErrDescriptorHashMismatch = errors.New("target execution descriptor hash mismatch")
	ErrClaimKindDisallowed    = errors.New("disallowed claim kind in hardware or non-hardware claim list")
	ErrClaimKindOverlap       = errors.New("claim resource exists in both expected claims and expected hardware claims")
)

// ComputeDescriptorHash computes a deterministic SHA-256 hash for a TargetExecutionDescriptor.
func ComputeDescriptorHash(d TargetExecutionDescriptor) (string, error) {
	if strings.TrimSpace(d.AllocationID) == "" {
		return "", fmt.Errorf("%w: empty allocation ID", ErrInvalidDescriptor)
	}
	if strings.TrimSpace(d.ExpectedOwner) == "" {
		return "", fmt.Errorf("%w: empty expected owner", ErrInvalidDescriptor)
	}
	if strings.TrimSpace(d.AllocationRevision) == "" {
		return "", fmt.Errorf("%w: empty allocation revision", ErrInvalidDescriptor)
	}
	if strings.TrimSpace(d.SnapshotRevision) == "" {
		return "", fmt.Errorf("%w: empty snapshot revision", ErrInvalidDescriptor)
	}
	if strings.TrimSpace(d.HardwareRevision) == "" {
		return "", fmt.Errorf("%w: empty hardware revision", ErrInvalidDescriptor)
	}

	// Verify ExpectedHardwareClaims contain ONLY physical tuner or demux claims
	hwClaimKeys := make(map[string]struct{}, len(d.ExpectedHardwareClaims))
	for i, claim := range d.ExpectedHardwareClaims {
		if claim.Kind != ResourceKindPhysicalTuner && claim.Kind != ResourceKindDemux {
			return "", fmt.Errorf("%w: hardware claim at index %d has invalid kind '%s'", ErrClaimKindDisallowed, i, claim.Kind)
		}
		key := fmt.Sprintf("%s:%s", claim.Kind, claim.Resource)
		hwClaimKeys[key] = struct{}{}
	}

	// Verify ExpectedClaims contain NO physical tuner or demux claims
	for i, claim := range d.ExpectedClaims {
		if claim.Kind == ResourceKindPhysicalTuner || claim.Kind == ResourceKindDemux {
			return "", fmt.Errorf("%w: expected claim at index %d has hardware kind '%s'", ErrClaimKindDisallowed, i, claim.Kind)
		}
		key := fmt.Sprintf("%s:%s", claim.Kind, claim.Resource)
		if _, overlap := hwClaimKeys[key]; overlap {
			return "", fmt.Errorf("%w: claim '%s' present in both expected claims and expected hardware claims", ErrClaimKindOverlap, key)
		}
	}

	canonClaims, err := formatCanonicalClaimsStrict(d.ExpectedClaims)
	if err != nil {
		return "", fmt.Errorf("%w: invalid expected claims: %v", ErrInvalidDescriptor, err)
	}
	canonHwClaims, err := formatCanonicalClaimsStrict(d.ExpectedHardwareClaims)
	if err != nil {
		return "", fmt.Errorf("%w: invalid expected hardware claims: %v", ErrInvalidDescriptor, err)
	}

	h := sha256.New()
	_, _ = fmt.Fprintf(h, "allocation_id=%s\n", d.AllocationID)
	_, _ = fmt.Fprintf(h, "expected_owner=%s\n", d.ExpectedOwner)
	_, _ = fmt.Fprintf(h, "alloc_rev=%s\n", d.AllocationRevision)
	_, _ = fmt.Fprintf(h, "snap_rev=%s\n", d.SnapshotRevision)
	_, _ = fmt.Fprintf(h, "hw_rev=%s\n", d.HardwareRevision)
	_, _ = fmt.Fprintf(h, "expected_claims=%s\n", canonClaims)
	_, _ = fmt.Fprintf(h, "expected_hw_claims=%s\n", canonHwClaims)

	return hex.EncodeToString(h.Sum(nil)), nil
}

// ValidateDescriptor verifies structural validity and SHA-256 hash match for a TargetExecutionDescriptor.
func ValidateDescriptor(d TargetExecutionDescriptor) error {
	computed, err := ComputeDescriptorHash(d)
	if err != nil {
		return err
	}
	if d.DescriptorHash != computed {
		return fmt.Errorf("%w: computed '%s' != stored '%s'", ErrDescriptorHashMismatch, computed, d.DescriptorHash)
	}
	return nil
}

// ComputeTargetDescriptorsHash generates a deterministic SHA-256 hash over an array of TargetExecutionDescriptors.
// Descriptors MUST be canonically sorted by AllocationID and each DescriptorHash must be valid.
func ComputeTargetDescriptorsHash(descriptors []TargetExecutionDescriptor) (string, error) {
	if len(descriptors) == 0 {
		return "", fmt.Errorf("%w: zero target descriptors", ErrInvalidDescriptor)
	}

	h := sha256.New()
	seen := make(map[string]struct{}, len(descriptors))
	for i, d := range descriptors {
		if err := ValidateDescriptor(d); err != nil {
			return "", fmt.Errorf("descriptor at index %d invalid: %w", i, err)
		}
		if _, duplicate := seen[d.AllocationID]; duplicate {
			return "", fmt.Errorf("%w: duplicate allocation ID '%s' in target descriptors", ErrInvalidDescriptor, d.AllocationID)
		}
		seen[d.AllocationID] = struct{}{}
		if i > 0 && descriptors[i-1].AllocationID > descriptors[i].AllocationID {
			return "", fmt.Errorf("%w: target descriptors are not canonically sorted by AllocationID at index %d", ErrInvalidDescriptor, i)
		}
		_, _ = fmt.Fprintf(h, "desc[%d]=%s:%s\n", i, d.AllocationID, d.DescriptorHash)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

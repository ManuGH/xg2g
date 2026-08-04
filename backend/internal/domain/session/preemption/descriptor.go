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

	canonHwBindings, err := formatCanonicalHardwareBindingsStrict(d.ExpectedHardwareBindings)
	if err != nil {
		return "", fmt.Errorf("%w: invalid expected hardware bindings: %v", ErrInvalidDescriptor, err)
	}
	canonLeaseBindings, err := formatCanonicalLeaseBindingsStrict(d.ExpectedLeaseBindings)
	if err != nil {
		return "", fmt.Errorf("%w: invalid expected lease bindings: %v", ErrInvalidDescriptor, err)
	}

	h := sha256.New()
	_, _ = fmt.Fprintf(h, "allocation_id=%s\n", d.AllocationID)
	_, _ = fmt.Fprintf(h, "expected_owner=%s\n", d.ExpectedOwner)
	_, _ = fmt.Fprintf(h, "alloc_rev=%s\n", d.AllocationRevision)
	_, _ = fmt.Fprintf(h, "snap_rev=%s\n", d.SnapshotRevision)
	_, _ = fmt.Fprintf(h, "hw_rev=%s\n", d.HardwareRevision)
	_, _ = fmt.Fprintf(h, "expected_claims=%s\n", canonClaims)
	_, _ = fmt.Fprintf(h, "expected_hw_claims=%s\n", canonHwClaims)
	_, _ = fmt.Fprintf(h, "expected_hw_bindings=%s\n", canonHwBindings)
	_, _ = fmt.Fprintf(h, "expected_lease_bindings=%s\n", canonLeaseBindings)

	return hex.EncodeToString(h.Sum(nil)), nil
}

func formatCanonicalHardwareBindingsStrict(bindings []ExpectedHardwareBinding) (string, error) {
	if len(bindings) == 0 {
		return "", nil
	}
	formatted := make([]string, 0, len(bindings))
	seen := make(map[string]struct{}, len(bindings))
	for i, b := range bindings {
		if !b.Kind.IsValid() {
			return "", fmt.Errorf("invalid resource kind '%s' at index %d", b.Kind, i)
		}
		if strings.TrimSpace(b.Resource) == "" {
			return "", fmt.Errorf("empty resource at index %d", i)
		}
		if strings.TrimSpace(b.AllocationID) == "" {
			return "", fmt.Errorf("empty allocation ID at index %d", i)
		}
		if b.Quantity <= 0 {
			return "", fmt.Errorf("non-positive quantity %d at index %d", b.Quantity, i)
		}
		item := fmt.Sprintf("%s:%s:%s:%d", b.Kind, strings.TrimSpace(b.Resource), strings.TrimSpace(b.AllocationID), b.Quantity)
		if _, dup := seen[item]; dup {
			return "", fmt.Errorf("duplicate hardware binding '%s' at index %d", item, i)
		}
		seen[item] = struct{}{}
		formatted = append(formatted, item)
	}
	sort.Strings(formatted)
	return strings.Join(formatted, ";"), nil
}

func formatCanonicalLeaseBindingsStrict(bindings []ExpectedLeaseBinding) (string, error) {
	if len(bindings) == 0 {
		return "", nil
	}
	formatted := make([]string, 0, len(bindings))
	seen := make(map[string]struct{}, len(bindings))
	for i, b := range bindings {
		if !b.LeaseKind.IsValid() {
			return "", fmt.Errorf("invalid lease kind '%s' at index %d", b.LeaseKind, i)
		}
		if strings.TrimSpace(b.Resource) == "" {
			return "", fmt.Errorf("empty resource at index %d", i)
		}
		if strings.TrimSpace(b.ScopeID) == "" {
			return "", fmt.Errorf("empty scope ID at index %d", i)
		}
		if strings.TrimSpace(b.ExpectedOwnerID) == "" {
			return "", fmt.Errorf("empty expected owner ID at index %d", i)
		}
		if b.Quantity <= 0 {
			return "", fmt.Errorf("non-positive quantity %d at index %d", b.Quantity, i)
		}
		item := fmt.Sprintf("%s:%s:%s:%s:%d", b.LeaseKind, strings.TrimSpace(b.Resource), strings.TrimSpace(b.ScopeID), strings.TrimSpace(b.ExpectedOwnerID), b.Quantity)
		if _, dup := seen[item]; dup {
			return "", fmt.Errorf("duplicate lease binding '%s' at index %d", item, i)
		}
		seen[item] = struct{}{}
		formatted = append(formatted, item)
	}
	sort.Strings(formatted)
	return strings.Join(formatted, ";"), nil
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

	seenIDs := make(map[string]struct{}, len(descriptors))
	var lastID string

	h := sha256.New()
	for i, d := range descriptors {
		if err := ValidateDescriptor(d); err != nil {
			return "", fmt.Errorf("descriptor at index %d invalid: %w", i, err)
		}

		if i > 0 && d.AllocationID <= lastID {
			return "", fmt.Errorf("%w: descriptors not strictly sorted by AllocationID (index %d '%s' <= index %d '%s')", ErrInvalidDescriptor, i, d.AllocationID, i-1, lastID)
		}
		if _, duplicate := seenIDs[d.AllocationID]; duplicate {
			return "", fmt.Errorf("%w: duplicate allocation ID '%s' in descriptors", ErrInvalidDescriptor, d.AllocationID)
		}
		seenIDs[d.AllocationID] = struct{}{}
		lastID = d.AllocationID

		_, _ = fmt.Fprintf(h, "desc[%d]=%s\n", i, d.DescriptorHash)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

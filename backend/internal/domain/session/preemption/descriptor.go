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
	ErrInvalidDescriptor = errors.New("invalid target execution descriptor")
)

// ComputeDescriptorHash generates a deterministic SHA-256 hash covering all execution-relevant fields.
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

// ValidateDescriptor validates the descriptor's fields and hash.
func ValidateDescriptor(d TargetExecutionDescriptor) error {
	computed, err := ComputeDescriptorHash(d)
	if err != nil {
		return err
	}
	if strings.TrimSpace(d.DescriptorHash) == "" {
		return fmt.Errorf("%w: empty descriptor hash", ErrInvalidDescriptor)
	}
	if d.DescriptorHash != computed {
		return fmt.Errorf("%w: computed descriptor hash '%s' != stored '%s'", ErrInvalidDescriptor, computed, d.DescriptorHash)
	}
	return nil
}

// ComputeTargetDescriptorsHash computes a canonical hash across a slice of target descriptors.
func ComputeTargetDescriptorsHash(descriptors []TargetExecutionDescriptor) (string, error) {
	if len(descriptors) == 0 {
		return "", fmt.Errorf("%w: empty descriptors list", ErrInvalidDescriptor)
	}

	sorted := make([]TargetExecutionDescriptor, len(descriptors))
	copy(sorted, descriptors)

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].AllocationID < sorted[j].AllocationID
	})

	h := sha256.New()
	seen := make(map[string]struct{}, len(sorted))
	for i, d := range sorted {
		if _, duplicate := seen[d.AllocationID]; duplicate {
			return "", fmt.Errorf("%w: duplicate allocation ID '%s' in descriptors", ErrInvalidDescriptor, d.AllocationID)
		}
		seen[d.AllocationID] = struct{}{}

		dHash, err := ComputeDescriptorHash(d)
		if err != nil {
			return "", fmt.Errorf("%w: descriptor at index %d invalid: %v", ErrInvalidDescriptor, i, err)
		}
		_, _ = fmt.Fprintf(h, "%d:%s:%s\n", i, d.AllocationID, dHash)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func formatCanonicalHardwareBindingsStrict(bindings []ExpectedHardwareBinding) (string, error) {
	if len(bindings) == 0 {
		return "", nil
	}
	qtyMap := make(map[string]int, len(bindings))
	for i, b := range bindings {
		if !b.Kind.IsValid() {
			return "", fmt.Errorf("invalid resource kind '%s' at index %d", b.Kind, i)
		}
		res := strings.TrimSpace(b.Resource)
		if res == "" {
			return "", fmt.Errorf("empty resource at index %d", i)
		}
		allocID := strings.TrimSpace(b.AllocationID)
		if allocID == "" {
			return "", fmt.Errorf("empty allocation ID at index %d", i)
		}
		if b.Quantity <= 0 {
			return "", fmt.Errorf("non-positive quantity %d at index %d", b.Quantity, i)
		}
		key := fmt.Sprintf("%s:%s:%s", b.Kind, res, allocID)
		if qtyMap[key]+b.Quantity < qtyMap[key] {
			return "", fmt.Errorf("quantity overflow at index %d", i)
		}
		qtyMap[key] += b.Quantity
	}

	keys := make([]string, 0, len(qtyMap))
	for k := range qtyMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	formatted := make([]string, 0, len(keys))
	for _, k := range keys {
		formatted = append(formatted, fmt.Sprintf("%s:%d", k, qtyMap[k]))
	}
	return strings.Join(formatted, ";"), nil
}

func formatCanonicalLeaseBindingsStrict(bindings []ExpectedLeaseBinding) (string, error) {
	if len(bindings) == 0 {
		return "", nil
	}
	qtyMap := make(map[string]int, len(bindings))
	for i, b := range bindings {
		if !b.LeaseKind.IsValid() {
			return "", fmt.Errorf("invalid lease kind '%s' at index %d", b.LeaseKind, i)
		}
		res := strings.TrimSpace(b.Resource)
		if res == "" {
			return "", fmt.Errorf("empty resource at index %d", i)
		}
		scopeID := strings.TrimSpace(b.ScopeID)
		if scopeID == "" {
			return "", fmt.Errorf("empty scope ID at index %d", i)
		}
		ownerID := strings.TrimSpace(b.ExpectedOwnerID)
		if ownerID == "" {
			return "", fmt.Errorf("empty expected owner ID at index %d", i)
		}
		if b.Quantity <= 0 {
			return "", fmt.Errorf("non-positive quantity %d at index %d", b.Quantity, i)
		}
		key := fmt.Sprintf("%s:%s:%s:%s", b.LeaseKind, res, scopeID, ownerID)
		if qtyMap[key]+b.Quantity < qtyMap[key] {
			return "", fmt.Errorf("quantity overflow at index %d", i)
		}
		qtyMap[key] += b.Quantity
	}

	keys := make([]string, 0, len(qtyMap))
	for k := range qtyMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	formatted := make([]string, 0, len(keys))
	for _, k := range keys {
		formatted = append(formatted, fmt.Sprintf("%s:%d", k, qtyMap[k]))
	}
	return strings.Join(formatted, ";"), nil
}

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
	if len(c.TargetAllocationIDs) == 0 {
		return ErrContractEmptyTargets
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

	h := sha256.New()
	_, _ = fmt.Fprintf(h, "contract_id=%s\n", c.ContractID)
	_, _ = fmt.Fprintf(h, "receiver_id=%s\n", c.ReceiverID)
	_, _ = fmt.Fprintf(h, "request_id=%s\n", c.RequestID)
	_, _ = fmt.Fprintf(h, "targets=%s\n", strings.Join(sortedTargets, ","))
	_, _ = fmt.Fprintf(h, "snapshot_rev=%s\n", c.SnapshotRevision)
	_, _ = fmt.Fprintf(h, "hardware_rev=%s\n", c.HardwareProfileRevision)
	_, _ = fmt.Fprintf(h, "proof_rev=%s\n", c.ConflictProofRevision)
	_, _ = fmt.Fprintf(h, "created_at=%s\n", c.CreatedAt.UTC().Format(time.RFC3339Nano))
	_, _ = fmt.Fprintf(h, "expires_at=%s\n", c.ExpiresAt.UTC().Format(time.RFC3339Nano))

	return hex.EncodeToString(h.Sum(nil)), nil
}

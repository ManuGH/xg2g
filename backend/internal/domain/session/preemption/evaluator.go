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

// Evaluator provides pure, deterministic preemption plan selection.
type Evaluator struct {
	DefaultContractTTL time.Duration
}

// NewEvaluator creates a new preemption evaluator with default configuration.
func NewEvaluator() *Evaluator {
	return &Evaluator{
		DefaultContractTTL: 30 * time.Second,
	}
}

// SelectPlan evaluates the preemption request, snapshot, and proof, returning SelectionResult.
// Domain rejections return a non-nil SelectionResult with error == nil.
// Technical errors (corrupted input, hash failure) return a Go error.
func (e *Evaluator) SelectPlan(req PreemptionRequest, snapshot ResourceSnapshot, proof ConflictResolutionProof, now time.Time) (SelectionResult, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}

	// 1. Validate Conflict Resolution Proof against Request & Snapshot
	if err := ValidateConflictProof(req, snapshot, proof); err != nil {
		if errorsIsProofMismatch(err) {
			return SelectionResult{
				Decision: DecisionRejected,
				Reason:   ReasonRejectedRevisionMismatch,
			}, nil
		}
		if errorsIsStaleHardware(err) {
			return SelectionResult{
				Decision: DecisionRejected,
				Reason:   ReasonRejectedStaleHardware,
			}, nil
		}
		return SelectionResult{
			Decision: DecisionRejected,
			Reason:   ReasonRejectedInvalidProof,
		}, nil
	}

	// 2. Identify active allocations and filter out sacrosanct / user-protected ones
	eligibleMap := make(map[string]ActiveAllocation)
	for _, alloc := range snapshot.Allocations {
		if alloc.Priority.Sacrosanct || alloc.Priority.UserProtected {
			continue
		}
		eligibleMap[alloc.AllocationID] = alloc
	}

	if len(eligibleMap) == 0 {
		return SelectionResult{
			Decision: DecisionRejected,
			Reason:   ReasonRejectedNoEligibleVictim,
		}, nil
	}
	if len(eligibleMap) > MaxCandidateAllocations {
		return SelectionResult{
			Decision: DecisionRejected,
			Reason:   ReasonRejectedBoundsExceeded,
		}, nil
	}

	// 3. Find candidate sets of targets that satisfy requested resources according to the hardware proof mappings
	// Build map of allocationID -> freed resources from proof
	freedByAllocation := make(map[string][]ResourceClaim)
	for _, mapping := range proof.AllocationMappings {
		freedByAllocation[mapping.AllocationID] = mapping.FreedResources
	}

	// Generate candidate combinations of eligible allocation IDs
	eligibleIDs := make([]string, 0, len(eligibleMap))
	for id := range eligibleMap {
		eligibleIDs = append(eligibleIDs, id)
	}
	sort.Strings(eligibleIDs)

	var validCandidateSets [][]string
	// Power set of eligibleIDs up to MaxVictimsPerPlan
	powerSet := generateSubsets(eligibleIDs, MaxVictimsPerPlan)
	for _, candidateSet := range powerSet {
		// Calculate total freed claims for this candidate set
		var combinedFreed []ResourceClaim
		for _, targetID := range candidateSet {
			combinedFreed = append(combinedFreed, freedByAllocation[targetID]...)
		}
		if SatisfiesResources(req.RequestedResources, combinedFreed) {
			validCandidateSets = append(validCandidateSets, candidateSet)
		}
	}

	if len(validCandidateSets) == 0 {
		return SelectionResult{
			Decision: DecisionRejected,
			Reason:   ReasonRejectedUnmappedHardware,
		}, nil
	}

	// 4. Rank valid candidate sets deterministically:
	// Criteria 1: Requester priority must strictly exceed candidate set's max priority
	// Criteria 2: Minimal victim count
	// Criteria 3: Lowest effective priority sum
	// Criteria 4: Minimal preemption cost sum
	// Criteria 5: Oldest session (earliest StartedAt)
	// Criteria 6: Lexicographical target list (stable tie-breaker)
	type CandidateRank struct {
		Set            []string
		MaxPriority    int
		SumPriority    int
		VictimCount    int
		SumCost        int
		EarliestStart  time.Time
		LexKey         string
		FreedResources []ResourceClaim
	}

	var ranked []CandidateRank
	for _, set := range validCandidateSets {
		maxPrio := 0
		sumPrio := 0
		sumCost := 0
		var earliestStart time.Time
		var combinedFreed []ResourceClaim

		for i, id := range set {
			alloc := eligibleMap[id]
			if alloc.Priority.BasePriority > maxPrio {
				maxPrio = alloc.Priority.BasePriority
			}
			sumPrio += alloc.Priority.BasePriority
			sumCost += alloc.Priority.PreemptionCost
			if i == 0 || alloc.Priority.StartedAt.Before(earliestStart) {
				earliestStart = alloc.Priority.StartedAt
			}
			combinedFreed = append(combinedFreed, freedByAllocation[id]...)
		}

		// Requester priority check
		if req.RequesterPriority.BasePriority <= maxPrio {
			continue // Requester priority does not strictly exceed candidate set max priority
		}

		sortedSet := append([]string(nil), set...)
		sort.Strings(sortedSet)

		ranked = append(ranked, CandidateRank{
			Set:            sortedSet,
			MaxPriority:    maxPrio,
			SumPriority:    sumPrio,
			VictimCount:    len(sortedSet),
			SumCost:        sumCost,
			EarliestStart:  earliestStart,
			LexKey:         strings.Join(sortedSet, ","),
			FreedResources: combinedFreed,
		})
	}

	if len(ranked) == 0 {
		return SelectionResult{
			Decision: DecisionRejected,
			Reason:   ReasonRejectedInsufficientPriority,
		}, nil
	}

	sort.Slice(ranked, func(i, j int) bool {
		r1, r2 := ranked[i], ranked[j]
		if r1.VictimCount != r2.VictimCount {
			return r1.VictimCount < r2.VictimCount // Prefer fewer victims
		}
		if r1.SumPriority != r2.SumPriority {
			return r1.SumPriority < r2.SumPriority // Prefer lower priority sum
		}
		if r1.SumCost != r2.SumCost {
			return r1.SumCost < r2.SumCost // Prefer lower preemption cost
		}
		if !r1.EarliestStart.Equal(r2.EarliestStart) {
			return r1.EarliestStart.Before(r2.EarliestStart) // Prefer oldest session
		}
		return r1.LexKey < r2.LexKey // Final tie-breaker
	})

	best := ranked[0]

	// 5. Construct immutable PreemptionExecutionContract
	contractID := generateContractID(req.RequestID, best.LexKey, now)
	ttl := e.DefaultContractTTL
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	expiresAt := now.Add(ttl)

	contract := &PreemptionExecutionContract{
		ContractID:              contractID,
		ReceiverID:              req.ReceiverID,
		RequestID:               req.RequestID,
		RequesterOwner:          req.RequesterOwner,
		RequesterAllocationID:   req.RequesterAllocationID,
		RequesterRevision:       req.RequesterRevision,
		TargetAllocationIDs:     best.Set,
		RequestedResources:      req.RequestedResources,
		ExpectedFreedResources:  best.FreedResources,
		SnapshotRevision:        snapshot.SnapshotRevision,
		HardwareProfileRevision: proof.HardwareProfileRevision,
		ConflictProofRevision:   proof.SnapshotRevision,
		CreatedAt:               now,
		ExpiresAt:               expiresAt,
	}

	hash, err := ComputeContractHash(contract)
	if err != nil {
		return SelectionResult{}, fmt.Errorf("failed to compute contract hash: %w", err)
	}
	contract.ContractHash = hash

	return SelectionResult{
		Decision: DecisionApproved,
		Reason:   ReasonApproved,
		Contract: contract,
	}, nil
}

func errorsIsProofMismatch(err error) bool {
	return errors.Is(err, ErrProofReceiverMismatch) || errors.Is(err, ErrProofRevisionMismatch)
}

func errorsIsStaleHardware(err error) bool {
	return errors.Is(err, ErrProofStaleHardware)
}

func generateSubsets(set []string, maxLen int) [][]string {
	var subsets [][]string
	n := len(set)
	limit := 1 << n
	for i := 1; i < limit; i++ {
		var subset []string
		for j := 0; j < n; j++ {
			if (i & (1 << j)) != 0 {
				subset = append(subset, set[j])
			}
		}
		if len(subset) <= maxLen {
			subsets = append(subsets, subset)
		}
	}
	return subsets
}

func generateContractID(requestID, lexKey string, t time.Time) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s:%s:%d", requestID, lexKey, t.UnixNano())
	return fmt.Sprintf("contract-%s", hex.EncodeToString(h.Sum(nil))[:16])
}

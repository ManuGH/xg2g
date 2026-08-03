package policy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	domainPolicy "github.com/ManuGH/xg2g/internal/domain/policy"
)

// ConsistencyTolerance defines the maximum allowable observation time gap between CandidateProvider and AllocationProvider.
const ConsistencyTolerance = 5 * time.Second

// SnapshotBuilder combines CandidateProvider and AllocationProvider into a typed domainPolicy.ResourceSnapshot.
type SnapshotBuilder interface {
	BuildSnapshot(ctx context.Context, req domainPolicy.EvaluationRequest) (domainPolicy.ResourceSnapshot, string, error)
}

// ProductionSnapshotBuilder implements SnapshotBuilder.
type ProductionSnapshotBuilder struct {
	Candidates  CandidateProvider
	Allocations AllocationProvider
}

// NewSnapshotBuilder creates a ProductionSnapshotBuilder.
func NewSnapshotBuilder(c CandidateProvider, a AllocationProvider) *ProductionSnapshotBuilder {
	return &ProductionSnapshotBuilder{
		Candidates:  c,
		Allocations: a,
	}
}

// BuildSnapshot reads candidates and active allocations from separate sources of truth,
// validates timestamp consistency, builds a domainPolicy.ResourceSnapshot, and calculates a canonical SnapshotRevision hash.
func (b *ProductionSnapshotBuilder) BuildSnapshot(ctx context.Context, req domainPolicy.EvaluationRequest) (domainPolicy.ResourceSnapshot, string, error) {
	if b.Candidates == nil {
		return domainPolicy.ResourceSnapshot{}, "", ErrCandidateProviderUnavailable
	}
	if b.Allocations == nil {
		return domainPolicy.ResourceSnapshot{}, "", ErrAllocationProviderUnavailable
	}

	cands, candObsAt, err := b.Candidates.Candidates(ctx, req)
	if err != nil {
		return domainPolicy.ResourceSnapshot{}, "", fmt.Errorf("%w: %v", ErrCandidateProviderUnavailable, err)
	}

	allocMeta, allocObsAt, err := b.Allocations.ActiveAllocations(ctx, req.ResourceKind)
	if err != nil {
		return domainPolicy.ResourceSnapshot{}, "", fmt.Errorf("%w: %v", ErrAllocationProviderUnavailable, err)
	}

	// 1. Observation Timestamp Consistency Check
	diff := candObsAt.Sub(allocObsAt)
	if diff < 0 {
		diff = -diff
	}
	if diff > ConsistencyTolerance {
		return domainPolicy.ResourceSnapshot{}, "", fmt.Errorf("%w: candidate time %s vs allocation time %s (gap: %v)", ErrInconsistentSnapshotState, candObsAt.Format(time.RFC3339), allocObsAt.Format(time.RFC3339), diff)
	}

	// 2. Canonical Deterministic Sorting of Candidates
	sortedCands := make([]domainPolicy.ResourceCandidate, len(cands))
	copy(sortedCands, cands)
	sort.Slice(sortedCands, func(i, j int) bool {
		return sortedCands[i].Scope < sortedCands[j].Scope
	})

	// 3. Translation and Validation of Allocations
	sortedActive := make([]domainPolicy.ResourceAllocation, 0, len(allocMeta))
	for _, meta := range allocMeta {
		if !meta.Consumer.IsValid() {
			return domainPolicy.ResourceSnapshot{}, "", fmt.Errorf("%w: allocation %s has invalid or missing consumer %q", ErrConsumerMetadataUnavailable, meta.AllocationID, meta.Consumer)
		}

		sortedActive = append(sortedActive, domainPolicy.ResourceAllocation{
			AllocationID: meta.AllocationID,
			Consumer:     meta.Consumer,
			Owner:        meta.Owner,
			Scope:        meta.Scope,
			AcquiredAt:   meta.AcquiredAt,
			IsSacrosanct: meta.Sacrosanct || meta.Consumer == domainPolicy.ConsumerScheduledRecording,
			IsReleasing:  meta.Releasing,
		})
	}

	// Canonical Deterministic Sorting of Active Allocations
	sort.Slice(sortedActive, func(i, j int) bool {
		return sortedActive[i].AllocationID < sortedActive[j].AllocationID
	})

	// 4. Construct Domain ResourceSnapshot
	snap := domainPolicy.ResourceSnapshot{
		Kind:       req.ResourceKind,
		Capacity:   len(sortedCands), // Discrete pool invariant: Capacity == len(Candidates)
		Candidates: sortedCands,
		Active:     sortedActive,
	}

	// 5. Calculate Canonical Deterministic SnapshotRevision Hash
	revHash := ComputeSnapshotRevision(snap)

	return snap, revHash, nil
}

// ComputeSnapshotRevision calculates a 100% deterministic SHA-256 fingerprint hash of a ResourceSnapshot.
// It uses sorted arrays and contains ZERO pointer addresses, map iteration non-determinism, or current time values.
func ComputeSnapshotRevision(snap domainPolicy.ResourceSnapshot) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "kind:%s|cap:%d|", snap.Kind, snap.Capacity)

	for _, c := range snap.Candidates {
		_, _ = fmt.Fprintf(h, "cand:%s:%v:%v|", c.Scope, c.Compatible, c.Available)
	}

	for _, a := range snap.Active {
		_, _ = fmt.Fprintf(h, "alloc:%s:%s:%s:%s:%d:%v:%v|", a.AllocationID, a.Consumer, a.Owner, a.Scope, a.AcquiredAt.UnixNano(), a.IsSacrosanct, a.IsReleasing)
	}

	return hex.EncodeToString(h.Sum(nil))[:16]
}

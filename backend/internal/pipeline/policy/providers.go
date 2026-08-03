package policy

import (
	"context"
	"fmt"
	"strings"
	"time"

	domainPolicy "github.com/ManuGH/xg2g/internal/domain/policy"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
)

// TunerCandidateProvider provides physical hardware tuner candidates.
type TunerCandidateProvider struct {
	TotalSlots  int
	Allocations AllocationProvider
}

// NewTunerCandidateProvider creates a TunerCandidateProvider for N total hardware slots.
func NewTunerCandidateProvider(totalSlots int, allocs AllocationProvider) *TunerCandidateProvider {
	return &TunerCandidateProvider{
		TotalSlots:  totalSlots,
		Allocations: allocs,
	}
}

// Candidates returns the list of physical tuner candidates (e.g. tuner:0, tuner:1) and observation timestamp.
func (p *TunerCandidateProvider) Candidates(ctx context.Context, req domainPolicy.EvaluationRequest) ([]domainPolicy.ResourceCandidate, time.Time, error) {
	obsAt := time.Now()
	if p.TotalSlots <= 0 {
		return nil, obsAt, fmt.Errorf("%w: TotalSlots must be positive", ErrCandidateProviderUnavailable)
	}

	activeAllocs, allocObsAt, err := p.Allocations.ActiveAllocations(ctx, req.ResourceKind)
	if err != nil {
		return nil, obsAt, fmt.Errorf("%w: failed to fetch active allocations for candidate availability: %v", ErrCandidateProviderUnavailable, err)
	}

	// Use allocation observation time as candidate observation time to preserve timestamp consistency
	obsAt = allocObsAt

	occupiedScopes := make(map[string]bool, len(activeAllocs))
	for _, a := range activeAllocs {
		occupiedScopes[a.Scope] = true
	}

	cands := make([]domainPolicy.ResourceCandidate, 0, p.TotalSlots)
	for i := 0; i < p.TotalSlots; i++ {
		scope := fmt.Sprintf("tuner:%d", i)
		cands = append(cands, domainPolicy.ResourceCandidate{
			Scope:      scope,
			Compatible: true,
			Available:  !occupiedScopes[scope],
		})
	}

	return cands, obsAt, nil
}

// SessionStoreAllocationProvider resolves active tuner lease allocations with explicit consumer metadata from store.LeaseStore.
type SessionStoreAllocationProvider struct {
	LeaseStore  store.LeaseStore
	MetadataMap map[string]AllocationMetadata // Explicit metadata registry indexed by lease key/owner
}

// NewSessionStoreAllocationProvider creates a SessionStoreAllocationProvider.
func NewSessionStoreAllocationProvider(ls store.LeaseStore, meta map[string]AllocationMetadata) *SessionStoreAllocationProvider {
	if meta == nil {
		meta = make(map[string]AllocationMetadata)
	}
	return &SessionStoreAllocationProvider{
		LeaseStore:  ls,
		MetadataMap: meta,
	}
}

// ActiveAllocations lists unexpired active tuner allocations and returns their explicit AllocationMetadata and observation timestamp.
func (p *SessionStoreAllocationProvider) ActiveAllocations(ctx context.Context, kind domainPolicy.ResourceKind) ([]AllocationMetadata, time.Time, error) {
	obsAt := time.Now()
	if p.LeaseStore == nil {
		return nil, obsAt, fmt.Errorf("%w: LeaseStore is nil", ErrAllocationProviderUnavailable)
	}

	leases, err := p.LeaseStore.ListLeases(ctx)
	if err != nil {
		return nil, obsAt, fmt.Errorf("%w: failed to list leases: %v", ErrAllocationProviderUnavailable, err)
	}

	var res []AllocationMetadata
	for _, l := range leases {
		key := l.Key()
		// Filter tuner leases only (e.g., tuner:0, tuner:1)
		if !strings.HasPrefix(key, "tuner:") {
			continue
		}
		if obsAt.After(l.ExpiresAt()) {
			continue
		}

		// Look up explicit metadata by key
		meta, exists := p.MetadataMap[key]
		if !exists {
			// Try lookup by owner
			meta, exists = p.MetadataMap[l.Owner()]
		}

		if !exists || !meta.Consumer.IsValid() {
			return nil, obsAt, fmt.Errorf("%w: explicit consumer metadata missing or invalid for active tuner lease %s (owner %s)", ErrConsumerMetadataUnavailable, key, l.Owner())
		}

		// Ensure key and owner match authoritative lease
		meta.AllocationID = key
		meta.Scope = key
		meta.Owner = l.Owner()

		res = append(res, meta)
	}

	return res, obsAt, nil
}

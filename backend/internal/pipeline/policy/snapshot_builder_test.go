package policy

import (
	"context"
	"errors"
	"testing"
	"time"

	domainPolicy "github.com/ManuGH/xg2g/internal/domain/policy"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
)

type mockCandidateProvider struct {
	cands []domainPolicy.ResourceCandidate
	obsAt time.Time
	err   error
}

func (m *mockCandidateProvider) Candidates(ctx context.Context, req domainPolicy.EvaluationRequest) ([]domainPolicy.ResourceCandidate, time.Time, error) {
	return m.cands, m.obsAt, m.err
}

type mockAllocationProvider struct {
	allocs []AllocationMetadata
	obsAt  time.Time
	err    error
}

func (m *mockAllocationProvider) ActiveAllocations(ctx context.Context, kind domainPolicy.ResourceKind) ([]AllocationMetadata, time.Time, error) {
	return m.allocs, m.obsAt, m.err
}

func TestSnapshotBuilder_BuildSnapshot(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 2, 22, 0, 0, 0, time.UTC)

	req := domainPolicy.EvaluationRequest{
		Consumer:     domainPolicy.ConsumerLiveTV,
		ResourceKind: domainPolicy.ResourceTuner,
		Owner:        "user-1",
		ScopeMode:    domainPolicy.ScopeSelectionAnyCompatible,
		EvaluatedAt:  now,
	}

	cands := []domainPolicy.ResourceCandidate{
		{Scope: "tuner:0", Compatible: true, Available: false},
		{Scope: "tuner:1", Compatible: true, Available: true},
	}

	allocs := []AllocationMetadata{
		{
			AllocationID: "tuner:0",
			Consumer:     domainPolicy.ConsumerScheduledRecording,
			Owner:        "timer-job-1",
			Scope:        "tuner:0",
			AcquiredAt:   now.Add(-10 * time.Minute),
			Sacrosanct:   true,
			Releasing:    false,
		},
	}

	t.Run("ValidSnapshot_BuildsSuccessfullyWithHash", func(t *testing.T) {
		candProv := &mockCandidateProvider{cands: cands, obsAt: now}
		allocProv := &mockAllocationProvider{allocs: allocs, obsAt: now}

		builder := NewSnapshotBuilder(candProv, allocProv)
		snap, rev, err := builder.BuildSnapshot(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if snap.Capacity != 2 {
			t.Errorf("expected Capacity 2, got %d", snap.Capacity)
		}
		if len(snap.Active) != 1 {
			t.Errorf("expected 1 active allocation, got %d", len(snap.Active))
		}
		if rev == "" {
			t.Error("expected non-empty SnapshotRevision hash")
		}
	})

	t.Run("MissingConsumerMetadata_Fails", func(t *testing.T) {
		invalidAllocs := []AllocationMetadata{
			{
				AllocationID: "tuner:0",
				Consumer:     "", // Missing consumer metadata!
				Owner:        "user-2",
				Scope:        "tuner:0",
				AcquiredAt:   now.Add(-5 * time.Minute),
			},
		}
		candProv := &mockCandidateProvider{cands: cands, obsAt: now}
		allocProv := &mockAllocationProvider{allocs: invalidAllocs, obsAt: now}

		builder := NewSnapshotBuilder(candProv, allocProv)
		_, _, err := builder.BuildSnapshot(ctx, req)
		if !errors.Is(err, ErrConsumerMetadataUnavailable) {
			t.Fatalf("expected ErrConsumerMetadataUnavailable, got %v", err)
		}
	})

	t.Run("InconsistentTimestamps_Fails", func(t *testing.T) {
		candProv := &mockCandidateProvider{cands: cands, obsAt: now}
		allocProv := &mockAllocationProvider{allocs: allocs, obsAt: now.Add(10 * time.Second)} // Gap > 5s

		builder := NewSnapshotBuilder(candProv, allocProv)
		_, _, err := builder.BuildSnapshot(ctx, req)
		if !errors.Is(err, ErrInconsistentSnapshotState) {
			t.Fatalf("expected ErrInconsistentSnapshotState, got %v", err)
		}
	})
}

func TestSnapshotRevision_CanonicalDeterminism(t *testing.T) {
	now := time.Date(2026, 8, 2, 22, 0, 0, 0, time.UTC)
	snap1 := domainPolicy.ResourceSnapshot{
		Kind:     domainPolicy.ResourceTuner,
		Capacity: 2,
		Candidates: []domainPolicy.ResourceCandidate{
			{Scope: "tuner:0", Compatible: true, Available: false},
			{Scope: "tuner:1", Compatible: true, Available: true},
		},
		Active: []domainPolicy.ResourceAllocation{
			{AllocationID: "tuner:0", Consumer: domainPolicy.ConsumerLiveTV, Owner: "user", Scope: "tuner:0", AcquiredAt: now},
		},
	}
	snap2 := domainPolicy.ResourceSnapshot{
		Kind:     domainPolicy.ResourceTuner,
		Capacity: 2,
		Candidates: []domainPolicy.ResourceCandidate{
			{Scope: "tuner:0", Compatible: true, Available: false},
			{Scope: "tuner:1", Compatible: true, Available: true},
		},
		Active: []domainPolicy.ResourceAllocation{
			{AllocationID: "tuner:0", Consumer: domainPolicy.ConsumerLiveTV, Owner: "user", Scope: "tuner:0", AcquiredAt: now},
		},
	}

	rev1 := ComputeSnapshotRevision(snap1)
	rev2 := ComputeSnapshotRevision(snap2)

	if rev1 != rev2 {
		t.Errorf("deterministic revision hash failed: %s != %s", rev1, rev2)
	}
}

func TestSessionStoreAllocationProvider(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()

	// Acquire a lease in memory store
	_, ok, err := st.TryAcquireLease(ctx, "tuner:0", "owner-live-1", 10*time.Second)
	if err != nil || !ok {
		t.Fatalf("failed to setup lease in store: %v", err)
	}

	t.Run("MissingConsumerMetadata_ReturnsErrConsumerMetadataUnavailable", func(t *testing.T) {
		prov := NewSessionStoreAllocationProvider(st, nil) // Empty metadata map!
		_, _, err := prov.ActiveAllocations(ctx, domainPolicy.ResourceTuner)
		if !errors.Is(err, ErrConsumerMetadataUnavailable) {
			t.Fatalf("expected ErrConsumerMetadataUnavailable when metadata missing, got %v", err)
		}
	})

	t.Run("ExplicitConsumerMetadata_ReturnsActiveAllocations", func(t *testing.T) {
		meta := map[string]AllocationMetadata{
			"tuner:0": {
				AllocationID: "tuner:0",
				Consumer:     domainPolicy.ConsumerLiveTV,
				Owner:        "owner-live-1",
				Scope:        "tuner:0",
				AcquiredAt:   time.Now(),
			},
		}
		prov := NewSessionStoreAllocationProvider(st, meta)
		allocs, _, err := prov.ActiveAllocations(ctx, domainPolicy.ResourceTuner)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(allocs) != 1 {
			t.Fatalf("expected 1 allocation, got %d", len(allocs))
		}
		if allocs[0].Consumer != domainPolicy.ConsumerLiveTV {
			t.Errorf("expected ConsumerLiveTV, got %s", allocs[0].Consumer)
		}
	})
}

// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lease

import (
	"context"
	"errors"
	"testing"
	"time"
)

type recoveryBackend struct {
	leases []Lease
	err    error
}

func (b *recoveryBackend) Acquire(context.Context, Owner, Scope, time.Duration) (*Lease, error) {
	return nil, errors.New("not used")
}
func (b *recoveryBackend) Renew(context.Context, ID, Owner, time.Duration) (*Lease, error) {
	return nil, errors.New("not used")
}
func (b *recoveryBackend) Release(context.Context, ID, Owner, ReasonCode) (*Lease, error) {
	return nil, errors.New("not used")
}
func (b *recoveryBackend) ListLeases(context.Context) ([]Lease, error) {
	if b.err != nil {
		return nil, b.err
	}
	return b.leases, nil
}

// One epoch for the whole file. Mixing a fixed recovery clock with
// time.Now()-derived fixture timestamps made these tests depend on the hour
// they ran in: on CI, CreatedAt landed after the fixed recovery time and the
// store correctly refused UpdatedAt < CreatedAt.
var (
	recoveryEpoch = time.Date(2026, 9, 9, 7, 0, 0, 0, time.UTC)
	fixtureBirth  = recoveryEpoch.Add(-time.Hour)
)

func fixedNow(t *testing.T) func() time.Time {
	t.Helper()
	return func() time.Time { return recoveryEpoch }
}

// staleIntentStore builds a store holding one ACTIVE intent whose backend lease
// is gone -- the exact shape that refuses startup.
func staleIntentStore(t *testing.T) (*InMemoryIntentStore, LeaseIntent) {
	t.Helper()
	store := NewInMemoryIntentStore()
	created := fixtureBirth
	// The store enforces CAS: a new intent starts at revision 1 and every
	// update increments by exactly one. Seed through that path rather than
	// around it, so the fixture is a state the product can actually reach.
	intent := LeaseIntent{
		IntentID:  "intent-005836f9e7574d80",
		LeaseID:   "tuner:0",
		Owner:     "2885f2b8-1f62-4fec-9638-f410b4366111",
		Scope:     "tuner:0",
		State:     IntentStatePending,
		Revision:  1,
		CreatedAt: created,
		UpdatedAt: created,
	}
	intent.LeaseID = ""
	if err := store.SaveIntent(context.Background(), intent); err != nil {
		t.Fatalf("seed pending intent: %v", err)
	}
	intent.LeaseID = "tuner:0"
	intent.State = IntentStateActive
	intent.Revision = 2
	if err := store.SaveIntent(context.Background(), intent); err != nil {
		t.Fatalf("seed active intent: %v", err)
	}
	return store, intent
}

func TestRecoverMissingIntent_TransitionsActiveToTerminal(t *testing.T) {
	ctx := context.Background()
	store, intent := staleIntentStore(t)

	res, err := RecoverMissingIntent(ctx, store, &recoveryBackend{}, RecoverMissingIntentRequest{
		IntentID:         intent.IntentID,
		ExpectedRevision: intent.Revision,
		Confirmed:        true,
		Reason:           "ENOSPC crash left the intent behind",
	}, fixedNow(t))
	if err != nil {
		t.Fatalf("recovery failed: %v", err)
	}

	if res.FromState != IntentStateActive || res.ToState != IntentStateTerminal {
		t.Fatalf("unexpected transition %s -> %s", res.FromState, res.ToState)
	}
	if res.ToRevision != intent.Revision+1 {
		t.Fatalf("revision %d, want %d", res.ToRevision, intent.Revision+1)
	}
	if res.Reason == "" {
		t.Fatal("audit result must carry the operator's reason")
	}

	// Identity is preserved; only state, revision and timestamp move.
	got, err := store.GetIntent(ctx, intent.IntentID)
	if err != nil || got == nil {
		t.Fatalf("read back: %v", err)
	}
	if got.State != IntentStateTerminal {
		t.Fatalf("persisted state %s, want TERMINAL", got.State)
	}
	if got.LeaseID != intent.LeaseID || got.Owner != intent.Owner ||
		got.Scope != intent.Scope || !got.CreatedAt.Equal(intent.CreatedAt) {
		t.Fatal("recovery must preserve intent identity and creation time")
	}
}

func TestRecoverMissingIntent_Refusals(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		name    string
		setup   func(t *testing.T) (*InMemoryIntentStore, ObservableLeaseBackend, RecoverMissingIntentRequest)
		wantErr error
	}{
		{
			name: "backend lease still exists",
			setup: func(t *testing.T) (*InMemoryIntentStore, ObservableLeaseBackend, RecoverMissingIntentRequest) {
				store, intent := staleIntentStore(t)
				backend := &recoveryBackend{leases: []Lease{{
					ID: intent.LeaseID, Owner: intent.Owner, Scope: intent.Scope,
					State:      StateAcquired,
					AcquiredAt: recoveryEpoch.Add(-time.Minute),
					ExpiresAt:  recoveryEpoch.Add(time.Hour),
				}}}
				return store, backend, RecoverMissingIntentRequest{
					IntentID: intent.IntentID, ExpectedRevision: intent.Revision, Confirmed: true,
				}
			},
			wantErr: ErrRecoveryBackendLeasePresent,
		},
		{
			name: "backend state cannot be verified",
			setup: func(t *testing.T) (*InMemoryIntentStore, ObservableLeaseBackend, RecoverMissingIntentRequest) {
				store, intent := staleIntentStore(t)
				return store, &recoveryBackend{err: errors.New("receiver unreachable")},
					RecoverMissingIntentRequest{
						IntentID: intent.IntentID, ExpectedRevision: intent.Revision, Confirmed: true,
					}
			},
			wantErr: ErrRecoveryBackendUnverifiable,
		},
		{
			name: "intent is not ACTIVE",
			setup: func(t *testing.T) (*InMemoryIntentStore, ObservableLeaseBackend, RecoverMissingIntentRequest) {
				store, intent := staleIntentStore(t)
				intent.State = IntentStateTerminal
				intent.Revision = 3
				if err := store.SaveIntent(ctx, intent); err != nil {
					t.Fatalf("seed: %v", err)
				}
				return store, &recoveryBackend{}, RecoverMissingIntentRequest{
					IntentID: intent.IntentID, ExpectedRevision: intent.Revision, Confirmed: true,
				}
			},
			wantErr: ErrRecoveryIntentNotActive,
		},
		{
			name: "revision changed since the operator read it",
			setup: func(t *testing.T) (*InMemoryIntentStore, ObservableLeaseBackend, RecoverMissingIntentRequest) {
				store, intent := staleIntentStore(t)
				return store, &recoveryBackend{}, RecoverMissingIntentRequest{
					IntentID: intent.IntentID, ExpectedRevision: intent.Revision - 1, Confirmed: true,
				}
			},
			wantErr: ErrRecoveryRevisionMismatch,
		},
		{
			name: "another ACTIVE intent shares the scope",
			setup: func(t *testing.T) (*InMemoryIntentStore, ObservableLeaseBackend, RecoverMissingIntentRequest) {
				store, intent := staleIntentStore(t)
				rival := intent
				rival.IntentID = "intent-rival"
				rival.Owner = "some-other-session"
				rival.Revision = 1
				if err := store.SaveIntent(ctx, rival); err != nil {
					t.Fatalf("seed rival: %v", err)
				}
				return store, &recoveryBackend{}, RecoverMissingIntentRequest{
					IntentID: intent.IntentID, ExpectedRevision: intent.Revision, Confirmed: true,
				}
			},
			wantErr: ErrRecoveryTargetAmbiguous,
		},
		{
			name: "no explicit operator confirmation",
			setup: func(t *testing.T) (*InMemoryIntentStore, ObservableLeaseBackend, RecoverMissingIntentRequest) {
				store, intent := staleIntentStore(t)
				return store, &recoveryBackend{}, RecoverMissingIntentRequest{
					IntentID: intent.IntentID, ExpectedRevision: intent.Revision,
				}
			},
			wantErr: ErrRecoveryNotConfirmed,
		},
		{
			name: "no target named",
			setup: func(t *testing.T) (*InMemoryIntentStore, ObservableLeaseBackend, RecoverMissingIntentRequest) {
				store, _ := staleIntentStore(t)
				return store, &recoveryBackend{}, RecoverMissingIntentRequest{Confirmed: true}
			},
			wantErr: ErrRecoveryTargetAmbiguous,
		},
		{
			name: "target does not exist",
			setup: func(t *testing.T) (*InMemoryIntentStore, ObservableLeaseBackend, RecoverMissingIntentRequest) {
				store, _ := staleIntentStore(t)
				return store, &recoveryBackend{}, RecoverMissingIntentRequest{
					IntentID: "intent-nonexistent", Confirmed: true,
				}
			},
			wantErr: ErrRecoveryIntentNotFound,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, backend, req := tc.setup(t)

			before, _ := store.ListIntents(ctx)

			_, err := RecoverMissingIntent(ctx, store, backend, req, fixedNow(t))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}

			// A refusal must leave the store exactly as it found it.
			after, _ := store.ListIntents(ctx)
			if len(before) != len(after) {
				t.Fatalf("refusal changed intent count: %d -> %d", len(before), len(after))
			}
			for _, b := range before {
				got, err := store.GetIntent(ctx, b.IntentID)
				if err != nil || got == nil {
					t.Fatalf("intent %s vanished on refusal", b.IntentID)
				}
				if got.State != b.State || got.Revision != b.Revision {
					t.Fatalf("refusal mutated %s: %s/%d -> %s/%d",
						b.IntentID, b.State, b.Revision, got.State, got.Revision)
				}
			}
		})
	}
}

// An expired backend lease is not a held lease: it must not block recovery.
func TestRecoverMissingIntent_ExpiredBackendLeaseDoesNotBlock(t *testing.T) {
	ctx := context.Background()
	store, intent := staleIntentStore(t)
	backend := &recoveryBackend{leases: []Lease{{
		ID: intent.LeaseID, Owner: intent.Owner, Scope: intent.Scope,
		State:      StateAcquired,
		AcquiredAt: recoveryEpoch.Add(-2 * time.Hour),
		ExpiresAt:  recoveryEpoch.Add(-time.Hour),
	}}}

	res, err := RecoverMissingIntent(ctx, store, backend, RecoverMissingIntentRequest{
		IntentID: intent.IntentID, ExpectedRevision: intent.Revision, Confirmed: true,
	}, fixedNow(t))
	if err != nil {
		t.Fatalf("expired lease must not block recovery: %v", err)
	}
	if res.BackendLeasesObserved != 1 {
		t.Fatalf("audit must record what was observed, got %d", res.BackendLeasesObserved)
	}
}

// The startup policy itself must keep refusing. Recovery resolves the state;
// it never relaxes the gate.
func TestStartupPolicyStillRefusesMissingLeases(t *testing.T) {
	report := ReconciliationReport{}
	report.Summary.Missing = 1

	got := (DefaultStartupPolicy{}).Evaluate(report)
	if got.Decision != StartupRefused {
		t.Fatalf("startup decision = %s, want REFUSED", got.Decision)
	}

	report.Summary.Missing = 0
	if got := (DefaultStartupPolicy{}).Evaluate(report); got.Decision != StartupAccepted {
		t.Fatalf("after recovery decision = %s, want ACCEPTED", got.Decision)
	}
}

// seedSibling puts a second intent on a scope, reached through real
// transitions so the fixture is a state the product can actually produce.
func seedSibling(t *testing.T, store *InMemoryIntentStore, id string, scope Scope, state IntentState) {
	t.Helper()
	ctx := context.Background()

	in := LeaseIntent{
		IntentID:  ID(id),
		Owner:     Owner("owner-" + id),
		Scope:     scope,
		State:     IntentStatePending,
		Revision:  1,
		CreatedAt: fixtureBirth,
		UpdatedAt: fixtureBirth,
	}
	if err := store.SaveIntent(ctx, in); err != nil {
		t.Fatalf("seed sibling %s as PENDING: %v", id, err)
	}
	if state == IntentStatePending {
		return
	}

	in.LeaseID = ID(string(scope))
	in.State = IntentStateActive
	in.Revision = 2
	if err := store.SaveIntent(ctx, in); err != nil {
		t.Fatalf("seed sibling %s as ACTIVE: %v", id, err)
	}
	if state == IntentStateActive {
		return
	}

	in.State = state
	in.Revision = 3
	if err := store.SaveIntent(ctx, in); err != nil {
		t.Fatalf("seed sibling %s as %s: %v", id, state, err)
	}
}

// Running the recovery asserts that the target is the sole stale claim on its
// scope. Any other live claim there makes that assertion untrue -- not only
// another ACTIVE one. A TERMINAL sibling is history and claims nothing.
func TestRecoverMissingIntent_SiblingClaimsOnTheSameScope(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		siblingState IntentState
		wantRefusal  bool
	}{
		{IntentStatePending, true},
		{IntentStateActive, true},
		{IntentStateReleasing, true},
		{IntentStateRecoveryRequired, true},
		{IntentStateTerminal, false},
	} {
		t.Run(string(tc.siblingState), func(t *testing.T) {
			store, intent := staleIntentStore(t)
			seedSibling(t, store, "intent-sibling", intent.Scope, tc.siblingState)

			before, _ := store.GetIntent(ctx, "intent-sibling")

			_, err := RecoverMissingIntent(ctx, store, &recoveryBackend{}, RecoverMissingIntentRequest{
				IntentID:         intent.IntentID,
				ExpectedRevision: intent.Revision,
				Confirmed:        true,
			}, fixedNow(t))

			if tc.wantRefusal {
				if !errors.Is(err, ErrRecoveryTargetAmbiguous) {
					t.Fatalf("sibling in %s: error = %v, want ambiguous refusal", tc.siblingState, err)
				}
			} else if err != nil {
				t.Fatalf("sibling in %s must not block recovery: %v", tc.siblingState, err)
			}

			// The sibling is never touched, refused or not.
			after, gErr := store.GetIntent(ctx, "intent-sibling")
			if gErr != nil || after == nil {
				t.Fatalf("sibling vanished: %v", gErr)
			}
			if after.State != before.State || after.Revision != before.Revision {
				t.Fatalf("sibling was mutated: %s/%d -> %s/%d",
					before.State, before.Revision, after.State, after.Revision)
			}
		})
	}
}

// A live claim on a different scope is none of this scope's business.
func TestRecoverMissingIntent_SiblingOnAnotherScopeDoesNotBlock(t *testing.T) {
	ctx := context.Background()
	store, intent := staleIntentStore(t)
	seedSibling(t, store, "intent-other-scope", "tuner:1", IntentStateActive)

	if _, err := RecoverMissingIntent(ctx, store, &recoveryBackend{}, RecoverMissingIntentRequest{
		IntentID:         intent.IntentID,
		ExpectedRevision: intent.Revision,
		Confirmed:        true,
	}, fixedNow(t)); err != nil {
		t.Fatalf("a claim on another scope must not block recovery: %v", err)
	}
}

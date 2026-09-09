// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lease

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Recovery for an ACTIVE intent whose backend lease no longer exists.
//
// DefaultStartupPolicy refuses startup while such an intent is in the store,
// and that is correct: an intent claiming a tuner the backend does not know
// about is exactly the ambiguity that must not be resolved by guessing. But
// refusing is only half a contract. A process that crashed mid-session leaves
// this state behind and then cannot start, with no supported way out -- the
// only recourse was hand-editing the intent JSON, which is how a staging
// outage was actually resolved.
//
// This is that way out. It resolves the inconsistency rather than bypassing
// it: the refusal stays, and the operator gets an operation that can only
// succeed when the backend has independently confirmed the lease is gone.
//
// There is deliberately no --force, no --ignore-reconciliation and no
// --disable-safety. Every condition below is a refusal, never a warning.

var (
	// ErrRecoveryNotConfirmed is returned when the caller did not state explicit intent.
	ErrRecoveryNotConfirmed = errors.New("lease recovery requires explicit operator confirmation")
	// ErrRecoveryTargetAmbiguous is returned when the request does not identify exactly one intent.
	ErrRecoveryTargetAmbiguous = errors.New("lease recovery target is ambiguous")
	// ErrRecoveryIntentNotFound is returned when the targeted intent is absent from the store.
	ErrRecoveryIntentNotFound = errors.New("lease recovery target intent not found")
	// ErrRecoveryIntentNotActive is returned when the targeted intent is not in ACTIVE state.
	ErrRecoveryIntentNotActive = errors.New("lease recovery target intent is not ACTIVE")
	// ErrRecoveryRevisionMismatch is returned when the intent changed since the operator read it.
	ErrRecoveryRevisionMismatch = errors.New("lease recovery revision mismatch")
	// ErrRecoveryBackendLeasePresent is returned when the backend still holds a matching lease.
	ErrRecoveryBackendLeasePresent = errors.New("lease recovery refused: backend lease still exists")
	// ErrRecoveryBackendUnverifiable is returned when backend state could not be established.
	ErrRecoveryBackendUnverifiable = errors.New("lease recovery refused: backend state could not be verified")
)

// intentStillClaimsScope reports whether an intent in this state is still a
// live claim on its scope. Everything that is not terminal is: PENDING is an
// acquisition in flight, RELEASING a release not yet confirmed, and
// RECOVERY_REQUIRED an unresolved question by definition.
func intentStillClaimsScope(state IntentState) bool {
	switch state {
	case IntentStatePending, IntentStateActive, IntentStateReleasing, IntentStateRecoveryRequired:
		return true
	default:
		return false
	}
}

// RecoverMissingIntentRequest identifies exactly one intent and the revision
// the operator believes it is at.
type RecoverMissingIntentRequest struct {
	// IntentID is the exact intent to transition. Required.
	IntentID ID
	// ExpectedRevision is the revision the operator observed. The transition
	// applies only if the store still agrees, so a concurrent change loses.
	ExpectedRevision uint64
	// Confirmed must be true. It carries the operator's explicit intent and
	// has no default that would let this run by accident.
	Confirmed bool
	// Reason is recorded in the audit result.
	Reason string
}

// RecoverMissingIntentResult is the audit record of a completed recovery.
type RecoverMissingIntentResult struct {
	IntentID              ID          `json:"intent_id"`
	LeaseID               ID          `json:"lease_id"`
	Owner                 Owner       `json:"owner"`
	Scope                 Scope       `json:"scope"`
	FromState             IntentState `json:"from_state"`
	ToState               IntentState `json:"to_state"`
	FromRevision          uint64      `json:"from_revision"`
	ToRevision            uint64      `json:"to_revision"`
	BackendLeasesObserved int         `json:"backend_leases_observed"`
	VerifiedAt            time.Time   `json:"verified_at"`
	Reason                string      `json:"reason,omitempty"`
}

// RecoverMissingIntent transitions one ACTIVE intent to TERMINAL after proving
// the backend holds no matching lease.
//
// The backend is re-read here rather than trusting an earlier reconciliation
// report: the report that motivated the operator may be minutes old, and a
// lease that came back in the meantime must abort the recovery.
func RecoverMissingIntent(
	ctx context.Context,
	store IntentStore,
	backend ObservableLeaseBackend,
	req RecoverMissingIntentRequest,
	nowFunc func() time.Time,
) (*RecoverMissingIntentResult, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: intent store is required", ErrInvalidIntent)
	}
	if backend == nil {
		return nil, fmt.Errorf("%w: no backend to query", ErrRecoveryBackendUnverifiable)
	}
	if !req.Confirmed {
		return nil, ErrRecoveryNotConfirmed
	}
	if req.IntentID == "" {
		return nil, fmt.Errorf("%w: intent id is required", ErrRecoveryTargetAmbiguous)
	}
	if nowFunc == nil {
		nowFunc = time.Now
	}

	intent, err := store.GetIntent(ctx, req.IntentID)
	// Stores signal absence either as a sentinel error or as a nil record;
	// both mean the operator named something that is not there.
	if errors.Is(err, ErrIntentNotFound) || (err == nil && intent == nil) {
		return nil, fmt.Errorf("%w: %s", ErrRecoveryIntentNotFound, req.IntentID)
	}
	if err != nil {
		return nil, fmt.Errorf("read intent %s: %w", req.IntentID, err)
	}
	if intent.State != IntentStateActive {
		return nil, fmt.Errorf("%w: %s is %s", ErrRecoveryIntentNotActive, req.IntentID, intent.State)
	}
	if intent.Revision != req.ExpectedRevision {
		return nil, fmt.Errorf("%w: %s is at revision %d, operator expected %d",
			ErrRecoveryRevisionMismatch, req.IntentID, intent.Revision, req.ExpectedRevision)
	}

	// By running this, the operator asserts that this exact intent is the sole
	// stale claim on its scope. Any other live claim there -- not just another
	// ACTIVE one, but a PENDING acquisition, an unconfirmed RELEASING, or an
	// unresolved RECOVERY_REQUIRED -- makes that assertion untrue, so refuse
	// rather than pick. A TERMINAL sibling is history and claims nothing.
	//
	// Siblings are never touched: this refuses, it does not tidy up.
	all, err := store.ListIntents(ctx)
	if err != nil {
		return nil, fmt.Errorf("list intents: %w", err)
	}
	for _, other := range all {
		if other.IntentID == intent.IntentID {
			continue
		}
		if other.Scope == intent.Scope && intentStillClaimsScope(other.State) {
			return nil, fmt.Errorf("%w: scope %s also has %s intent %s",
				ErrRecoveryTargetAmbiguous, intent.Scope, other.State, other.IntentID)
		}
	}

	// Re-verify backend absence now, not from a stale report.
	leases, err := backend.ListLeases(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRecoveryBackendUnverifiable, err)
	}
	now := nowFunc()
	for i := range leases {
		l := &leases[i]
		if !l.IsActive(now) {
			continue
		}
		if l.ID == intent.LeaseID || (l.Scope == intent.Scope && l.Owner == intent.Owner) {
			return nil, fmt.Errorf("%w: lease %s on scope %s is held by %s",
				ErrRecoveryBackendLeasePresent, l.ID, l.Scope, l.Owner)
		}
	}

	updated := *intent
	updated.State = IntentStateTerminal
	updated.Revision = intent.Revision + 1
	updated.UpdatedAt = now
	if err := store.SaveIntent(ctx, updated); err != nil {
		return nil, fmt.Errorf("persist recovered intent %s: %w", req.IntentID, err)
	}

	// Read back: the operation reports what the store actually holds, not what
	// it was asked to write.
	persisted, err := store.GetIntent(ctx, req.IntentID)
	if err != nil {
		return nil, fmt.Errorf("verify recovered intent %s: %w", req.IntentID, err)
	}
	if persisted == nil || persisted.State != IntentStateTerminal || persisted.Revision != updated.Revision {
		return nil, fmt.Errorf("%w: intent %s did not persist as TERMINAL", ErrIntentDurabilityUncertain, req.IntentID)
	}

	return &RecoverMissingIntentResult{
		IntentID:              persisted.IntentID,
		LeaseID:               persisted.LeaseID,
		Owner:                 persisted.Owner,
		Scope:                 persisted.Scope,
		FromState:             IntentStateActive,
		ToState:               persisted.State,
		FromRevision:          intent.Revision,
		ToRevision:            persisted.Revision,
		BackendLeasesObserved: len(leases),
		VerifiedAt:            now,
		Reason:                req.Reason,
	}, nil
}

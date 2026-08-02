// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lease

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

var (
	ErrInvalidIntent  = errors.New("invalid lease intent")
	ErrIntentNotFound = errors.New("lease intent not found")
	ErrIntentConflict = errors.New("lease intent revision conflict")
)

// IntentState represents the state of a LeaseIntent.
type IntentState string

const (
	IntentStatePending   IntentState = "PENDING"
	IntentStateActive    IntentState = "ACTIVE"
	IntentStateReleasing IntentState = "RELEASING"
	IntentStateTerminal  IntentState = "TERMINAL"
)

// LeaseIntent represents an intended or tracked lease reservation in the IntentStore.
type LeaseIntent struct {
	IntentID    ID          `json:"intent_id"`
	LeaseID     ID          `json:"lease_id,omitempty"`
	Owner       Owner       `json:"owner"`
	Scope       Scope       `json:"scope"`
	CompositeID ID          `json:"composite_id,omitempty"`
	State       IntentState `json:"state"`
	Revision    uint64      `json:"revision"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

// IntentStore defines the contract for persisting and querying lease intents across processes/restarts.
type IntentStore interface {
	SaveIntent(ctx context.Context, intent LeaseIntent) error
	GetIntent(ctx context.Context, intentID ID) (*LeaseIntent, error)
	ListIntents(ctx context.Context) ([]LeaseIntent, error)
	DeleteIntent(ctx context.Context, intentID ID) error
}

// InMemoryIntentStore is a thread-safe in-memory reference implementation of IntentStore.
type InMemoryIntentStore struct {
	mu      sync.RWMutex
	intents map[ID]LeaseIntent
	nowFunc func() time.Time
}

// InMemoryIntentStoreConfig configures InMemoryIntentStore.
type InMemoryIntentStoreConfig struct {
	Clock func() time.Time
}

// NewInMemoryIntentStore creates a new InMemoryIntentStore instance.
func NewInMemoryIntentStore() *InMemoryIntentStore {
	return NewInMemoryIntentStoreWithConfig(InMemoryIntentStoreConfig{})
}

// NewInMemoryIntentStoreWithConfig creates an InMemoryIntentStore with custom config.
func NewInMemoryIntentStoreWithConfig(cfg InMemoryIntentStoreConfig) *InMemoryIntentStore {
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	return &InMemoryIntentStore{
		intents: make(map[ID]LeaseIntent),
		nowFunc: clock,
	}
}

func (s *InMemoryIntentStore) SaveIntent(ctx context.Context, intent LeaseIntent) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if err := validateIntent(intent); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.nowFunc()
	existing, exists := s.intents[intent.IntentID]

	if !exists {
		if intent.Revision == 0 {
			intent.Revision = 1
		} else if intent.Revision != 1 {
			return fmt.Errorf("%w: new intent must start at revision 1, got %d", ErrIntentConflict, intent.Revision)
		}
		if intent.CreatedAt.IsZero() {
			intent.CreatedAt = now
		}
	} else {
		if intent.Revision != existing.Revision+1 {
			return fmt.Errorf("%w: intent update revision must be exactly existing revision+1 (%d), got %d",
				ErrIntentConflict, existing.Revision+1, intent.Revision)
		}
		if intent.CreatedAt.IsZero() {
			intent.CreatedAt = existing.CreatedAt
		}
	}

	if intent.UpdatedAt.IsZero() || intent.UpdatedAt.Before(intent.CreatedAt) {
		intent.UpdatedAt = now
	}

	nextMap := cloneIntentMap(s.intents)
	nextMap[intent.IntentID] = intent
	s.intents = nextMap
	return nil
}

func (s *InMemoryIntentStore) GetIntent(ctx context.Context, intentID ID) (*LeaseIntent, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	intent, exists := s.intents[intentID]
	if !exists {
		return nil, fmt.Errorf("%w: intent %s", ErrIntentNotFound, intentID)
	}
	return &intent, nil
}

func (s *InMemoryIntentStore) ListIntents(ctx context.Context) ([]LeaseIntent, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	res := make([]LeaseIntent, 0, len(s.intents))
	for _, intent := range s.intents {
		res = append(res, intent)
	}
	return res, nil
}

func (s *InMemoryIntentStore) DeleteIntent(ctx context.Context, intentID ID) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.intents[intentID]; !exists {
		return nil
	}

	nextMap := cloneIntentMap(s.intents)
	delete(nextMap, intentID)
	s.intents = nextMap
	return nil
}

// ObservableLeaseBackend extends LeaseBackend with the ability to query backend leases for reconciliation.
type ObservableLeaseBackend interface {
	LeaseBackend
	ListLeases(ctx context.Context) ([]Lease, error)
}

// ReconciliationStatus defines the 5 deterministic outcome statuses of a lease reconciliation.
type ReconciliationStatus string

const (
	ReconciliationStatusConfirmed                 ReconciliationStatus = "confirmed"
	ReconciliationStatusReleased                  ReconciliationStatus = "released"
	ReconciliationStatusOrphaned                  ReconciliationStatus = "orphaned"
	ReconciliationStatusMissing                   ReconciliationStatus = "missing"
	ReconciliationStatusManualInterventionRequired ReconciliationStatus = "manual-intervention-required"
	CompositeStatusBroken                         ReconciliationStatus = "broken"
)

// ReconciliationReasonCode provides structured machine-readable explanation for reconciliation items.
type ReconciliationReasonCode string

const (
	ReasonReconciliationMatchConfirmed        ReconciliationReasonCode = "RECONCILIATION_MATCH_CONFIRMED"
	ReasonReconciliationReleased              ReconciliationReasonCode = "RECONCILIATION_RELEASED"
	ReasonReconciliationLeaseMissing          ReconciliationReasonCode = "RECONCILIATION_LEASE_MISSING"
	ReasonReconciliationOrphaned              ReconciliationReasonCode = "RECONCILIATION_ORPHANED"
	ReasonReconciliationOwnerMismatch         ReconciliationReasonCode = "RECONCILIATION_OWNER_MISMATCH"
	ReasonReconciliationLeaseIDMismatch       ReconciliationReasonCode = "RECONCILIATION_LEASE_ID_MISMATCH"
	ReasonReconciliationDuplicateIntents      ReconciliationReasonCode = "RECONCILIATION_DUPLICATE_INTENTS"
	ReasonReconciliationDuplicateBackendLeases ReconciliationReasonCode = "RECONCILIATION_DUPLICATE_BACKEND_LEASES"
	ReasonReconciliationOrphanReleaseFailed   ReconciliationReasonCode = "RECONCILIATION_ORPHAN_RELEASE_FAILED"
	ReasonReconciliationRevalidationFailed    ReconciliationReasonCode = "RECONCILIATION_REVALIDATION_FAILED"
	ReasonReconciliationPendingAcquisition   ReconciliationReasonCode = "RECONCILIATION_PENDING_ACQUISITION"
	ReasonReconciliationReleasePending       ReconciliationReasonCode = "RECONCILIATION_RELEASE_PENDING"
	ReasonReconciliationBrokenComposite       ReconciliationReasonCode = "RECONCILIATION_BROKEN_COMPOSITE"
)

// RemediationOutcome describes the result of an automated remediation attempt.
type RemediationOutcome string

const (
	RemediationNotAttempted RemediationOutcome = "NOT_ATTEMPTED"
	RemediationSucceeded    RemediationOutcome = "SUCCEEDED"
	RemediationFailed       RemediationOutcome = "FAILED"
	RemediationSkippedStale RemediationOutcome = "SKIPPED_STALE_OBSERVATION"
)

// ReconciliationItem describes the reconciliation result for a single resource lease/intent.
type ReconciliationItem struct {
	Scope              Scope                    `json:"scope"`
	IntentID           ID                       `json:"intent_id,omitempty"`
	BackendID          ID                       `json:"backend_id,omitempty"`
	IntentOwner        Owner                    `json:"intent_owner,omitempty"`
	BackendOwner       Owner                    `json:"backend_owner,omitempty"`
	ObservedStatus     ReconciliationStatus     `json:"observed_status"`
	FinalStatus        ReconciliationStatus     `json:"final_status"`
	ReasonCode         ReconciliationReasonCode `json:"reason_code"`
	RemediationOutcome RemediationOutcome       `json:"remediation_outcome"`
	RemediationDetails string                   `json:"remediation_details,omitempty"`
	Diagnostic         string                   `json:"diagnostic,omitempty"`
}

// CompositeReconciliationSummary summarizes reconciliation state for a multi-resource composite lease.
type CompositeReconciliationSummary struct {
	CompositeID ID                   `json:"composite_id"`
	Confirmed   int                  `json:"confirmed"`
	Missing     int                  `json:"missing"`
	Released    int                  `json:"released"`
	Pending     int                  `json:"pending"`
	Conflicted  int                  `json:"conflicted"`
	Status      ReconciliationStatus `json:"status"`
}

// ReconciliationSummary provides aggregate metrics for a reconciliation run.
type ReconciliationSummary struct {
	TotalChecked               int                              `json:"total_checked"`
	Confirmed                  int                              `json:"confirmed"`
	Released                   int                              `json:"released"`
	Orphaned                   int                              `json:"orphaned"`
	Missing                    int                              `json:"missing"`
	ManualInterventionRequired int                              `json:"manual_intervention_required"`
	RemediatedOrphans          int                              `json:"remediated_orphans"`
	Composites                 []CompositeReconciliationSummary `json:"composites,omitempty"`
}

// ReconciliationReport holds the output of a ReconciliationEngine execution.
type ReconciliationReport struct {
	Timestamp time.Time             `json:"timestamp"`
	Summary   ReconciliationSummary `json:"summary"`
	Items     []ReconciliationItem  `json:"items"`
}

// ReconcilerConfig holds configuration options for the Reconciler.
type ReconcilerConfig struct {
	IntentStore          IntentStore
	Backend              ObservableLeaseBackend
	AutoRemediateOrphans bool
	RemediationTimeout   time.Duration
	Clock                func() time.Time
}

// Reconciler inspects LeaseIntents vs actual ObservableLeaseBackend reality and classifies each resource state deterministically.
type Reconciler struct {
	intentStore          IntentStore
	backend              ObservableLeaseBackend
	autoRemediateOrphans bool
	remediationTimeout   time.Duration
	nowFunc              func() time.Time
}

// NewReconciler creates a new Reconciler instance.
func NewReconciler(cfg ReconcilerConfig) (*Reconciler, error) {
	if cfg.IntentStore == nil {
		return nil, fmt.Errorf("%w: intent store is required", ErrInvalidIntent)
	}
	if cfg.Backend == nil {
		return nil, fmt.Errorf("%w: backend is required", ErrBindingUnavailable)
	}
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	remTimeout := cfg.RemediationTimeout
	if remTimeout <= 0 {
		remTimeout = DefaultCleanupTimeout
	}
	return &Reconciler{
		intentStore:          cfg.IntentStore,
		backend:              cfg.Backend,
		autoRemediateOrphans: cfg.AutoRemediateOrphans,
		remediationTimeout:   remTimeout,
		nowFunc:              clock,
	}, nil
}

// Reconcile performs reconciliation comparing active intents and backend leases.
// Output items and reports are deterministically ordered.
func (r *Reconciler) Reconcile(ctx context.Context) (*ReconciliationReport, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}

	intents, err := r.intentStore.ListIntents(ctx)
	if err != nil {
		return nil, fmt.Errorf("list intents for reconciliation: %w", err)
	}

	backendLeases, err := r.backend.ListLeases(ctx)
	if err != nil {
		return nil, fmt.Errorf("list backend leases for reconciliation: %w", err)
	}

	now := r.nowFunc()

	// Deterministically sort intents by Scope -> IntentID
	sort.SliceStable(intents, func(i, j int) bool {
		if intents[i].Scope != intents[j].Scope {
			return intents[i].Scope < intents[j].Scope
		}
		return intents[i].IntentID < intents[j].IntentID
	})

	// Deterministically sort backend leases by Scope -> ID
	sort.SliceStable(backendLeases, func(i, j int) bool {
		if backendLeases[i].Scope != backendLeases[j].Scope {
			return backendLeases[i].Scope < backendLeases[j].Scope
		}
		return backendLeases[i].ID < backendLeases[j].ID
	})

	// Group intents by Scope
	intentsByScope := make(map[Scope][]LeaseIntent)
	for _, intent := range intents {
		intentsByScope[intent.Scope] = append(intentsByScope[intent.Scope], intent)
	}

	// Group active backend leases by Scope
	backendByScope := make(map[Scope][]Lease)
	for _, l := range backendLeases {
		if l.IsActive(now) {
			backendByScope[l.Scope] = append(backendByScope[l.Scope], l)
		}
	}

	var items []ReconciliationItem
	summary := ReconciliationSummary{}

	processedScopes := make(map[Scope]bool)
	compositeTracker := make(map[ID]*CompositeReconciliationSummary)

	trackComposite := func(compID ID, state IntentState, status ReconciliationStatus) {
		if compID == "" {
			return
		}
		comp, exists := compositeTracker[compID]
		if !exists {
			comp = &CompositeReconciliationSummary{CompositeID: compID}
			compositeTracker[compID] = comp
		}
		if status == ReconciliationStatusManualInterventionRequired {
			comp.Conflicted++
			return
		}
		switch state {
		case IntentStatePending, IntentStateReleasing:
			comp.Pending++
		case IntentStateActive:
			if status == ReconciliationStatusConfirmed {
				comp.Confirmed++
			} else if status == ReconciliationStatusMissing {
				comp.Missing++
			} else {
				comp.Conflicted++
			}
		case IntentStateTerminal:
			if status == ReconciliationStatusReleased {
				comp.Released++
			} else {
				comp.Conflicted++
			}
		}
	}

	// 1. Process all scopes derived from intents
	for _, rawIntent := range intents {
		scope := rawIntent.Scope
		if processedScopes[scope] {
			continue
		}
		processedScopes[scope] = true

		scopeIntents := intentsByScope[scope]

		// Authoritative Scope Intent Selection: Filter non-terminal intents (PENDING, ACTIVE, RELEASING)
		nonTerminalIntents := make([]LeaseIntent, 0)
		terminalIntents := make([]LeaseIntent, 0)
		for _, in := range scopeIntents {
			if in.State == IntentStateActive || in.State == IntentStatePending || in.State == IntentStateReleasing {
				nonTerminalIntents = append(nonTerminalIntents, in)
			} else {
				terminalIntents = append(terminalIntents, in)
			}
		}

		bLeases := backendByScope[scope]

		// Check for multiple non-terminal intents on same scope (Conflict)
		if len(nonTerminalIntents) > 1 {
			item := ReconciliationItem{
				Scope:              scope,
				IntentID:           nonTerminalIntents[0].IntentID,
				IntentOwner:        nonTerminalIntents[0].Owner,
				ObservedStatus:     ReconciliationStatusManualInterventionRequired,
				FinalStatus:        ReconciliationStatusManualInterventionRequired,
				ReasonCode:         ReasonReconciliationDuplicateIntents,
				RemediationOutcome: RemediationNotAttempted,
				Diagnostic:         fmt.Sprintf("multiple non-terminal intents (%d) exist for scope %s", len(nonTerminalIntents), scope),
			}
			if len(bLeases) > 0 {
				item.BackendID = bLeases[0].ID
				item.BackendOwner = bLeases[0].Owner
			}
			items = append(items, item)
			summary.ManualInterventionRequired++
			trackComposite(nonTerminalIntents[0].CompositeID, nonTerminalIntents[0].State, item.ObservedStatus)
			continue
		}

		// Check for duplicate active backend leases on same scope (Conflict)
		if len(bLeases) > 1 {
			targetIntent := rawIntent
			if len(nonTerminalIntents) == 1 {
				targetIntent = nonTerminalIntents[0]
			}
			item := ReconciliationItem{
				Scope:              scope,
				IntentID:           targetIntent.IntentID,
				BackendID:          bLeases[0].ID,
				IntentOwner:        targetIntent.Owner,
				BackendOwner:       bLeases[0].Owner,
				ObservedStatus:     ReconciliationStatusManualInterventionRequired,
				FinalStatus:        ReconciliationStatusManualInterventionRequired,
				ReasonCode:         ReasonReconciliationDuplicateBackendLeases,
				RemediationOutcome: RemediationNotAttempted,
				Diagnostic:         fmt.Sprintf("multiple active backend leases (%d) exist for scope %s", len(bLeases), scope),
			}
			items = append(items, item)
			summary.ManualInterventionRequired++
			trackComposite(targetIntent.CompositeID, targetIntent.State, item.ObservedStatus)
			continue
		}

		// Select authoritative intent: if exactly one non-terminal exists, use it; otherwise use latest terminal intent
		var intent LeaseIntent
		if len(nonTerminalIntents) == 1 {
			intent = nonTerminalIntents[0]
		} else if len(terminalIntents) > 0 {
			// Sort terminal intents by UpdatedAt desc to pick latest
			sort.Slice(terminalIntents, func(i, j int) bool {
				return terminalIntents[i].UpdatedAt.After(terminalIntents[j].UpdatedAt)
			})
			intent = terminalIntents[0]
		} else {
			intent = rawIntent
		}

		item := ReconciliationItem{
			Scope:              scope,
			IntentID:           intent.IntentID,
			IntentOwner:        intent.Owner,
			RemediationOutcome: RemediationNotAttempted,
		}

		var bLease *Lease
		if len(bLeases) == 1 {
			bLease = &bLeases[0]
			item.BackendID = bLease.ID
			item.BackendOwner = bLease.Owner
		}

		// Intent State Matrix
		switch intent.State {
		case IntentStatePending:
			if bLease == nil {
				item.ObservedStatus = ReconciliationStatusConfirmed
				item.ReasonCode = ReasonReconciliationPendingAcquisition
				item.Diagnostic = "intent is PENDING lease acquisition; no backend lease present yet"
				summary.Confirmed++
			} else {
				item.ObservedStatus = ReconciliationStatusManualInterventionRequired
				item.ReasonCode = ReasonReconciliationOwnerMismatch
				item.Diagnostic = fmt.Sprintf("intent state is PENDING, but backend lease ID %s is already active (owner %s)", bLease.ID, bLease.Owner)
				summary.ManualInterventionRequired++
			}

		case IntentStateActive:
			if bLease == nil {
				item.ObservedStatus = ReconciliationStatusMissing
				item.ReasonCode = ReasonReconciliationLeaseMissing
				item.Diagnostic = fmt.Sprintf("active intent expects lease ID %s, missing in backend", intent.LeaseID)
				summary.Missing++
			} else {
				if bLease.ID == intent.LeaseID && bLease.Owner == intent.Owner {
					item.ObservedStatus = ReconciliationStatusConfirmed
					item.ReasonCode = ReasonReconciliationMatchConfirmed
					summary.Confirmed++
				} else if bLease.Owner != intent.Owner {
					item.ObservedStatus = ReconciliationStatusManualInterventionRequired
					item.ReasonCode = ReasonReconciliationOwnerMismatch
					item.Diagnostic = fmt.Sprintf("owner mismatch: intent owner=%s, backend owner=%s", intent.Owner, bLease.Owner)
					summary.ManualInterventionRequired++
				} else {
					item.ObservedStatus = ReconciliationStatusManualInterventionRequired
					item.ReasonCode = ReasonReconciliationLeaseIDMismatch
					item.Diagnostic = fmt.Sprintf("lease ID mismatch: intent leaseID=%s, backend leaseID=%s", intent.LeaseID, bLease.ID)
					summary.ManualInterventionRequired++
				}
			}

		case IntentStateReleasing:
			if bLease != nil {
				item.ObservedStatus = ReconciliationStatusConfirmed
				item.ReasonCode = ReasonReconciliationReleasePending
				item.Diagnostic = fmt.Sprintf("intent is RELEASING; backend lease ID %s release is in progress", bLease.ID)
				summary.Confirmed++
			} else {
				item.ObservedStatus = ReconciliationStatusReleased
				item.ReasonCode = ReasonReconciliationReleased
				summary.Released++
			}

		case IntentStateTerminal:
			if bLease == nil {
				item.ObservedStatus = ReconciliationStatusReleased
				item.ReasonCode = ReasonReconciliationReleased
				summary.Released++
			} else {
				item.ObservedStatus = ReconciliationStatusManualInterventionRequired
				item.ReasonCode = ReasonReconciliationOwnerMismatch
				item.Diagnostic = fmt.Sprintf("intent state is TERMINAL, but backend holds active lease ID %s (owner %s)", bLease.ID, bLease.Owner)
				summary.ManualInterventionRequired++
			}
		}

		item.FinalStatus = item.ObservedStatus
		trackComposite(intent.CompositeID, intent.State, item.ObservedStatus)
		items = append(items, item)
	}

	// 2. Process remaining active backend leases not covered by intents (ORPHANED)
	for _, bLease := range backendLeases {
		if !bLease.IsActive(now) || processedScopes[bLease.Scope] {
			continue
		}
		scope := bLease.Scope
		processedScopes[scope] = true

		bLeasesOnScope := backendByScope[scope]
		if len(bLeasesOnScope) > 1 {
			item := ReconciliationItem{
				Scope:              scope,
				BackendID:          bLease.ID,
				BackendOwner:       bLease.Owner,
				ObservedStatus:     ReconciliationStatusManualInterventionRequired,
				FinalStatus:        ReconciliationStatusManualInterventionRequired,
				ReasonCode:         ReasonReconciliationDuplicateBackendLeases,
				RemediationOutcome: RemediationNotAttempted,
				Diagnostic:         fmt.Sprintf("multiple active backend leases (%d) exist for scope %s", len(bLeasesOnScope), scope),
			}
			items = append(items, item)
			summary.ManualInterventionRequired++
			continue
		}

		item := ReconciliationItem{
			Scope:              scope,
			BackendID:          bLease.ID,
			BackendOwner:       bLease.Owner,
			ObservedStatus:     ReconciliationStatusOrphaned,
			FinalStatus:        ReconciliationStatusOrphaned,
			ReasonCode:         ReasonReconciliationOrphaned,
			RemediationOutcome: RemediationNotAttempted,
		}

		if r.autoRemediateOrphans {
			// Compare-before-act: re-verify backend & intent state immediately prior to release
			recheckLeases, rErr := r.backend.ListLeases(ctx)
			recheckIntents, rIErr := r.intentStore.ListIntents(ctx)

			if rErr != nil || rIErr != nil {
				item.RemediationOutcome = RemediationFailed
				item.ReasonCode = ReasonReconciliationRevalidationFailed
				item.Diagnostic = fmt.Sprintf("compare-before-act revalidation failed: backendErr=%v, intentErr=%v", rErr, rIErr)
			} else {
				recheckNow := r.nowFunc()
				stillOrphaned := false
				var activeLeaseOnScope *Lease
				for _, rl := range recheckLeases {
					if rl.Scope == scope && rl.ID == bLease.ID && rl.IsActive(recheckNow) {
						activeLeaseOnScope = &rl
						break
					}
				}
				hasActiveIntent := false
				for _, ri := range recheckIntents {
					if ri.Scope == scope && (ri.State == IntentStateActive || ri.State == IntentStatePending) {
						hasActiveIntent = true
						break
					}
				}
				if activeLeaseOnScope != nil && !hasActiveIntent {
					stillOrphaned = true
				}

				if !stillOrphaned {
					item.RemediationOutcome = RemediationSkippedStale
					item.RemediationDetails = "remediation skipped: state changed prior to execution"
				} else {
					// Execute detached bounded cleanup using remCtx for both Release AND Confirmation!
					remCtx, remCancel := context.WithTimeout(context.WithoutCancel(nonNilContext(ctx)), r.remediationTimeout)

					_, relErr := r.backend.Release(remCtx, bLease.ID, bLease.Owner, ReasonReconciliationOrphanCleanup)
					if relErr != nil {
						remCancel()
						item.RemediationOutcome = RemediationFailed
						item.ReasonCode = ReasonReconciliationOrphanReleaseFailed
						item.Diagnostic = fmt.Sprintf("failed to release orphaned lease %s: %v", bLease.ID, relErr)
					} else {
						// Post-remediation confirmation via remCtx BEFORE calling remCancel()
						confirmLeases, confirmErr := r.backend.ListLeases(remCtx)
						remCancel()

						if confirmErr != nil {
							item.RemediationOutcome = RemediationFailed
							item.ReasonCode = ReasonReconciliationOrphanReleaseFailed
							item.Diagnostic = fmt.Sprintf("orphan release could not be confirmed due to backend error: %v", confirmErr)
						} else {
							cNow := r.nowFunc()
							leaseStillActive := false
							for _, cl := range confirmLeases {
								if cl.ID == bLease.ID && cl.IsActive(cNow) {
									leaseStillActive = true
									break
								}
							}
							if !leaseStillActive {
								item.RemediationOutcome = RemediationSucceeded
								item.FinalStatus = ReconciliationStatusReleased
								item.RemediationDetails = fmt.Sprintf("successfully released orphaned backend lease ID %s", bLease.ID)
								summary.RemediatedOrphans++
							} else {
								item.RemediationOutcome = RemediationFailed
								item.ReasonCode = ReasonReconciliationOrphanReleaseFailed
								item.Diagnostic = fmt.Sprintf("release returned nil error but lease %s remains active in backend", bLease.ID)
							}
						}
					}
				}
			}
		}

		summary.Orphaned++
		items = append(items, item)
	}

	// Deterministically sort final report items by Scope -> IntentID -> BackendID -> ObservedStatus -> ReasonCode
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Scope != items[j].Scope {
			return items[i].Scope < items[j].Scope
		}
		if items[i].IntentID != items[j].IntentID {
			return items[i].IntentID < items[j].IntentID
		}
		if items[i].BackendID != items[j].BackendID {
			return items[i].BackendID < items[j].BackendID
		}
		if items[i].ObservedStatus != items[j].ObservedStatus {
			return items[i].ObservedStatus < items[j].ObservedStatus
		}
		return items[i].ReasonCode < items[j].ReasonCode
	})

	summary.TotalChecked = len(items)

	// Format Composite Summaries
	if len(compositeTracker) > 0 {
		compKeys := make([]ID, 0, len(compositeTracker))
		for k := range compositeTracker {
			compKeys = append(compKeys, k)
		}
		sort.Slice(compKeys, func(i, j int) bool { return compKeys[i] < compKeys[j] })

		for _, k := range compKeys {
			comp := compositeTracker[k]
			if comp.Conflicted > 0 || (comp.Confirmed > 0 && comp.Missing > 0) {
				comp.Status = CompositeStatusBroken
			} else if comp.Missing > 0 {
				comp.Status = ReconciliationStatusManualInterventionRequired
			} else if comp.Confirmed > 0 || comp.Pending > 0 {
				comp.Status = ReconciliationStatusConfirmed
			} else {
				comp.Status = ReconciliationStatusReleased
			}
			summary.Composites = append(summary.Composites, *comp)
		}
	}

	return &ReconciliationReport{
		Timestamp: now,
		Summary:   summary,
		Items:     items,
	}, nil
}

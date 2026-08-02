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
}

// NewInMemoryIntentStore creates a new InMemoryIntentStore instance.
func NewInMemoryIntentStore() *InMemoryIntentStore {
	return &InMemoryIntentStore{
		intents: make(map[ID]LeaseIntent),
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

	existing, exists := s.intents[intent.IntentID]
	if exists && intent.Revision < existing.Revision {
		return fmt.Errorf("%w: intent revision %d is lower than existing revision %d", ErrIntentConflict, intent.Revision, existing.Revision)
	}

	if intent.CreatedAt.IsZero() {
		if exists {
			intent.CreatedAt = existing.CreatedAt
		} else {
			intent.CreatedAt = time.Now()
		}
	}
	if intent.UpdatedAt.IsZero() {
		intent.UpdatedAt = time.Now()
	}

	s.intents[intent.IntentID] = intent
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

	delete(s.intents, intentID)
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
	Status             ReconciliationStatus     `json:"status"`
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

	// Helper to track composite summary
	trackComposite := func(compID ID, status ReconciliationStatus) {
		if compID == "" {
			return
		}
		comp, exists := compositeTracker[compID]
		if !exists {
			comp = &CompositeReconciliationSummary{CompositeID: compID}
			compositeTracker[compID] = comp
		}
		switch status {
		case ReconciliationStatusConfirmed:
			comp.Confirmed++
		case ReconciliationStatusMissing:
			comp.Missing++
		default:
			comp.Conflicted++
		}
	}

	// 1. Process allScopes in deterministic order derived from sortedIntents
	for _, intent := range intents {
		scope := intent.Scope
		if processedScopes[scope] {
			continue
		}
		processedScopes[scope] = true

		scopeIntents := intentsByScope[scope]
		activeIntents := make([]LeaseIntent, 0)
		for _, in := range scopeIntents {
			if in.State == IntentStateActive {
				activeIntents = append(activeIntents, in)
			}
		}

		bLeases := backendByScope[scope]

		// Check for duplicate active intents on same scope
		if len(activeIntents) > 1 {
			item := ReconciliationItem{
				Scope:              scope,
				IntentID:           intent.IntentID,
				IntentOwner:        intent.Owner,
				Status:             ReconciliationStatusManualInterventionRequired,
				ReasonCode:         ReasonReconciliationDuplicateIntents,
				RemediationOutcome: RemediationNotAttempted,
				Diagnostic:         fmt.Sprintf("multiple active intents (%d) exist for scope %s", len(activeIntents), scope),
			}
			if len(bLeases) > 0 {
				item.BackendID = bLeases[0].ID
				item.BackendOwner = bLeases[0].Owner
			}
			items = append(items, item)
			summary.ManualInterventionRequired++
			trackComposite(intent.CompositeID, item.Status)
			continue
		}

		// Check for duplicate active backend leases on same scope
		if len(bLeases) > 1 {
			item := ReconciliationItem{
				Scope:              scope,
				IntentID:           intent.IntentID,
				BackendID:          bLeases[0].ID,
				IntentOwner:        intent.Owner,
				BackendOwner:       bLeases[0].Owner,
				Status:             ReconciliationStatusManualInterventionRequired,
				ReasonCode:         ReasonReconciliationDuplicateBackendLeases,
				RemediationOutcome: RemediationNotAttempted,
				Diagnostic:         fmt.Sprintf("multiple active backend leases (%d) exist for scope %s", len(bLeases), scope),
			}
			items = append(items, item)
			summary.ManualInterventionRequired++
			trackComposite(intent.CompositeID, item.Status)
			continue
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

		if intent.State != IntentStateActive {
			// Intent is releasing or terminal
			if bLease == nil {
				item.Status = ReconciliationStatusReleased
				item.ReasonCode = ReasonReconciliationReleased
				summary.Released++
			} else {
				item.Status = ReconciliationStatusManualInterventionRequired
				item.ReasonCode = ReasonReconciliationOwnerMismatch
				item.Diagnostic = fmt.Sprintf("intent state is %s, but backend holds active lease ID %s (owner %s)", intent.State, bLease.ID, bLease.Owner)
				summary.ManualInterventionRequired++
			}
		} else {
			// Active Intent
			if bLease == nil {
				item.Status = ReconciliationStatusMissing
				item.ReasonCode = ReasonReconciliationLeaseMissing
				item.Diagnostic = fmt.Sprintf("active intent expects lease ID %s, missing in backend", intent.LeaseID)
				summary.Missing++
			} else {
				if bLease.ID == intent.LeaseID && bLease.Owner == intent.Owner {
					item.Status = ReconciliationStatusConfirmed
					item.ReasonCode = ReasonReconciliationMatchConfirmed
					summary.Confirmed++
				} else if bLease.Owner != intent.Owner {
					item.Status = ReconciliationStatusManualInterventionRequired
					item.ReasonCode = ReasonReconciliationOwnerMismatch
					item.Diagnostic = fmt.Sprintf("owner mismatch: intent owner=%s, backend owner=%s", intent.Owner, bLease.Owner)
					summary.ManualInterventionRequired++
				} else {
					item.Status = ReconciliationStatusManualInterventionRequired
					item.ReasonCode = ReasonReconciliationLeaseIDMismatch
					item.Diagnostic = fmt.Sprintf("lease ID mismatch: intent leaseID=%s, backend leaseID=%s", intent.LeaseID, bLease.ID)
					summary.ManualInterventionRequired++
				}
			}
		}

		trackComposite(intent.CompositeID, item.Status)
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
				Status:             ReconciliationStatusManualInterventionRequired,
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
			Status:             ReconciliationStatusOrphaned,
			ReasonCode:         ReasonReconciliationOrphaned,
			RemediationOutcome: RemediationNotAttempted,
		}

		if r.autoRemediateOrphans {
			// Compare-before-act: re-verify backend & intent state immediately prior to release
			recheckLeases, rErr := r.backend.ListLeases(ctx)
			recheckIntents, rIErr := r.intentStore.ListIntents(ctx)

			stillOrphaned := false
			if rErr == nil && rIErr == nil {
				recheckNow := r.nowFunc()
				var activeLeaseOnScope *Lease
				for _, rl := range recheckLeases {
					if rl.Scope == scope && rl.ID == bLease.ID && rl.IsActive(recheckNow) {
						activeLeaseOnScope = &rl
						break
					}
				}
				hasActiveIntent := false
				for _, ri := range recheckIntents {
					if ri.Scope == scope && ri.State == IntentStateActive {
						hasActiveIntent = true
						break
					}
				}
				if activeLeaseOnScope != nil && !hasActiveIntent {
					stillOrphaned = true
				}
			}

			if !stillOrphaned {
				item.RemediationOutcome = RemediationSkippedStale
				item.RemediationDetails = "remediation skipped: state changed prior to execution"
			} else {
				// Execute detached bounded cleanup
				remCtx, remCancel := context.WithTimeout(context.WithoutCancel(nonNilContext(ctx)), r.remediationTimeout)

				_, relErr := r.backend.Release(remCtx, bLease.ID, bLease.Owner, ReasonReconciliationOrphanCleanup)
				remCancel()

				if relErr != nil {
					item.RemediationOutcome = RemediationFailed
					item.ReasonCode = ReasonReconciliationOrphanReleaseFailed
					item.Diagnostic = fmt.Sprintf("failed to release orphaned lease %s: %v", bLease.ID, relErr)
				} else {
					// Post-remediation confirmation: re-check backend to verify release
					confirmLeases, cErr := r.backend.ListLeases(ctx)
					confirmedReleased := true
					if cErr == nil {
						cNow := r.nowFunc()
						for _, cl := range confirmLeases {
							if cl.ID == bLease.ID && cl.IsActive(cNow) {
								confirmedReleased = false
								break
							}
						}
					}
					if confirmedReleased {
						item.RemediationOutcome = RemediationSucceeded
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

		summary.Orphaned++
		items = append(items, item)
	}

	// Deterministically sort final report items by Scope -> IntentID -> BackendID -> Status -> ReasonCode
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
		if items[i].Status != items[j].Status {
			return items[i].Status < items[j].Status
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
			if comp.Missing > 0 || comp.Conflicted > 0 {
				comp.Status = ReconciliationStatusManualInterventionRequired
			} else {
				comp.Status = ReconciliationStatusConfirmed
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

// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lease

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ReconciliationStatus defines the 5 deterministic outcome statuses of a lease reconciliation.
type ReconciliationStatus string

const (
	ReconciliationStatusConfirmed                 ReconciliationStatus = "confirmed"
	ReconciliationStatusReleased                  ReconciliationStatus = "released"
	ReconciliationStatusOrphaned                  ReconciliationStatus = "orphaned"
	ReconciliationStatusMissing                   ReconciliationStatus = "missing"
	ReconciliationStatusManualInterventionRequired ReconciliationStatus = "manual-intervention-required"
)

// LeaseIntent represents an intended or tracked lease reservation in the IntentStore.
type LeaseIntent struct {
	ID          ID        `json:"id"`
	Owner       Owner     `json:"owner"`
	Scope       Scope     `json:"scope"`
	CompositeID ID        `json:"composite_id,omitempty"`
	State       State     `json:"state"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// IntentStore defines the contract for persisting and querying lease intents across processes/restarts.
type IntentStore interface {
	SaveIntent(ctx context.Context, intent LeaseIntent) error
	GetIntent(ctx context.Context, id ID) (*LeaseIntent, error)
	ListIntents(ctx context.Context) ([]LeaseIntent, error)
	DeleteIntent(ctx context.Context, id ID) error
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
	if intent.ID == "" {
		return fmt.Errorf("%w: intent ID cannot be empty", ErrInvalidBackendResult)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.intents[intent.ID] = intent
	return nil
}

func (s *InMemoryIntentStore) GetIntent(ctx context.Context, id ID) (*LeaseIntent, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	intent, ok := s.intents[id]
	if !ok {
		return nil, ErrNotFound
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

func (s *InMemoryIntentStore) DeleteIntent(ctx context.Context, id ID) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.intents, id)
	return nil
}

// ObservableLeaseBackend extends LeaseBackend with the ability to query backend leases for reconciliation.
type ObservableLeaseBackend interface {
	LeaseBackend
	ListLeases(ctx context.Context) ([]Lease, error)
}

// ReconciliationItem describes the reconciliation result for a single resource lease/intent.
type ReconciliationItem struct {
	Scope              Scope                `json:"scope"`
	IntentID           ID                   `json:"intent_id,omitempty"`
	BackendID          ID                   `json:"backend_id,omitempty"`
	IntentOwner        Owner                `json:"intent_owner,omitempty"`
	BackendOwner       Owner                `json:"backend_owner,omitempty"`
	Status             ReconciliationStatus `json:"status"`
	Remediated         bool                 `json:"remediated,omitempty"`
	RemediationDetails string               `json:"remediation_details,omitempty"`
	Error              string               `json:"error,omitempty"`
}

// ReconciliationSummary provides aggregate metrics for a reconciliation run.
type ReconciliationSummary struct {
	TotalChecked               int `json:"total_checked"`
	Confirmed                  int `json:"confirmed"`
	Released                   int `json:"released"`
	Orphaned                   int `json:"orphaned"`
	Missing                    int `json:"missing"`
	ManualInterventionRequired int `json:"manual_intervention_required"`
	RemediatedOrphans          int `json:"remediated_orphans"`
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
	Clock                func() time.Time
}

// Reconciler inspects LeaseIntents vs actual ObservableLeaseBackend reality and classifies each resource state.
type Reconciler struct {
	intentStore          IntentStore
	backend              ObservableLeaseBackend
	autoRemediateOrphans bool
	nowFunc              func() time.Time
}

// NewReconciler creates a new Reconciler instance.
func NewReconciler(cfg ReconcilerConfig) (*Reconciler, error) {
	if cfg.IntentStore == nil {
		return nil, fmt.Errorf("%w: intent store is required", ErrInvalidBackendResult)
	}
	if cfg.Backend == nil {
		return nil, fmt.Errorf("%w: backend is required", ErrBindingUnavailable)
	}
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	return &Reconciler{
		intentStore:          cfg.IntentStore,
		backend:              cfg.Backend,
		autoRemediateOrphans: cfg.AutoRemediateOrphans,
		nowFunc:              clock,
	}, nil
}

// Reconcile performs reconciliation comparing active intents and backend leases.
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

	// Index intents by ID and Scope
	intentByScope := make(map[Scope]LeaseIntent, len(intents))
	for _, intent := range intents {
		if intent.State == StateAcquired {
			intentByScope[intent.Scope] = intent
		}
	}

	// Index active backend leases by ID and Scope
	backendByScope := make(map[Scope]Lease, len(backendLeases))
	for _, l := range backendLeases {
		if l.IsActive(now) {
			backendByScope[l.Scope] = l
		}
	}

	var items []ReconciliationItem
	summary := ReconciliationSummary{}

	processedScopes := make(map[Scope]bool)

	// 1. Process all Intents
	for _, intent := range intents {
		if processedScopes[intent.Scope] {
			continue
		}

		bLease, hasBackend := backendByScope[intent.Scope]

		item := ReconciliationItem{
			Scope:       intent.Scope,
			IntentID:    intent.ID,
			IntentOwner: intent.Owner,
		}

		if intent.State != StateAcquired {
			// Intent claims released/expired
			if !hasBackend || !bLease.IsActive(now) {
				item.Status = ReconciliationStatusReleased
				if hasBackend {
					item.BackendID = bLease.ID
					item.BackendOwner = bLease.Owner
				}
				summary.Released++
			} else {
				// Intent claims released, but backend still holds an active lease!
				item.BackendID = bLease.ID
				item.BackendOwner = bLease.Owner
				item.Status = ReconciliationStatusManualInterventionRequired
				item.Error = fmt.Sprintf("intent state is %s, but backend holds active lease ID %s (owner %s)", intent.State, bLease.ID, bLease.Owner)
				summary.ManualInterventionRequired++
			}
		} else {
			// Active Intent
			if !hasBackend {
				// Backend has no active lease for this scope
				item.Status = ReconciliationStatusMissing
				item.Error = fmt.Sprintf("intent expects active lease ID %s, but missing in backend", intent.ID)
				summary.Missing++
			} else {
				item.BackendID = bLease.ID
				item.BackendOwner = bLease.Owner

				// Check exact match vs conflict
				if bLease.ID == intent.ID && bLease.Owner == intent.Owner {
					item.Status = ReconciliationStatusConfirmed
					summary.Confirmed++
				} else {
					// ID or Owner mismatch!
					item.Status = ReconciliationStatusManualInterventionRequired
					item.Error = fmt.Sprintf("conflict: intent (ID %s, Owner %s) != backend (ID %s, Owner %s)",
						intent.ID, intent.Owner, bLease.ID, bLease.Owner)
					summary.ManualInterventionRequired++
				}
			}
		}

		processedScopes[intent.Scope] = true
		items = append(items, item)
	}

	// 2. Process remaining active backend leases not covered by intents (ORPHANED)
	for scope, bLease := range backendByScope {
		if processedScopes[scope] {
			continue
		}

		item := ReconciliationItem{
			Scope:        scope,
			BackendID:    bLease.ID,
			BackendOwner: bLease.Owner,
			Status:       ReconciliationStatusOrphaned,
		}

		if r.autoRemediateOrphans {
			_, relErr := r.backend.Release(ctx, bLease.ID, bLease.Owner, ReasonReleasedByOwner)
			if relErr != nil {
				item.Remediated = false
				item.Error = fmt.Sprintf("auto-remediation failed to release orphaned lease %s: %v", bLease.ID, relErr)
			} else {
				item.Remediated = true
				item.RemediationDetails = fmt.Sprintf("automatically released orphaned backend lease ID %s", bLease.ID)
				summary.RemediatedOrphans++
			}
		}

		summary.Orphaned++
		processedScopes[scope] = true
		items = append(items, item)
	}

	summary.TotalChecked = len(items)

	return &ReconciliationReport{
		Timestamp: now,
		Summary:   summary,
		Items:     items,
	}, nil
}

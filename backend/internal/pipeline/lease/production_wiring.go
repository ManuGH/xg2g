// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lease

import (
	"context"
	"fmt"
	"time"
)

// IntentTrackedTunerLeaseController wraps a TunerLeaseController and persists LeaseIntents
// to an IntentStore throughout the lease lifecycle (PENDING -> ACTIVE -> RELEASING -> TERMINAL).
type IntentTrackedTunerLeaseController struct {
	controller  TunerLeaseController
	intentStore IntentStore
}

// NewIntentTrackedTunerLeaseController creates a new IntentTrackedTunerLeaseController instance.
func NewIntentTrackedTunerLeaseController(controller TunerLeaseController, store IntentStore) (*IntentTrackedTunerLeaseController, error) {
	if controller == nil {
		return nil, fmt.Errorf("%w: tuner lease controller is required", ErrBindingUnavailable)
	}
	if store == nil {
		return nil, fmt.Errorf("%w: intent store is required", ErrInvalidIntent)
	}
	return &IntentTrackedTunerLeaseController{
		controller:  controller,
		intentStore: store,
	}, nil
}

// Acquire wraps tuner lease acquisition with intent lifecycle tracking.
func (c *IntentTrackedTunerLeaseController) Acquire(ctx context.Context, owner Owner, slot int, ttl time.Duration) (*TunerLeaseHandle, error) {
	scope := ScopeForTunerSlot(slot)
	intentID := ID(fmt.Sprintf("intent-%s-%s-%d", owner, scope, time.Now().UnixNano()))

	// 1. Persist PENDING intent before acquisition
	intent := LeaseIntent{
		IntentID:  intentID,
		Owner:     owner,
		Scope:     scope,
		State:     IntentStatePending,
		Revision:  1,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := c.intentStore.SaveIntent(ctx, intent); err != nil {
		return nil, fmt.Errorf("failed to persist PENDING intent: %w", err)
	}

	// 2. Perform underlying Acquire
	handle, err := c.controller.Acquire(ctx, owner, slot, ttl)
	if err != nil {
		// Mark intent TERMINAL on failure
		intent.State = IntentStateTerminal
		intent.Revision = 2
		intent.UpdatedAt = time.Now()
		_ = c.intentStore.SaveIntent(ctx, intent)
		return nil, err
	}

	// 3. Mark intent ACTIVE on acquisition success
	if handle != nil && handle.LeaseID != "" {
		intent.LeaseID = handle.LeaseID
		intent.State = IntentStateActive
		intent.Revision = 2
		intent.UpdatedAt = time.Now()
		if err := c.intentStore.SaveIntent(ctx, intent); err != nil {
			_ = c.controller.Release(ctx, handle, ReasonRollback)
			return nil, fmt.Errorf("failed to persist ACTIVE intent: %w", err)
		}
	}

	return handle, nil
}

// Renew delegates to the underlying controller.
func (c *IntentTrackedTunerLeaseController) Renew(ctx context.Context, handle *TunerLeaseHandle, ttl time.Duration) error {
	return c.controller.Renew(ctx, handle, ttl)
}

// Release updates the intent to RELEASING then TERMINAL upon release.
func (c *IntentTrackedTunerLeaseController) Release(ctx context.Context, handle *TunerLeaseHandle, reason ReasonCode) error {
	if handle != nil && handle.LeaseID != "" {
		intents, err := c.intentStore.ListIntents(ctx)
		if err == nil {
			for _, in := range intents {
				if in.LeaseID == handle.LeaseID && in.State == IntentStateActive {
					in.State = IntentStateReleasing
					in.Revision++
					in.UpdatedAt = time.Now()
					_ = c.intentStore.SaveIntent(ctx, in)

					relErr := c.controller.Release(ctx, handle, reason)

					in.State = IntentStateTerminal
					in.Revision++
					in.UpdatedAt = time.Now()
					_ = c.intentStore.SaveIntent(ctx, in)

					return relErr
				}
			}
		}
	}
	return c.controller.Release(ctx, handle, reason)
}

// RunStartupReconciliation executes a startup audit reconciliation check before normal work begins.
func RunStartupReconciliation(ctx context.Context, cfg ReconcilerConfig) (*ReconciliationReport, error) {
	reconciler, err := NewReconciler(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize startup reconciler: %w", err)
	}

	report, err := reconciler.Reconcile(ctx)
	if err != nil {
		return nil, fmt.Errorf("startup reconciliation failed: %w", err)
	}

	return report, nil
}

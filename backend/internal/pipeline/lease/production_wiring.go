// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lease

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// StartupGateState represents the mandatory gate status for lease acquisitions.
type StartupGateState string

const (
	GatePending  StartupGateState = "GATE_PENDING"
	GateAccepted StartupGateState = "GATE_ACCEPTED"
	GateRefused  StartupGateState = "GATE_REFUSED"
)

var (
	ErrStartupReconciliationRequired = errors.New("startup reconciliation required before acquiring leases")
	ErrStartupReconciliationFailed   = errors.New("startup reconciliation refused startup due to manual intervention requirement")
)

// IntentTrackedTunerLeaseController wraps a TunerLeaseController and persists LeaseIntents
// to an IntentStore throughout the lease lifecycle (PENDING -> ACTIVE -> RELEASING -> TERMINAL).
// It enforces a mandatory startup gate requiring successful reconciliation before accepting acquisitions.
type IntentTrackedTunerLeaseController struct {
	controller  TunerLeaseController
	intentStore IntentStore

	gateMu               sync.RWMutex
	gateState            StartupGateState
	reconciliationReport *ReconciliationReport
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
		gateState:   GatePending,
	}, nil
}

// ExecuteStartupReconciliation performs mandatory startup reconciliation audit and sets the gate state.
func (c *IntentTrackedTunerLeaseController) ExecuteStartupReconciliation(ctx context.Context, cfg ReconcilerConfig) (*ReconciliationReport, error) {
	c.gateMu.Lock()
	defer c.gateMu.Unlock()

	reconciler, err := NewReconciler(cfg)
	if err != nil {
		c.gateState = GateRefused
		return nil, fmt.Errorf("%w: failed to initialize reconciler: %v", ErrStartupReconciliationFailed, err)
	}

	report, err := reconciler.Reconcile(ctx)
	if err != nil {
		c.gateState = GateRefused
		return nil, fmt.Errorf("%w: reconciliation execution failed: %v", ErrStartupReconciliationFailed, err)
	}

	c.reconciliationReport = report
	if report.Summary.ManualInterventionRequired > 0 {
		c.gateState = GateRefused
		return report, fmt.Errorf("%w: %d items require manual intervention", ErrStartupReconciliationFailed, report.Summary.ManualInterventionRequired)
	}

	for _, comp := range report.Summary.Composites {
		if comp.Status == CompositeStatusBroken || comp.Status == ReconciliationStatusManualInterventionRequired {
			c.gateState = GateRefused
			return report, fmt.Errorf("%w: composite %s is broken", ErrStartupReconciliationFailed, comp.CompositeID)
		}
	}

	c.gateState = GateAccepted
	return report, nil
}

// GateState returns the current startup gate state.
func (c *IntentTrackedTunerLeaseController) GateState() StartupGateState {
	c.gateMu.RLock()
	defer c.gateMu.RUnlock()
	return c.gateState
}

// ReconciliationReport returns the report from startup reconciliation, if executed.
func (c *IntentTrackedTunerLeaseController) ReconciliationReport() *ReconciliationReport {
	c.gateMu.RLock()
	defer c.gateMu.RUnlock()
	return c.reconciliationReport
}

// Acquire wraps tuner lease acquisition with intent lifecycle tracking and mandatory startup gate enforcement.
func (c *IntentTrackedTunerLeaseController) Acquire(ctx context.Context, owner Owner, slot int, ttl time.Duration) (*TunerLeaseHandle, error) {
	c.gateMu.RLock()
	gState := c.gateState
	c.gateMu.RUnlock()

	if gState == GatePending {
		return nil, ErrStartupReconciliationRequired
	}
	if gState == GateRefused {
		return nil, ErrStartupReconciliationFailed
	}

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


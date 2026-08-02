// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lease

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

// StartupDecision represents the evaluation result of a StartupPolicy.
type StartupDecision string

const (
	StartupAccepted StartupDecision = "ACCEPTED"
	StartupRefused  StartupDecision = "REFUSED"
)

var (
	ErrStartupReconciliationRequired = errors.New("startup reconciliation required before acquiring leases")
	ErrStartupReconciliationFailed   = errors.New("startup reconciliation refused startup due to policy evaluation")
)

// StartupPolicyResult holds the decision and diagnostic reason for a startup policy evaluation.
type StartupPolicyResult struct {
	Decision StartupDecision
	Reason   string
}

// StartupPolicy evaluates a ReconciliationReport to decide whether startup can proceed.
type StartupPolicy interface {
	Evaluate(report ReconciliationReport) StartupPolicyResult
}

// DefaultStartupPolicy implements strict production startup safety rules.
type DefaultStartupPolicy struct {
	AllowAuditOnlyOrphans bool
}

// Evaluate applies strict production rules to a ReconciliationReport.
func (p DefaultStartupPolicy) Evaluate(report ReconciliationReport) StartupPolicyResult {
	if report.Summary.ManualInterventionRequired > 0 {
		return StartupPolicyResult{
			Decision: StartupRefused,
			Reason:   fmt.Sprintf("%d items require manual intervention", report.Summary.ManualInterventionRequired),
		}
	}

	if report.Summary.Missing > 0 {
		return StartupPolicyResult{
			Decision: StartupRefused,
			Reason:   fmt.Sprintf("%d active intents are missing backend leases", report.Summary.Missing),
		}
	}

	for _, comp := range report.Summary.Composites {
		if comp.Status == CompositeStatusBroken || comp.Status == ReconciliationStatusManualInterventionRequired {
			return StartupPolicyResult{
				Decision: StartupRefused,
				Reason:   fmt.Sprintf("composite %s is broken", comp.CompositeID),
			}
		}
	}

	for _, item := range report.Items {
		if item.RemediationOutcome == RemediationFailed {
			return StartupPolicyResult{
				Decision: StartupRefused,
				Reason:   fmt.Sprintf("remediation failed for scope %s: %s", item.Scope, item.Diagnostic),
			}
		}
	}

	if report.Summary.Orphaned > 0 && report.Summary.RemediatedOrphans == 0 && !p.AllowAuditOnlyOrphans {
		return StartupPolicyResult{
			Decision: StartupRefused,
			Reason:   fmt.Sprintf("%d orphaned backend leases detected in audit-only mode", report.Summary.Orphaned),
		}
	}

	return StartupPolicyResult{
		Decision: StartupAccepted,
		Reason:   "startup reconciliation approved cleanly",
	}
}

// IntentTrackedControllerConfig holds configuration options for IntentTrackedTunerLeaseController.
type IntentTrackedControllerConfig struct {
	Controller     TunerLeaseController
	IntentStore    IntentStore
	CleanupTimeout time.Duration
	Clock          func() time.Time
	IDGenerator    func() (ID, error)
	StartupPolicy  StartupPolicy
}

// IntentTrackedTunerLeaseController wraps a TunerLeaseController and persists LeaseIntents
// to an IntentStore throughout the lease lifecycle (PENDING -> ACTIVE -> RELEASING -> TERMINAL / RECOVERY_REQUIRED).
// It enforces a transactional Saga lifecycle and a mandatory startup gate requiring successful reconciliation before accepting acquisitions.
type IntentTrackedTunerLeaseController struct {
	controller     TunerLeaseController
	intentStore    IntentStore
	cleanupTimeout time.Duration
	nowFunc        func() time.Time
	idGen          func() (ID, error)
	startupPolicy  StartupPolicy

	gateMu               sync.RWMutex
	gateState            StartupGateState
	reconciliationReport *ReconciliationReport
}

// NewIntentTrackedTunerLeaseController creates a new IntentTrackedTunerLeaseController instance using positional arguments.
func NewIntentTrackedTunerLeaseController(controller TunerLeaseController, store IntentStore) (*IntentTrackedTunerLeaseController, error) {
	return NewIntentTrackedTunerLeaseControllerWithConfig(IntentTrackedControllerConfig{
		Controller:  controller,
		IntentStore: store,
	})
}

// NewIntentTrackedTunerLeaseControllerWithConfig creates a new IntentTrackedTunerLeaseController instance using a config struct.
func NewIntentTrackedTunerLeaseControllerWithConfig(cfg IntentTrackedControllerConfig) (*IntentTrackedTunerLeaseController, error) {
	if cfg.Controller == nil {
		return nil, fmt.Errorf("%w: tuner lease controller is required", ErrBindingUnavailable)
	}
	if cfg.IntentStore == nil {
		return nil, fmt.Errorf("%w: intent store is required", ErrInvalidIntent)
	}
	cleanupTimeout := cfg.CleanupTimeout
	if cleanupTimeout <= 0 {
		cleanupTimeout = DefaultCleanupTimeout
	}
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	idGen := cfg.IDGenerator
	if idGen == nil {
		idGen = defaultCryptographicIDGen
	}
	policy := cfg.StartupPolicy
	if policy == nil {
		policy = DefaultStartupPolicy{AllowAuditOnlyOrphans: false}
	}
	return &IntentTrackedTunerLeaseController{
		controller:     cfg.Controller,
		intentStore:    cfg.IntentStore,
		cleanupTimeout: cleanupTimeout,
		nowFunc:        clock,
		idGen:          idGen,
		startupPolicy:  policy,
		gateState:      GatePending,
	}, nil
}

func defaultCryptographicIDGen() (ID, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random intent id: %w", err)
	}
	return ID("intent-" + hex.EncodeToString(b)), nil
}

// ExecuteStartupReconciliation performs mandatory startup reconciliation audit and sets the gate state idempotently.
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

	c.reconciliationReport = cloneReconciliationReport(report)
	result := c.startupPolicy.Evaluate(*report)
	if result.Decision == StartupRefused {
		c.gateState = GateRefused
		return cloneReconciliationReport(report), fmt.Errorf("%w: %s", ErrStartupReconciliationFailed, result.Reason)
	}

	c.gateState = GateAccepted
	return cloneReconciliationReport(report), nil
}

// GateState returns the current startup gate state.
func (c *IntentTrackedTunerLeaseController) GateState() StartupGateState {
	c.gateMu.RLock()
	defer c.gateMu.RUnlock()
	return c.gateState
}

// ReconciliationReport returns a deep-copy snapshot of the startup reconciliation report.
func (c *IntentTrackedTunerLeaseController) ReconciliationReport() *ReconciliationReport {
	c.gateMu.RLock()
	defer c.gateMu.RUnlock()
	return cloneReconciliationReport(c.reconciliationReport)
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
	genID, err := c.idGen()
	if err != nil {
		return nil, fmt.Errorf("failed to generate intent ID: %w", err)
	}
	now := c.nowFunc()

	// 1. Persist PENDING intent before acquisition
	intent := LeaseIntent{
		IntentID:  genID,
		Owner:     owner,
		Scope:     scope,
		State:     IntentStatePending,
		Revision:  1,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if saveErr := c.intentStore.SaveIntent(ctx, intent); saveErr != nil {
		return nil, fmt.Errorf("failed to persist PENDING intent: %w", saveErr)
	}

	// 2. Perform underlying Acquire
	handle, acqErr := c.controller.Acquire(ctx, owner, slot, ttl)
	if acqErr != nil {
		// Detached context compensation to persist TERMINAL intent
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(nonNilContext(ctx)), c.cleanupTimeout)
		defer cancel()

		intent.State = IntentStateTerminal
		intent.Revision = 2
		intent.UpdatedAt = c.nowFunc()
		if saveErr := c.intentStore.SaveIntent(cleanupCtx, intent); saveErr != nil {
			return nil, errors.Join(fmt.Errorf("acquire tuner lease failed: %w", acqErr), fmt.Errorf("failed to persist TERMINAL intent after acquire failure: %w", saveErr))
		}
		return nil, acqErr
	}

	// 3. Mark intent ACTIVE on acquisition success
	if handle != nil && handle.LeaseID != "" {
		intent.LeaseID = handle.LeaseID
		intent.State = IntentStateActive
		intent.Revision = 2
		intent.UpdatedAt = c.nowFunc()
		if saveErr := c.intentStore.SaveIntent(ctx, intent); saveErr != nil {
			// Transactional rollback with detached context
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(nonNilContext(ctx)), c.cleanupTimeout)
			defer cancel()

			relErr := c.controller.Release(cleanupCtx, handle, ReasonRollback)

			intent.State = IntentStateRecoveryRequired
			intent.Revision = 3
			intent.UpdatedAt = c.nowFunc()
			_ = c.intentStore.SaveIntent(cleanupCtx, intent)

			return nil, errors.Join(fmt.Errorf("failed to persist ACTIVE intent: %w", saveErr), fmt.Errorf("rollback release result: %v", relErr))
		}
	}

	return handle, nil
}

// Renew delegates to the underlying controller and marks RECOVERY_REQUIRED if renew fails.
func (c *IntentTrackedTunerLeaseController) Renew(ctx context.Context, handle *TunerLeaseHandle, ttl time.Duration) error {
	renewErr := c.controller.Renew(ctx, handle, ttl)
	if renewErr != nil && handle != nil && handle.LeaseID != "" {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(nonNilContext(ctx)), c.cleanupTimeout)
		defer cancel()

		intents, err := c.intentStore.ListIntents(cleanupCtx)
		if err == nil {
			for _, in := range intents {
				if in.LeaseID == handle.LeaseID && in.State == IntentStateActive {
					in.State = IntentStateRecoveryRequired
					in.Revision++
					in.UpdatedAt = c.nowFunc()
					_ = c.intentStore.SaveIntent(cleanupCtx, in)
					break
				}
			}
		}
	}
	return renewErr
}

// Release updates the intent to RELEASING then TERMINAL upon confirmed release.
func (c *IntentTrackedTunerLeaseController) Release(ctx context.Context, handle *TunerLeaseHandle, reason ReasonCode) error {
	if handle == nil || handle.LeaseID == "" {
		return c.controller.Release(ctx, handle, reason)
	}

	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(nonNilContext(ctx)), c.cleanupTimeout)
	defer cancel()

	intents, err := c.intentStore.ListIntents(ctx)
	var activeIntent *LeaseIntent
	if err == nil {
		for _, in := range intents {
			if in.LeaseID == handle.LeaseID && in.State == IntentStateActive {
				inCopy := in
				activeIntent = &inCopy
				break
			}
		}
	}

	if activeIntent != nil {
		activeIntent.State = IntentStateReleasing
		activeIntent.Revision++
		activeIntent.UpdatedAt = c.nowFunc()
		_ = c.intentStore.SaveIntent(ctx, *activeIntent)
	}

	relErr := c.controller.Release(ctx, handle, reason)
	if relErr != nil {
		if activeIntent != nil {
			activeIntent.State = IntentStateRecoveryRequired
			activeIntent.Revision++
			activeIntent.UpdatedAt = c.nowFunc()
			_ = c.intentStore.SaveIntent(cleanupCtx, *activeIntent)
		}
		return relErr
	}

	// ONLY persist TERMINAL after confirmed successful backend release!
	if activeIntent != nil {
		activeIntent.State = IntentStateTerminal
		activeIntent.Revision++
		activeIntent.UpdatedAt = c.nowFunc()
		if saveErr := c.intentStore.SaveIntent(cleanupCtx, *activeIntent); saveErr != nil {
			return fmt.Errorf("failed to persist TERMINAL intent after backend release: %w", saveErr)
		}
	}

	return nil
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

	return cloneReconciliationReport(report), nil
}



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

	"github.com/ManuGH/xg2g/internal/pipeline/policy"
)

// StartupGateState represents the mandatory gate status for lease acquisitions.
type StartupGateState string

const (
	GatePending  StartupGateState = "GATE_PENDING"
	GateRunning  StartupGateState = "GATE_RUNNING"
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
	ErrStartupGateAlreadyResolved    = errors.New("startup gate has already been resolved and cannot be re-evaluated")
	ErrTrackedIntentNotFound         = errors.New("tracked lease intent not found")
	ErrReconciliationRequired        = errors.New("reconciliation required due to ambiguous backend state")
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
		if item.IntentState == IntentStateRecoveryRequired {
			return StartupPolicyResult{
				Decision: StartupRefused,
				Reason:   fmt.Sprintf("intent for scope %s is in RECOVERY_REQUIRED state", item.Scope),
			}
		}
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
// If the gate has already been resolved, it returns ErrStartupGateAlreadyResolved.
func (c *IntentTrackedTunerLeaseController) ExecuteStartupReconciliation(ctx context.Context, cfg ReconcilerConfig) (*ReconciliationReport, error) {
	c.gateMu.Lock()
	if c.gateState != GatePending {
		c.gateMu.Unlock()
		return nil, ErrStartupGateAlreadyResolved
	}
	c.gateState = GateRunning
	c.gateMu.Unlock()

	reconciler, err := NewReconciler(cfg)
	if err != nil {
		c.gateMu.Lock()
		c.gateState = GateRefused
		c.gateMu.Unlock()
		return nil, fmt.Errorf("%w: failed to initialize reconciler: %v", ErrStartupReconciliationFailed, err)
	}

	report, err := reconciler.Reconcile(ctx)
	if err != nil {
		c.gateMu.Lock()
		c.gateState = GateRefused
		c.gateMu.Unlock()
		return nil, fmt.Errorf("%w: reconciliation execution failed: %v", ErrStartupReconciliationFailed, err)
	}

	c.gateMu.Lock()
	defer c.gateMu.Unlock()

	if c.gateState != GateRunning {
		return nil, ErrStartupGateAlreadyResolved
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

	if gState == GatePending || gState == GateRunning {
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
		saveErr := c.intentStore.SaveIntent(cleanupCtx, intent)

		var errs []error
		errs = append(errs, acqErr)
		if saveErr != nil {
			errs = append(errs, fmt.Errorf("failed to mark intent TERMINAL after acquire failure: %w", saveErr))
		}
		return nil, errors.Join(errs...)
	}

	// 2b. Validate underlying controller result
	if handle == nil || handle.LeaseID == "" {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(nonNilContext(ctx)), c.cleanupTimeout)
		defer cancel()

		intent.State = IntentStateRecoveryRequired
		intent.Revision = 2
		intent.UpdatedAt = c.nowFunc()
		recoverySaveErr := c.intentStore.SaveIntent(cleanupCtx, intent)

		var errs []error
		errs = append(errs, ErrInvalidBackendResult, ErrReconciliationRequired)
		if recoverySaveErr != nil {
			errs = append(errs, fmt.Errorf("persist RECOVERY_REQUIRED after invalid backend result: %w", recoverySaveErr))
		}
		return nil, errors.Join(errs...)
	}

	// 3. Mark intent ACTIVE on acquisition success
	intent.LeaseID = handle.LeaseID
	intent.State = IntentStateActive
	intent.Revision = 2
	intent.UpdatedAt = c.nowFunc()
	if saveErr := c.intentStore.SaveIntent(ctx, intent); saveErr != nil {
		// Transactional rollback with detached context
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(nonNilContext(ctx)), c.cleanupTimeout)
		defer cancel()

		relErr := c.controller.Release(cleanupCtx, handle, ReasonRollback)

		var recoverySaveErr error
		if storedIntent, getErr := c.intentStore.GetIntent(cleanupCtx, intent.IntentID); getErr == nil && storedIntent != nil {
			recIntent := *storedIntent
			recIntent.State = IntentStateRecoveryRequired
			recIntent.Revision++
			recIntent.UpdatedAt = c.nowFunc()
			recoverySaveErr = c.intentStore.SaveIntent(cleanupCtx, recIntent)
		} else {
			intent.State = IntentStateRecoveryRequired
			intent.Revision = 2
			intent.UpdatedAt = c.nowFunc()
			recoverySaveErr = c.intentStore.SaveIntent(cleanupCtx, intent)
		}

		var errs []error
		errs = append(errs, fmt.Errorf("failed to persist ACTIVE intent: %w", saveErr))
		if relErr != nil {
			errs = append(errs, fmt.Errorf("rollback release: %w", relErr))
		}
		if recoverySaveErr != nil {
			errs = append(errs, fmt.Errorf("persist RECOVERY_REQUIRED: %w", recoverySaveErr))
		}
		return nil, errors.Join(errs...)
	}

	return handle, nil
}

func (c *IntentTrackedTunerLeaseController) AcquireWithBoundTicket(ctx context.Context, ticket *policy.AdmissionTicket, sessionID, userID, profileID string, owner Owner, slot int, ttl time.Duration) (*TunerLeaseHandle, error) {
	if ticket != nil {
		if err := policy.ValidateBoundTicket(ticket, sessionID, userID, profileID, "tuner"); err != nil {
			return nil, fmt.Errorf("tuner lease ticket validation failed: %w", err)
		}
	}
	return c.Acquire(ctx, owner, slot, ttl)
}

// Renew delegates to the underlying controller and marks RECOVERY_REQUIRED if renew fails.
func (c *IntentTrackedTunerLeaseController) Renew(ctx context.Context, handle *TunerLeaseHandle, ttl time.Duration) error {
	renewErr := c.controller.Renew(ctx, handle, ttl)
	if renewErr != nil {
		if handle == nil || handle.LeaseID == "" {
			return errors.Join(renewErr, ErrTrackedIntentNotFound)
		}

		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(nonNilContext(ctx)), c.cleanupTimeout)
		defer cancel()

		intents, lookupErr := c.intentStore.ListIntents(cleanupCtx)
		var recoverySaveErr error
		foundIntent := false
		if lookupErr == nil {
			for _, in := range intents {
				if in.LeaseID == handle.LeaseID && in.State == IntentStateActive {
					foundIntent = true
					in.State = IntentStateRecoveryRequired
					in.Revision++
					in.UpdatedAt = c.nowFunc()
					recoverySaveErr = c.intentStore.SaveIntent(cleanupCtx, in)
					break
				}
			}
		}

		var errs []error
		errs = append(errs, renewErr)
		if lookupErr != nil {
			errs = append(errs, fmt.Errorf("list intents during renew recovery: %w", lookupErr))
		} else if !foundIntent {
			errs = append(errs, fmt.Errorf("%w: lease_id %s", ErrTrackedIntentNotFound, handle.LeaseID))
		}
		if recoverySaveErr != nil {
			errs = append(errs, fmt.Errorf("persist RECOVERY_REQUIRED on renew failure: %w", recoverySaveErr))
		}
		return errors.Join(errs...)
	}
	return nil
}

// Release updates the intent to RELEASING then TERMINAL upon confirmed release using a detached cleanup context.
func (c *IntentTrackedTunerLeaseController) Release(ctx context.Context, handle *TunerLeaseHandle, reason ReasonCode) error {
	if handle == nil || handle.LeaseID == "" {
		return fmt.Errorf("%w: release handle is empty", ErrTrackedIntentNotFound)
	}

	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(nonNilContext(ctx)), c.cleanupTimeout)
	defer cancel()

	intents, lookupErr := c.intentStore.ListIntents(cleanupCtx)
	if lookupErr != nil {
		// CRITICAL GATE: If ListIntents fails, DO NOT mutate backend!
		return fmt.Errorf("lookup intent before release: %w", lookupErr)
	}

	var activeIntent *LeaseIntent
	for _, in := range intents {
		if in.LeaseID == handle.LeaseID && (in.State == IntentStateActive || in.State == IntentStateReleasing) {
			inCopy := in
			activeIntent = &inCopy
			break
		}
	}

	if activeIntent == nil {
		// CRITICAL GATE: If no active or releasing intent exists, DO NOT mutate backend!
		return fmt.Errorf("%w: lease_id %s", ErrTrackedIntentNotFound, handle.LeaseID)
	}

	// 1. Persist RELEASING state before mutating backend
	activeIntent.State = IntentStateReleasing
	activeIntent.Revision++
	activeIntent.UpdatedAt = c.nowFunc()
	if releasingSaveErr := c.intentStore.SaveIntent(cleanupCtx, *activeIntent); releasingSaveErr != nil {
		// CRITICAL GATE: If RELEASING save fails, DO NOT mutate backend!
		return fmt.Errorf("failed to persist RELEASING intent prior to backend mutation: %w", releasingSaveErr)
	}

	// 2. Perform backend release
	relErr := c.controller.Release(cleanupCtx, handle, reason)
	if relErr != nil {
		activeIntent.State = IntentStateRecoveryRequired
		activeIntent.Revision++
		activeIntent.UpdatedAt = c.nowFunc()
		recoverySaveErr := c.intentStore.SaveIntent(cleanupCtx, *activeIntent)
		var errs []error
		errs = append(errs, relErr)
		if recoverySaveErr != nil {
			errs = append(errs, fmt.Errorf("persist RECOVERY_REQUIRED on backend release failure: %w", recoverySaveErr))
		}
		return errors.Join(errs...)
	}

	// 3. Backend release confirmed success -> Persist TERMINAL state
	activeIntent.State = IntentStateTerminal
	activeIntent.Revision++
	activeIntent.UpdatedAt = c.nowFunc()
	if terminalSaveErr := c.intentStore.SaveIntent(cleanupCtx, *activeIntent); terminalSaveErr != nil {
		var recoverySaveErr error
		if storedIntent, getErr := c.intentStore.GetIntent(cleanupCtx, activeIntent.IntentID); getErr == nil && storedIntent != nil {
			recIntent := *storedIntent
			recIntent.State = IntentStateRecoveryRequired
			recIntent.Revision++
			recIntent.UpdatedAt = c.nowFunc()
			recoverySaveErr = c.intentStore.SaveIntent(cleanupCtx, recIntent)
		} else {
			activeIntent.State = IntentStateRecoveryRequired
			activeIntent.Revision++
			activeIntent.UpdatedAt = c.nowFunc()
			recoverySaveErr = c.intentStore.SaveIntent(cleanupCtx, *activeIntent)
		}

		var errs []error
		errs = append(errs, fmt.Errorf("failed to persist TERMINAL intent after backend release: %w", terminalSaveErr))
		if recoverySaveErr != nil {
			errs = append(errs, fmt.Errorf("persist RECOVERY_REQUIRED after terminal save failure: %w", recoverySaveErr))
		}
		return errors.Join(errs...)
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

// SessionStoreObservableBackend adapts SessionStoreTunerLeaseController to ObservableLeaseBackend.
type SessionStoreObservableBackend struct {
	*SessionStoreTunerLeaseController
}

func (b SessionStoreObservableBackend) ListLeases(ctx context.Context) ([]Lease, error) {
	if b.SessionStoreTunerLeaseController == nil {
		return nil, ErrBindingUnavailable
	}
	return b.SessionStoreTunerLeaseController.ListLeases(ctx)
}

func (b SessionStoreObservableBackend) Acquire(ctx context.Context, owner Owner, scope Scope, ttl time.Duration) (*Lease, error) {
	if b.SessionStoreTunerLeaseController == nil || b.Store == nil {
		return nil, ErrBindingUnavailable
	}
	l, ok, err := b.Store.TryAcquireLease(ctx, string(scope), string(owner), ttl)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrScopeConflict
	}
	return &Lease{
		ID:         ID(l.Key()),
		Owner:      Owner(l.Owner()),
		Scope:      Scope(l.Key()),
		State:      StateAcquired,
		ExpiresAt:  l.ExpiresAt(),
		ReasonCode: ReasonAcquired,
	}, nil
}

func (b SessionStoreObservableBackend) Renew(ctx context.Context, id ID, owner Owner, ttl time.Duration) (*Lease, error) {
	if b.SessionStoreTunerLeaseController == nil || b.Store == nil {
		return nil, ErrBindingUnavailable
	}
	l, ok, err := b.Store.RenewLease(ctx, string(id), string(owner), ttl)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrLeaseInactive
	}
	return &Lease{
		ID:         ID(l.Key()),
		Owner:      Owner(l.Owner()),
		Scope:      Scope(l.Key()),
		State:      StateAcquired,
		ExpiresAt:  l.ExpiresAt(),
		ReasonCode: ReasonAcquired,
	}, nil
}

func (b SessionStoreObservableBackend) Release(ctx context.Context, id ID, owner Owner, reason ReasonCode) (*Lease, error) {
	if b.SessionStoreTunerLeaseController == nil || b.Store == nil {
		return nil, ErrBindingUnavailable
	}
	err := b.Store.ReleaseLease(ctx, string(id), string(owner))
	if err != nil {
		return nil, err
	}
	return &Lease{
		ID:         id,
		Owner:      owner,
		Scope:      Scope(id),
		State:      StateReleased,
		ReasonCode: reason,
	}, nil
}

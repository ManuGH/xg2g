// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lease

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// TunerLeaseHandle represents the active lease token held by a session/worker.
type TunerLeaseHandle struct {
	LeaseID LeaseID `json:"lease_id"`
	Owner   Owner   `json:"owner"`
	Slot    int     `json:"slot"`
	Scope   Scope   `json:"scope"`
}

type LeaseID = ID

// TunerLeaseController defines the contract for controlling tuner slot leases.
type TunerLeaseController interface {
	Acquire(ctx context.Context, owner Owner, slot int, ttl time.Duration) (*TunerLeaseHandle, error)
	Renew(ctx context.Context, handle *TunerLeaseHandle, ttl time.Duration) error
	Release(ctx context.Context, handle *TunerLeaseHandle, reason ReasonCode) error
}

// RenewalScheduler abstracts periodic ticker scheduling for deterministic testing without real time.Sleep.
type RenewalScheduler interface {
	C() <-chan time.Time
	Stop()
}

// RealRenewalScheduler is the production implementation of RenewalScheduler backed by time.Ticker.
type RealRenewalScheduler struct {
	ticker *time.Ticker
}

func NewRealRenewalScheduler(d time.Duration) *RealRenewalScheduler {
	return &RealRenewalScheduler{ticker: time.NewTicker(d)}
}

func (r *RealRenewalScheduler) C() <-chan time.Time {
	return r.ticker.C
}

func (r *RealRenewalScheduler) Stop() {
	if r.ticker != nil {
		r.ticker.Stop()
	}
}

// TunerBindingController implements TunerLeaseController wrapping TunerBinding.
type TunerBindingController struct {
	tb *TunerBinding
}

// NewTunerBindingController creates a TunerLeaseController backed by TunerBinding.
func NewTunerBindingController(tb *TunerBinding) *TunerBindingController {
	return &TunerBindingController{tb: tb}
}

func (c *TunerBindingController) Acquire(ctx context.Context, owner Owner, slot int, ttl time.Duration) (*TunerLeaseHandle, error) {
	if c == nil || c.tb == nil {
		return nil, ErrBindingUnavailable
	}
	l, err := c.tb.AcquireTunerSlot(ctx, owner, slot, ttl)
	if err != nil {
		return nil, err
	}
	return &TunerLeaseHandle{
		LeaseID: l.ID,
		Owner:   l.Owner,
		Slot:    slot,
		Scope:   l.Scope,
	}, nil
}

func (c *TunerBindingController) Renew(ctx context.Context, handle *TunerLeaseHandle, ttl time.Duration) error {
	if c == nil || c.tb == nil {
		return ErrBindingUnavailable
	}
	if handle == nil || handle.LeaseID == "" {
		return ErrNotFound
	}
	_, err := c.tb.RenewTunerSlot(ctx, handle.LeaseID, handle.Owner, ttl)
	return err
}

func (c *TunerBindingController) Release(ctx context.Context, handle *TunerLeaseHandle, reason ReasonCode) error {
	if c == nil || c.tb == nil {
		return ErrBindingUnavailable
	}
	if handle == nil || handle.LeaseID == "" {
		return nil
	}
	_, err := c.tb.ReleaseTunerSlot(ctx, handle.LeaseID, handle.Owner, reason)
	return err
}

// RunnerConfig configures TunerLifecycleRunner with explicit validations.
type RunnerConfig struct {
	Controller     TunerLeaseController
	TTL            time.Duration
	RenewInterval  time.Duration
	CleanupTimeout time.Duration
	SchedulerFunc  func(d time.Duration) RenewalScheduler
}

// TunerLifecycleRunner orchestrates full active session/worker tuner leases,
// bounded renewal loops, startup failure compensation, and cancellation.
type TunerLifecycleRunner struct {
	controller     TunerLeaseController
	TTL            time.Duration
	RenewInterval  time.Duration
	CleanupTimeout time.Duration
	schedulerFunc  func(d time.Duration) RenewalScheduler
}

// NewTunerLifecycleRunner creates a TunerLifecycleRunner with validated parameters.
func NewTunerLifecycleRunner(cfg RunnerConfig) (*TunerLifecycleRunner, error) {
	if cfg.Controller == nil {
		return nil, fmt.Errorf("%w: controller must not be nil", ErrBindingUnavailable)
	}
	if cfg.TTL <= 0 {
		return nil, fmt.Errorf("%w: TTL must be greater than 0", ErrInvalidTTL)
	}
	if cfg.RenewInterval <= 0 {
		return nil, fmt.Errorf("%w: renew interval must be greater than 0", ErrInvalidTTL)
	}
	if cfg.RenewInterval >= cfg.TTL {
		return nil, fmt.Errorf("%w: renew interval (%v) must be strictly less than TTL (%v)", ErrInvalidTTL, cfg.RenewInterval, cfg.TTL)
	}
	if cfg.CleanupTimeout <= 0 {
		return nil, fmt.Errorf("%w: cleanup timeout must be greater than 0", ErrInvalidTTL)
	}

	schedulerFunc := cfg.SchedulerFunc
	if schedulerFunc == nil {
		schedulerFunc = func(d time.Duration) RenewalScheduler {
			return NewRealRenewalScheduler(d)
		}
	}

	return &TunerLifecycleRunner{
		controller:     cfg.Controller,
		TTL:            cfg.TTL,
		RenewInterval:  cfg.RenewInterval,
		CleanupTimeout: cfg.CleanupTimeout,
		schedulerFunc:  schedulerFunc,
	}, nil
}

// RunSession manages the complete tuner session lifecycle:
// 1. Checks requiresTuner. If false, bypasses tuner lease completely.
// 2. Acquires tuner lease BEFORE any hardware zap or stream start. If ErrScopeConflict, zero hardware operations occur.
// 3. Executes tuneFn (Zap / Readiness). If tuneFn fails, performs compensatory lease release with errors.Join.
// 4. Executes runFn (active streaming / FFmpeg) while launching a background renewal ticker.
// 5. If renewal fails or lease is revoked/expired, cancels runFn context immediately.
// 6. Releases lease idempotently upon termination using a detached bounded context and preserves explicit reason code.
func (r *TunerLifecycleRunner) RunSession(
	parentCtx context.Context,
	owner Owner,
	slot int,
	requiresTuner bool,
	tuneFn func(ctx context.Context) error,
	runFn func(ctx context.Context) error,
) error {
	if parentCtx == nil {
		parentCtx = context.Background()
	}

	// Non-tuner bound workloads bypass tuner lease completely
	if !requiresTuner {
		if runFn != nil {
			return runFn(parentCtx)
		}
		return nil
	}

	if r == nil || r.controller == nil {
		return ErrBindingUnavailable
	}

	// 1. Acquire Tuner Lease BEFORE any hardware operation
	handle, err := r.controller.Acquire(parentCtx, owner, slot, r.TTL)
	if err != nil {
		// ErrScopeConflict or other acquire errors: ZERO hardware operations occur
		return err
	}

	// Helper for detached bounded cleanup returning any release error
	releaseCleanup := func(reason ReasonCode) error {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(parentCtx), r.CleanupTimeout)
		defer cancel()
		return r.controller.Release(cleanupCtx, handle, reason)
	}

	// 2. Execute Tune (Zap / Readiness) with compensation on failure
	if tuneFn != nil {
		if err := tuneFn(parentCtx); err != nil {
			relErr := releaseCleanup(ReasonReleasedByOwner)
			return errors.Join(fmt.Errorf("tuner prep failed: %w", err), relErr)
		}
	}

	// Check if context was canceled during tune
	if parentCtx.Err() != nil {
		relErr := releaseCleanup(ReasonReleasedByOwner)
		return errors.Join(parentCtx.Err(), relErr)
	}

	// 3. Active Usage with Bounded Renewal Loop
	sessionCtx, cancelSession := context.WithCancel(parentCtx)

	renewDone := make(chan struct{})
	var renewErr error

	go func() {
		defer close(renewDone)
		scheduler := r.schedulerFunc(r.RenewInterval)
		defer scheduler.Stop()

		for {
			select {
			case <-sessionCtx.Done():
				return
			case <-scheduler.C():
				if err := r.controller.Renew(sessionCtx, handle, r.TTL); err != nil {
					renewErr = err
					cancelSession() // Revoke / Expire / Error: abort active usage context immediately!
					return
				}
			}
		}
	}()

	var runErr error
	if runFn != nil {
		runErr = runFn(sessionCtx)
	}

	cancelSession()
	<-renewDone

	// Determine final release reason
	releaseReason := ReasonReleasedByOwner
	if renewErr != nil {
		if errors.Is(renewErr, ErrLeaseInactive) {
			releaseReason = ReasonExpired
		} else {
			releaseReason = ReasonPreempted
		}
	}

	releaseErr := releaseCleanup(releaseReason)

	if runErr != nil || renewErr != nil || releaseErr != nil {
		var errs []error
		if runErr != nil {
			errs = append(errs, runErr)
		}
		if renewErr != nil {
			errs = append(errs, fmt.Errorf("tuner lease lost during active session: %w", renewErr))
		}
		if releaseErr != nil {
			errs = append(errs, fmt.Errorf("tuner lease release failed: %w", releaseErr))
		}
		return errors.Join(errs...)
	}

	return nil
}

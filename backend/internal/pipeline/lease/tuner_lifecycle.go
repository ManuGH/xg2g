// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lease

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

// SourceClassifier determines whether a given stream URL requires a hardware tuner lease.
type SourceClassifier interface {
	IsTunerBound(sourceURL string) bool
}

// DefaultSourceClassifier checks whether a source URL requires hardware tuner allocation.
type DefaultSourceClassifier struct{}

// IsTunerBound returns true if the URL represents a tuner-dependent Enigma2 live service,
// and false for direct HTTP streams, VOD files, file:// paths, or recorded assets.
func (d DefaultSourceClassifier) IsTunerBound(sourceURL string) bool {
	s := strings.TrimSpace(sourceURL)
	if s == "" {
		return false
	}
	sLower := strings.ToLower(s)
	if strings.HasPrefix(sLower, "file://") {
		return false
	}
	if strings.HasSuffix(sLower, ".mp4") || strings.HasSuffix(sLower, ".mkv") || strings.Contains(sLower, "/file/") || strings.Contains(sLower, "/vod/") {
		return false
	}
	// Enigma2 live service references contain colons ("1:0:"), /web/zap, /web/stream, or TS stream ports 8001/8002
	if strings.Contains(s, "1:0:") || strings.Contains(sLower, "/web/zap") || strings.Contains(sLower, "/web/stream") || strings.Contains(sLower, ":8001") || strings.Contains(sLower, ":8002") {
		return true
	}
	return false
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
	_, err := c.tb.RenewTunerSlot(handle.LeaseID, handle.Owner, ttl)
	return err
}

func (c *TunerBindingController) Release(ctx context.Context, handle *TunerLeaseHandle, reason ReasonCode) error {
	if c == nil || c.tb == nil {
		return ErrBindingUnavailable
	}
	if handle == nil || handle.LeaseID == "" {
		return nil
	}
	_, err := c.tb.ReleaseTunerSlot(handle.LeaseID, handle.Owner)
	return err
}

// TunerLifecycleRunner orchestrates full active session/worker tuner leases,
// bounded renewal loops, startup failure compensation, and cancellation.
type TunerLifecycleRunner struct {
	controller TunerLeaseController
	classifier SourceClassifier

	TTL           time.Duration
	RenewInterval time.Duration
	CleanupTimeout time.Duration
	Clock         func() time.Time
}

// NewTunerLifecycleRunner creates a TunerLifecycleRunner.
func NewTunerLifecycleRunner(controller TunerLeaseController, classifier SourceClassifier) *TunerLifecycleRunner {
	if classifier == nil {
		classifier = DefaultSourceClassifier{}
	}
	return &TunerLifecycleRunner{
		controller:    controller,
		classifier:    classifier,
		TTL:           30 * time.Second,
		RenewInterval: 10 * time.Second,
		CleanupTimeout: 3 * time.Second,
		Clock:         time.Now,
	}
}

func (r *TunerLifecycleRunner) now() time.Time {
	if r.Clock != nil {
		return r.Clock()
	}
	return time.Now()
}

// RunSession manages the complete tuner session lifecycle:
// 1. Checks if sourceURL requires a tuner lease. If false, bypasses tuner lease.
// 2. Acquires tuner lease BEFORE any hardware zap or stream start. If ErrScopeConflict, zero hardware operations occur.
// 3. Executes tuneFn (Zap / Readiness). If tuneFn fails, performs compensatory lease release.
// 4. Executes runFn (active streaming / FFmpeg) while launching a background renewal ticker.
// 5. If renewal fails or lease is revoked/expired, cancels runFn context immediately.
// 6. Releases lease idempotently upon termination using a detached bounded context.
func (r *TunerLifecycleRunner) RunSession(
	parentCtx context.Context,
	owner Owner,
	slot int,
	sourceURL string,
	tuneFn func(ctx context.Context) error,
	runFn func(ctx context.Context) error,
) error {
	if parentCtx == nil {
		parentCtx = context.Background()
	}

	// Non-tuner bound sources bypass tuner lease completely
	if !r.classifier.IsTunerBound(sourceURL) {
		if runFn != nil {
			return runFn(parentCtx)
		}
		return nil
	}

	if r.controller == nil {
		return ErrBindingUnavailable
	}

	// 1. Acquire Tuner Lease BEFORE any hardware operation
	handle, err := r.controller.Acquire(parentCtx, owner, slot, r.TTL)
	if err != nil {
		// ErrScopeConflict or other acquire errors: ZERO hardware operations occur
		return err
	}

	// Helper for detached bounded cleanup
	releaseCleanup := func(reason ReasonCode) {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(parentCtx), r.CleanupTimeout)
		defer cancel()
		_ = r.controller.Release(cleanupCtx, handle, reason)
	}

	// 2. Execute Tune (Zap / Readiness) with compensation on failure
	if tuneFn != nil {
		if err := tuneFn(parentCtx); err != nil {
			releaseCleanup(ReasonReleasedByOwner)
			return fmt.Errorf("tuner prep failed: %w", err)
		}
	}

	// Check if context was canceled during tune
	if parentCtx.Err() != nil {
		releaseCleanup(ReasonReleasedByOwner)
		return parentCtx.Err()
	}

	// 3. Active Usage with Bounded Renewal Loop
	sessionCtx, cancelSession := context.WithCancel(parentCtx)
	defer cancelSession()

	renewDone := make(chan struct{})
	var renewErr error

	go func() {
		defer close(renewDone)
		ticker := time.NewTicker(r.RenewInterval)
		defer ticker.Stop()

		for {
			select {
			case <-sessionCtx.Done():
				return
			case <-ticker.C:
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

	releaseCleanup(releaseReason)

	if runErr != nil {
		return runErr
	}
	if renewErr != nil {
		return fmt.Errorf("tuner lease lost during active session: %w", renewErr)
	}
	return nil
}

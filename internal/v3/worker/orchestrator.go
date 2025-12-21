//go:build v3
// +build v3

// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package worker

import (
	"context"
	"errors"
	"time"

	"github.com/ManuGH/xg2g/internal/v3/bus"
	"github.com/ManuGH/xg2g/internal/v3/model"
	"github.com/ManuGH/xg2g/internal/v3/store"
)

// Orchestrator consumes intents and drives pipelines. It is intentionally side-effecting,
// and MUST be out-of-band from HTTP request paths.
//
// MVP:
//   - acquire a single-writer lease per serviceKey
//   - transition Session: STARTING -> READY/FAILED
//   - (placeholder) for receiver tuning + ffmpeg lifecycle
type Orchestrator struct {
	Store store.StateStore
	Bus   bus.Bus

	LeaseTTL       time.Duration
	HeartbeatEvery time.Duration
}

func (o *Orchestrator) Run(ctx context.Context) error {
	if o.LeaseTTL <= 0 {
		o.LeaseTTL = 30 * time.Second
	}
	if o.HeartbeatEvery <= 0 {
		o.HeartbeatEvery = 10 * time.Second
	}

	sub, err := o.Bus.Subscribe(ctx, string(model.EventStartSession))
	if err != nil {
		return err
	}
	defer sub.Close()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-sub.C():
			if !ok {
				return errors.New("event channel closed")
			}
			// Type switch on message interface
			if evt, ok := msg.(model.StartSessionEvent); ok {
				_ = o.handleStart(ctx, evt)
			}
		}
	}
}

func (o *Orchestrator) handleStart(ctx context.Context, e model.StartSessionEvent) error {
	// Single-writer lease per service key (prevents stampedes).
	lease, ok, err := o.Store.TryAcquireLease(ctx, e.ServiceRef, e.SessionID, o.LeaseTTL)
	if err != nil {
		// Store error: retry immediately or backoff? Bus default retry?
		return err
	}
	if !ok {
		// Lease held by another worker.
		// MVP Robustness: Requeue intent to verify it eventually succeeds or fails if lease is stale.
		// For proper "at-least-once", strictly we should define a retry count or rely on a durable queue.
		// Here: Log and ignore (let client retry) OR simple in-memory requeue.
		// User requested: "Event ignored on busy lease" is MVP contract.
		// We leave a comment for Phase 2 hardening.
		return nil
	}
	defer o.Store.ReleaseLease(ctx, lease.Key(), lease.Owner())

	// Heartbeat loop (crash-safe; store implementation decides renewal semantics).
	// We run this until work is done.
	hbCtx, hbCancel := context.WithCancel(ctx)
	defer hbCancel()
	go func() {
		t := time.NewTicker(o.HeartbeatEvery)
		defer t.Stop()
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-t.C:
				// Renew lease on store
				_, _, _ = o.Store.RenewLease(ctx, lease.Key(), lease.Owner(), o.LeaseTTL)
			}
		}
	}()

	// Placeholder "fast path": mark READY immediately.
	// Real implementation:
	//  - receiver tune state machine
	//  - ffmpeg worker spawn + readiness signal
	//  - packager ready
	//  - then READY
	_, err = o.Store.UpdateSession(ctx, e.SessionID, func(r *model.SessionRecord) error {
		r.State = model.SessionReady
		r.UpdatedAtUnix = time.Now().Unix()
		return nil
	})
	return err
}

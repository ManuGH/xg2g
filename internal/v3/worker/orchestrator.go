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
//  - acquire a single-writer lease per serviceKey
//  - transition Session: STARTING -> READY/FAILED
//  - (placeholder) for receiver tuning + ffmpeg lifecycle
type Orchestrator struct {
	Store store.StateStore
	Bus   bus.EventBus

	LeaseTTL      time.Duration
	HeartbeatEvery time.Duration
}

func (o *Orchestrator) Run(ctx context.Context) error {
	if o.LeaseTTL <= 0 {
		o.LeaseTTL = 30 * time.Second
	}
	if o.HeartbeatEvery <= 0 {
		o.HeartbeatEvery = 10 * time.Second
	}

	ch, cancel, err := o.Bus.Subscribe(ctx, "v3", 256)
	if err != nil {
		return err
	}
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-ch:
			if !ok {
				return errors.New("event channel closed")
			}
			if msg.Type == string(model.EventStartSession) {
				_ = o.handleStart(ctx, msg)
			}
		}
	}
}

func (o *Orchestrator) handleStart(ctx context.Context, msg bus.Message) error {
	e, ok := msg.Data.(model.StartSessionEvent)
	if !ok {
		return nil
	}

	// Single-writer lease per service key (prevents stampedes).
	lease, err := o.Store.AcquireLease(ctx, e.ServiceKey, e.SessionID, o.LeaseTTL)
	if err != nil {
		// Another worker likely owns the lease; we do not fail the session here.
		return nil
	}
	defer lease.Release(ctx)

	// Heartbeat loop (crash-safe; store implementation decides renewal semantics).
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
				_ = lease.Renew(ctx, o.LeaseTTL)
			}
		}
	}()

	// Placeholder "fast path": mark READY immediately.
	// Real implementation:
	//  - receiver tune state machine
	//  - ffmpeg worker spawn + readiness signal
	//  - packager ready
	//  - then READY
	return o.Store.UpdateSession(ctx, e.SessionID, func(r *model.SessionRecord) error {
		r.State = model.SessionReady
		r.UpdatedAt = time.Now().UTC()
		return nil
	})
}

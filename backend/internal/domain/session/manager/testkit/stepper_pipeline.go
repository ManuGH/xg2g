// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package testkit

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
)

type StepperPipeline struct {
	startCalled   chan struct{}
	startRelease  chan struct{}
	startReturned chan error
	startOnce     sync.Once
	releaseOnce   sync.Once
	returnOnce    sync.Once
	stopCalled    chan struct{}
	stopOnce      sync.Once
	stopCount     atomic.Int32
	healthy       atomic.Bool
}

func NewStepperPipeline() *StepperPipeline {
	p := &StepperPipeline{
		startCalled:   make(chan struct{}),
		startRelease:  make(chan struct{}),
		startReturned: make(chan error, 1),
		stopCalled:    make(chan struct{}),
	}
	p.healthy.Store(true)
	return p
}

func (p *StepperPipeline) Start(ctx context.Context, spec ports.StreamSpec) (ports.RunHandle, error) {
	p.startOnce.Do(func() {
		close(p.startCalled)
	})
	var err error
	select {
	case <-p.startRelease:
		err = nil
	case <-ctx.Done():
		err = ctx.Err()
	}
	p.returnOnce.Do(func() {
		p.startReturned <- err
		close(p.startReturned)
	})
	if err != nil {
		return "", err
	}
	return ports.RunHandle(spec.SessionID), nil
}

func (p *StepperPipeline) Stop(ctx context.Context, handle ports.RunHandle) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		p.stopOnce.Do(func() {
			close(p.stopCalled)
		})
		p.stopCount.Add(1)
		p.healthy.Store(false)
		return nil
	}
}

func (p *StepperPipeline) Health(ctx context.Context, handle ports.RunHandle) ports.HealthStatus {
	return ports.HealthStatus{Healthy: p.healthy.Load()}
}

func (p *StepperPipeline) StartCalled() <-chan struct{} {
	return p.startCalled
}

func (p *StepperPipeline) StartReturned() <-chan error {
	return p.startReturned
}

func (p *StepperPipeline) AllowStart() {
	p.releaseOnce.Do(func() {
		close(p.startRelease)
	})
}

func (p *StepperPipeline) StopCalled() <-chan struct{} {
	return p.stopCalled
}

func (p *StepperPipeline) StopCount() int32 {
	return p.stopCount.Load()
}

func (p *StepperPipeline) SetHealthy(healthy bool) {
	p.healthy.Store(healthy)
}

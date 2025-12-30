// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package exec

import (
	"context"
	"time"

	"github.com/ManuGH/xg2g/internal/v3/model"
)

// StubFactory returns stub components.
type StubFactory struct {
	TuneDuration time.Duration
}

func (f *StubFactory) NewTuner(slot int) (Tuner, error) {
	return &StubTuner{
		TuneDelay: f.TuneDuration,
	}, nil
}

func (f *StubFactory) NewTranscoder() (Transcoder, error) {
	return &StubTranscoder{
		StartDelay: 50 * time.Millisecond,
	}, nil
}

// StubTuner simulates a tuner that always succeeds.
type StubTuner struct {
	TuneDelay time.Duration
}

func (t *StubTuner) Tune(ctx context.Context, ref string) error {
	// Simulate tuning delay
	delay := t.TuneDelay
	if delay == 0 {
		delay = 100 * time.Millisecond
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}

func (t *StubTuner) Healthy(ctx context.Context) error {
	return nil
}

func (t *StubTuner) Close() error {
	return nil
}

// StubTranscoder simulates a running process.
type StubTranscoder struct {
	StartDelay time.Duration
	start      time.Time
}

func (t *StubTranscoder) Start(ctx context.Context, sessionID, source string, profileSpec model.ProfileSpec, startMs int64) error {
	t.start = time.Now()
	// Simulate startup delay
	delay := t.StartDelay
	if delay == 0 {
		delay = 50 * time.Millisecond
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}

func (t *StubTranscoder) Wait(ctx context.Context) (model.ExitStatus, error) {
	// In stub, we just wait on context, simulating an infinite running process
	<-ctx.Done()
	return model.ExitStatus{
		Code:      0,
		Reason:    "context_cancelled",
		StartedAt: t.start,
		EndedAt:   time.Now(),
	}, ctx.Err()
}

func (t *StubTranscoder) Stop(ctx context.Context) error {
	return nil
}

func (t *StubTranscoder) LastLogLines(n int) []string {
	return []string{}
}

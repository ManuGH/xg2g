//go:build v3
// +build v3

// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package fsm

import (
	"context"
	"fmt"
	"sync"
)

// Transition describes a single edge in the FSM.
// Guard may reject the transition; Action performs side-effects (worker-only).
type Transition[S ~string, E ~string] struct {
	From   S
	Event  E
	To     S
	Guard  func(ctx context.Context, from S, event E) error
	Action func(ctx context.Context, from S, to S, event E) error
}

// Machine is a small, test-friendly FSM runner.
// It is intentionally strict: unknown transitions are errors.
type Machine[S ~string, E ~string] struct {
	mu    sync.Mutex
	state S
	index map[string]Transition[S, E]
}

func New[S ~string, E ~string](initial S, transitions []Transition[S, E]) (*Machine[S, E], error) {
	idx := make(map[string]Transition[S, E], len(transitions))
	for _, t := range transitions {
		k := key(t.From, t.Event)
		if _, exists := idx[k]; exists {
			return nil, fmt.Errorf("duplicate transition: %s -> %s", t.From, t.Event)
		}
		idx[k] = t
	}
	return &Machine[S, E]{state: initial, index: idx}, nil
}

func (m *Machine[S, E]) State() S {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

// Fire attempts to apply an event atomically.
func (m *Machine[S, E]) Fire(ctx context.Context, event E) (S, error) {
	m.mu.Lock()
	from := m.state
	t, ok := m.index[key(from, event)]
	if !ok {
		m.mu.Unlock()
		return from, fmt.Errorf("invalid transition: state=%s event=%s", from, event)
	}

	// Guard + Action are executed outside the critical section to avoid blocking the world.
	to := t.To
	m.mu.Unlock()

	if t.Guard != nil {
		if err := t.Guard(ctx, from, event); err != nil {
			return from, err
		}
	}
	if t.Action != nil {
		if err := t.Action(ctx, from, to, event); err != nil {
			return from, err
		}
	}

	m.mu.Lock()
	// Defensive: ensure no one else moved state in between.
	if m.state != from {
		cur := m.state
		m.mu.Unlock()
		return cur, fmt.Errorf("concurrent transition detected: from=%s cur=%s event=%s", from, cur, event)
	}
	m.state = to
	m.mu.Unlock()

	return to, nil
}

func key[S ~string, E ~string](from S, event E) string {
	return string(from) + "|" + string(event)
}

// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package middleware

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"

	"github.com/ManuGH/xg2g/internal/control/http/deadline"
)

type RuntimeMode uint8

const (
	RuntimeDisabled RuntimeMode = iota
	RuntimeEnforced
)

type deadlineStateKey struct{}

// DeadlineState is owned by the request-lifecycle middleware.
type DeadlineState struct {
	Policy   deadline.RoutePolicy
	Timeouts deadline.DeadlineTimeouts
	Mode     RuntimeMode
	bound    atomic.Bool
	hijacked atomic.Bool
}

func (s *DeadlineState) BindPolicy(policy deadline.RoutePolicy) error {
	if s == nil {
		return fmt.Errorf("nil DeadlineState")
	}
	if !s.bound.CompareAndSwap(false, true) {
		return fmt.Errorf("policy already bound for request")
	}
	s.Policy = policy
	return nil
}

func (s *DeadlineState) IsBound() bool {
	return s != nil && s.bound.Load()
}

func (s *DeadlineState) SetHijacked() {
	if s != nil {
		s.hijacked.Store(true)
	}
}

func (s *DeadlineState) IsHijacked() bool {
	return s != nil && s.hijacked.Load()
}

func DeadlineStateFromContext(ctx context.Context) (*DeadlineState, bool) {
	state, ok := ctx.Value(deadlineStateKey{}).(*DeadlineState)
	return state, ok
}

// WithRoutePolicy is functionally passive in RuntimeDisabled mode.
func WithRoutePolicy(policy deadline.RoutePolicy, mode RuntimeMode) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if mode == RuntimeDisabled {
				next.ServeHTTP(w, r)
				return
			}

			state, ok := DeadlineStateFromContext(r.Context())
			if !ok {
				http.Error(w, "deadline state missing from request context", http.StatusInternalServerError)
				return
			}
			if err := state.BindPolicy(policy); err != nil {
				http.Error(w, "failed to bind deadline policy: "+err.Error(), http.StatusInternalServerError)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// WriteTimeoutMiddleware only establishes lifecycle state when enforcement is
// enabled. Deadline operations are introduced in later Phase 2 steps.
func WriteTimeoutMiddleware(timeouts deadline.DeadlineTimeouts, mode RuntimeMode) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if mode == RuntimeDisabled {
				next.ServeHTTP(w, r)
				return
			}
			state := &DeadlineState{Timeouts: timeouts, Mode: mode}
			ctx := context.WithValue(r.Context(), deadlineStateKey{}, state)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

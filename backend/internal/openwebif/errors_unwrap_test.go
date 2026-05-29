// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package openwebif

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
)

// TestOWIErrorUnwrapReachesTransportError proves the multi-error Unwrap lets
// errors.Is/As reach BOTH the boundary sentinel and the nested transport error.
// Before the fix, Unwrap returned only the sentinel, so the circuit-breaker
// classifier could never see a dial timeout / connection refused and never
// recorded a technical failure — the breaker never tripped on a dead box.
func TestOWIErrorUnwrapReachesTransportError(t *testing.T) {
	owi := &OWIError{
		Sentinel:  ErrTimeout,
		Operation: "zap",
		Err:       fmt.Errorf("dial tcp: %w", context.DeadlineExceeded),
	}

	if !errors.Is(owi, ErrTimeout) {
		t.Error("errors.Is(err, ErrTimeout) must still match via the sentinel branch")
	}
	if !errors.Is(owi, context.DeadlineExceeded) {
		t.Error("errors.Is(err, context.DeadlineExceeded) must reach the nested transport error")
	}
	if !isTechnicalError(owi) {
		t.Error("breaker classifier must treat a timeout OWIError as technical (so the breaker can trip)")
	}
}

func TestOWIErrorUnwrapReachesNetError(t *testing.T) {
	netErr := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	owi := &OWIError{
		Sentinel:  ErrUpstreamUnavailable,
		Operation: "zap",
		Err:       netErr,
	}

	var ne net.Error
	if !errors.As(owi, &ne) {
		t.Error("errors.As must reach the nested net.Error")
	}
	if !errors.Is(owi, ErrUpstreamUnavailable) {
		t.Error("sentinel matching must still work")
	}
	if !isTechnicalError(owi) {
		t.Error("connection-refused OWIError must be classified technical")
	}
}

// TestOWIErrorUnwrapNoTransportError verifies an HTTP-status OWIError (no nested
// Err) still unwraps to its sentinel and is not misclassified as technical.
func TestOWIErrorUnwrapNoTransportError(t *testing.T) {
	owi := &OWIError{Sentinel: ErrNotFound, Operation: "zap", Status: 404}
	if !errors.Is(owi, ErrNotFound) {
		t.Error("errors.Is(err, ErrNotFound) must match the sentinel")
	}
	if isTechnicalError(owi) {
		t.Error("a 404 must not be a technical failure")
	}
}

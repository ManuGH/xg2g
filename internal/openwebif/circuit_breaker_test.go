// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// SPDX-License-Identifier: MIT
package openwebif

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestCircuitBreaker_StateTransitions(t *testing.T) {
	// Setup: 2 failures to open, 100ms reset timeout
	cb := NewCircuitBreaker(2, 100*time.Millisecond)

	// Initial state: Closed
	if cb.State() != StateClosed {
		t.Errorf("Expected StateClosed, got %v", cb.State())
	}

	// 1st Failure: Should remain Closed
	err := cb.Execute(func() error { return errors.New("fail") })
	if err == nil {
		t.Error("Expected error, got nil")
	}
	if cb.State() != StateClosed {
		t.Errorf("Expected StateClosed after 1 failure, got %v", cb.State())
	}

	// 2nd Failure: Should switch to Open
	err = cb.Execute(func() error { return errors.New("fail") })
	if err == nil {
		t.Error("Expected error, got nil")
	}
	if cb.State() != StateOpen {
		t.Errorf("Expected StateOpen after 2 failures, got %v", cb.State())
	}

	// Request while Open: Should return ErrCircuitOpen immediately
	err = cb.Execute(func() error { return nil })
	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("Expected ErrCircuitOpen, got %v", err)
	}

	// Wait for reset timeout
	time.Sleep(150 * time.Millisecond)

	// Next request: Should be allowed (HalfOpen)
	// We simulate a success here
	err = cb.Execute(func() error { return nil })
	if err != nil {
		t.Errorf("Expected success in HalfOpen, got %v", err)
	}

	// Should be Closed again
	if cb.State() != StateClosed {
		t.Errorf("Expected StateClosed after success, got %v", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenFailure(t *testing.T) {
	cb := NewCircuitBreaker(1, 100*time.Millisecond)

	// Open the circuit
	_ = cb.Execute(func() error { return errors.New("fail") })
	if cb.State() != StateOpen {
		t.Fatalf("Expected StateOpen")
	}

	// Wait for reset
	time.Sleep(150 * time.Millisecond)

	// HalfOpen failure: Should go back to Open
	_ = cb.Execute(func() error { return errors.New("fail") })
	if cb.State() != StateOpen {
		t.Errorf("Expected StateOpen after HalfOpen failure, got %v", cb.State())
	}
}

func TestCircuitBreaker_Concurrency(t *testing.T) {
	cb := NewCircuitBreaker(100, 1*time.Minute) // High threshold
	var wg sync.WaitGroup

	// Run 100 concurrent requests
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cb.Execute(func() error { return nil })
		}()
	}

	wg.Wait()

	if cb.failures != 0 {
		t.Errorf("Expected 0 failures, got %d", cb.failures)
	}
	if cb.State() != StateClosed {
		t.Errorf("Expected StateClosed, got %v", cb.State())
	}
}

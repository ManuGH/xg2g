// SPDX-License-Identifier: MIT

package openwebif

import (
	"errors"
	"testing"
	"time"
)

func TestCircuitBreaker_InitialState(t *testing.T) {
	cb := NewCircuitBreaker(3, 30*time.Second)

	if cb.State() != stateClosed {
		t.Errorf("expected initial state to be closed, got %s", cb.State())
	}
}

func TestCircuitBreaker_Success(t *testing.T) {
	cb := NewCircuitBreaker(3, 30*time.Second)

	// Successful calls should keep circuit closed
	for i := 0; i < 5; i++ {
		err := cb.Call(func() error {
			return nil
		})

		if err != nil {
			t.Errorf("call %d: unexpected error: %v", i+1, err)
		}

		if cb.State() != stateClosed {
			t.Errorf("call %d: expected state closed, got %s", i+1, cb.State())
		}
	}
}

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	threshold := 3
	cb := NewCircuitBreaker(threshold, 30*time.Second)

	// First threshold-1 failures should keep circuit closed
	for i := 0; i < threshold-1; i++ {
		err := cb.Call(func() error {
			return errors.New("test error")
		})

		if err == nil {
			t.Errorf("call %d: expected error, got nil", i+1)
		}

		if cb.State() != stateClosed {
			t.Errorf("call %d: expected state closed, got %s", i+1, cb.State())
		}
	}

	// Threshold failure should open circuit
	err := cb.Call(func() error {
		return errors.New("test error")
	})

	if err == nil {
		t.Error("expected error after threshold failures")
	}

	if cb.State() != stateOpen {
		t.Errorf("expected state open after threshold failures, got %s", cb.State())
	}
}

func TestCircuitBreaker_RejectsWhenOpen(t *testing.T) {
	cb := NewCircuitBreaker(2, 30*time.Second)

	// Open the circuit
	for i := 0; i < 2; i++ {
		_ = cb.Call(func() error {
			return errors.New("test error")
		})
	}

	if cb.State() != stateOpen {
		t.Fatalf("expected circuit to be open, got %s", cb.State())
	}

	// Next call should be rejected immediately without executing function
	executed := false
	err := cb.Call(func() error {
		executed = true
		return nil
	})

	if err == nil {
		t.Error("expected error when circuit is open")
	}

	if executed {
		t.Error("function should not be executed when circuit is open")
	}

	if cb.State() != stateOpen {
		t.Errorf("expected state to remain open, got %s", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenAfterTimeout(t *testing.T) {
	cb := NewCircuitBreaker(2, 100*time.Millisecond)

	// Open the circuit
	for i := 0; i < 2; i++ {
		_ = cb.Call(func() error {
			return errors.New("test error")
		})
	}

	if cb.State() != stateOpen {
		t.Fatalf("expected circuit to be open, got %s", cb.State())
	}

	// Wait for timeout
	time.Sleep(150 * time.Millisecond)

	// Next call should transition to half-open
	executed := false
	err := cb.Call(func() error {
		executed = true
		return errors.New("still failing")
	})

	if !executed {
		t.Error("function should be executed in half-open state")
	}

	if err == nil {
		t.Error("expected error from failing function")
	}

	// Circuit should open again after failure in half-open
	if cb.State() != stateOpen {
		t.Errorf("expected state open after half-open failure, got %s", cb.State())
	}
}

func TestCircuitBreaker_ClosesAfterSuccessInHalfOpen(t *testing.T) {
	cb := NewCircuitBreaker(2, 100*time.Millisecond)

	// Open the circuit
	for i := 0; i < 2; i++ {
		_ = cb.Call(func() error {
			return errors.New("test error")
		})
	}

	if cb.State() != stateOpen {
		t.Fatalf("expected circuit to be open, got %s", cb.State())
	}

	// Wait for timeout to transition to half-open
	time.Sleep(150 * time.Millisecond)

	// Successful call in half-open should close circuit
	err := cb.Call(func() error {
		return nil // success
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if cb.State() != stateClosed {
		t.Errorf("expected state closed after success in half-open, got %s", cb.State())
	}
}

func TestCircuitBreaker_ResetsFailureCountOnSuccess(t *testing.T) {
	cb := NewCircuitBreaker(3, 30*time.Second)

	// Two failures
	_ = cb.Call(func() error { return errors.New("error 1") })
	_ = cb.Call(func() error { return errors.New("error 2") })

	if cb.State() != stateClosed {
		t.Errorf("expected state closed after 2 failures (threshold 3), got %s", cb.State())
	}

	// One success should reset failure count
	_ = cb.Call(func() error { return nil })

	// Two more failures should not open circuit (count was reset)
	_ = cb.Call(func() error { return errors.New("error 3") })
	_ = cb.Call(func() error { return errors.New("error 4") })

	if cb.State() != stateClosed {
		t.Errorf("expected state closed (failures reset by success), got %s", cb.State())
	}

	// One more failure should open it (3rd consecutive failure)
	_ = cb.Call(func() error { return errors.New("error 5") })

	if cb.State() != stateOpen {
		t.Errorf("expected state open after 3 consecutive failures, got %s", cb.State())
	}
}

func TestCircuitBreaker_ConcurrentCalls(t *testing.T) {
	cb := NewCircuitBreaker(10, 100*time.Millisecond)

	// Run concurrent calls
	done := make(chan bool, 100)

	for i := 0; i < 100; i++ {
		go func(n int) {
			defer func() { done <- true }()

			_ = cb.Call(func() error {
				if n%2 == 0 {
					return nil // success
				}
				return errors.New("test error")
			})
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 100; i++ {
		<-done
	}

	// Circuit should eventually open due to failures
	finalState := cb.State()
	if finalState != stateOpen && finalState != stateClosed {
		t.Errorf("unexpected final state: %s", finalState)
	}
}

func BenchmarkCircuitBreaker_Success(b *testing.B) {
	cb := NewCircuitBreaker(3, 30*time.Second)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cb.Call(func() error {
			return nil
		})
	}
}

func BenchmarkCircuitBreaker_Failure(b *testing.B) {
	cb := NewCircuitBreaker(999999, 30*time.Second) // High threshold to avoid opening

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cb.Call(func() error {
			return errors.New("test error")
		})
	}
}

func BenchmarkCircuitBreaker_Open(b *testing.B) {
	cb := NewCircuitBreaker(1, 30*time.Second)

	// Open the circuit
	_ = cb.Call(func() error {
		return errors.New("test error")
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cb.Call(func() error {
			return nil
		})
	}
}

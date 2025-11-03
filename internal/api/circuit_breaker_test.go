package api

import (
	"errors"
	"testing"
	"time"
)

// fakeClock allows controlling time in tests.
type fakeClock struct {
	now time.Time
}

func newFakeClock(start time.Time) *fakeClock {
	return &fakeClock{now: start}
}

func (f *fakeClock) Now() time.Time { return f.now }

func (f *fakeClock) Advance(d time.Duration) {
	f.now = f.now.Add(d)
}

// TestCircuitBreaker_StateMachine tests circuit breaker state transitions.
func TestCircuitBreaker_StateMachine(t *testing.T) {
	t.Run("closed_remains_closed_under_threshold", func(t *testing.T) {
		clk := newFakeClock(time.Now())
		cb := NewCircuitBreaker(3, 5*time.Second, WithClock(clk))

		// Fail twice (threshold is 3)
		_ = cb.Call(func() error { return errors.New("fail") })
		_ = cb.Call(func() error { return errors.New("fail") })

		if cb.State() != "closed" {
			t.Errorf("expected state closed, got %s", cb.State())
		}
	})

	t.Run("closed_to_open_on_threshold", func(t *testing.T) {
		clk := newFakeClock(time.Now())
		cb := NewCircuitBreaker(3, 5*time.Second, WithClock(clk))

		// Fail 3 times (threshold is 3)
		_ = cb.Call(func() error { return errors.New("fail") })
		_ = cb.Call(func() error { return errors.New("fail") })
		_ = cb.Call(func() error { return errors.New("fail") })

		if cb.State() != "open" {
			t.Errorf("expected state open, got %s", cb.State())
		}

		// Next call should return circuit open error
		err := cb.Call(func() error { return nil })
		if !errors.Is(err, errCircuitOpen) {
			t.Errorf("expected errCircuitOpen, got %v", err)
		}
	})

	t.Run("open_remains_open_during_timeout", func(t *testing.T) {
		clk := newFakeClock(time.Now())
		cb := NewCircuitBreaker(3, 5*time.Second, WithClock(clk))

		// Open circuit
		_ = cb.Call(func() error { return errors.New("fail") })
		_ = cb.Call(func() error { return errors.New("fail") })
		_ = cb.Call(func() error { return errors.New("fail") })

		// Advance time but not enough to exceed timeout
		clk.Advance(3 * time.Second)

		// Should still be open
		err := cb.Call(func() error { return nil })
		if !errors.Is(err, errCircuitOpen) {
			t.Errorf("expected errCircuitOpen during timeout, got %v", err)
		}
		if cb.State() != "open" {
			t.Errorf("expected state open, got %s", cb.State())
		}
	})

	t.Run("open_to_half_open_after_timeout", func(t *testing.T) {
		clk := newFakeClock(time.Now())
		cb := NewCircuitBreaker(3, 5*time.Second, WithClock(clk))

		// Open circuit
		_ = cb.Call(func() error { return errors.New("fail") })
		_ = cb.Call(func() error { return errors.New("fail") })
		_ = cb.Call(func() error { return errors.New("fail") })

		// Advance past timeout
		clk.Advance(6 * time.Second)

		// Next call should transition to half-open
		// Use a successful function to test state transition
		err := cb.Call(func() error { return nil })
		if err != nil {
			t.Errorf("expected success in half-open, got %v", err)
		}

		// After success in half-open, should be closed
		if cb.State() != "closed" {
			t.Errorf("expected state closed after success, got %s", cb.State())
		}
	})

	t.Run("half_open_to_closed_on_success", func(t *testing.T) {
		clk := newFakeClock(time.Now())
		cb := NewCircuitBreaker(3, 5*time.Second, WithClock(clk))

		// Open circuit
		_ = cb.Call(func() error { return errors.New("fail") })
		_ = cb.Call(func() error { return errors.New("fail") })
		_ = cb.Call(func() error { return errors.New("fail") })

		// Advance past timeout to reach half-open
		clk.Advance(6 * time.Second)

		// Success should close circuit
		err := cb.Call(func() error { return nil })
		if err != nil {
			t.Errorf("expected success, got %v", err)
		}
		if cb.State() != "closed" {
			t.Errorf("expected state closed, got %s", cb.State())
		}

		// Subsequent call should work (circuit is closed)
		err = cb.Call(func() error { return nil })
		if err != nil {
			t.Errorf("expected success after close, got %v", err)
		}
	})

	t.Run("half_open_to_open_on_failure", func(t *testing.T) {
		clk := newFakeClock(time.Now())
		cb := NewCircuitBreaker(3, 5*time.Second, WithClock(clk))

		// Open circuit
		_ = cb.Call(func() error { return errors.New("fail") })
		_ = cb.Call(func() error { return errors.New("fail") })
		_ = cb.Call(func() error { return errors.New("fail") })

		// Advance past timeout to reach half-open
		clk.Advance(6 * time.Second)

		// Failure in half-open should reopen circuit
		err := cb.Call(func() error { return errors.New("fail") })
		if err == nil || err.Error() != "fail" {
			t.Errorf("expected failure error, got %v", err)
		}
		if cb.State() != "open" {
			t.Errorf("expected state open after half-open failure, got %s", cb.State())
		}
	})
}

func TestNewCircuitBreaker_DefaultValues(t *testing.T) {
	tests := []struct {
		name            string
		threshold       int
		timeout         time.Duration
		expectThreshold int
		expectTimeout   time.Duration
	}{
		{
			name:            "zero threshold and timeout",
			threshold:       0,
			timeout:         0,
			expectThreshold: 3,
			expectTimeout:   30 * time.Second,
		},
		{
			name:            "negative threshold",
			threshold:       -1,
			timeout:         10 * time.Second,
			expectThreshold: 3,
			expectTimeout:   10 * time.Second,
		},
		{
			name:            "negative timeout",
			threshold:       5,
			timeout:         -5 * time.Second,
			expectThreshold: 5,
			expectTimeout:   30 * time.Second,
		},
		{
			name:            "valid values",
			threshold:       10,
			timeout:         60 * time.Second,
			expectThreshold: 10,
			expectTimeout:   60 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb := NewCircuitBreaker(tt.threshold, tt.timeout)
			if cb.threshold != tt.expectThreshold {
				t.Errorf("expected threshold %d, got %d", tt.expectThreshold, cb.threshold)
			}
			if cb.timeout != tt.expectTimeout {
				t.Errorf("expected timeout %v, got %v", tt.expectTimeout, cb.timeout)
			}
			if cb.state != circuitStateClosed {
				t.Errorf("expected initial state closed, got %s", cb.State())
			}
		})
	}
}

// TestCircuitBreaker_ConcurrentCalls tests thread safety.
// TODO: Implement once circuit breaker is ready for testing.
func TestCircuitBreaker_ConcurrentCalls(t *testing.T) {
	t.Skip("TODO: Implement concurrent access tests")

	// Use t.Parallel() and sync.WaitGroup to test concurrent calls
	// Verify no race conditions (run with -race flag)
}

// TestCircuitBreaker_ContextCancellation tests context handling.
// TODO: Implement once circuit breaker supports context.
func TestCircuitBreaker_ContextCancellation(t *testing.T) {
	t.Skip("TODO: Circuit breaker does not currently use context")
	// Future enhancement: Pass context to Call() method
	// and check context.Done() before executing function
}

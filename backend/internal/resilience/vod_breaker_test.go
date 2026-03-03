package resilience

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestVODBreaker_StateTransitions(t *testing.T) {
	cfg := VODConfig{
		Name:        "test_root",
		Window:      1 * time.Minute,
		MinRequests: 5,
		FailureRate: 0.5,
		Consecutive: 3,
		RetryAfter:  100 * time.Millisecond,
	}

	b := NewVODBreaker(cfg)

	// 1. Initial State: Closed
	assert.Equal(t, VODStateClosed, b.state)
	assert.True(t, b.Allow())

	// 2. Failure Accumulation (Consecutive)
	b.Report(false)
	b.Report(false)
	assert.Equal(t, VODStateClosed, b.state) // 2 < 3

	// 3. Trip on Consecutive
	b.Report(false)
	assert.Equal(t, VODStateOpen, b.state, "Should trip after 3 consecutive failures")

	// 4. Open State Rejection
	assert.False(t, b.Allow())

	// 5. Retry Interval
	time.Sleep(150 * time.Millisecond)
	assert.True(t, b.Allow(), "Should allow probe after expiry")
	assert.Equal(t, VODStateHalfOpen, b.state)

	// 6. Half-Open -> Open (Failure)
	b.Report(false)
	assert.Equal(t, VODStateOpen, b.state)
	assert.False(t, b.Allow())

	// 7. Half-Open -> Closed (Success)
	time.Sleep(150 * time.Millisecond)
	b.Allow() // Enter Half-Open
	b.Report(true)
	assert.Equal(t, VODStateClosed, b.state)
}

func TestVODBreaker_FailureRate(t *testing.T) {
	cfg := VODConfig{
		Name:        "test_rate",
		MinRequests: 4,
		FailureRate: 0.5, // > 50%
		Consecutive: 10,
	}
	b := NewVODBreaker(cfg)

	// 2 Success, 3 Failures (Total 5, Fail Rate 0.6)
	b.Report(true)
	b.Report(true)
	b.Report(false)
	b.Report(false)
	assert.Equal(t, VODStateClosed, b.state) // Total 4, Rate 0.5 (not > 0.5 yet? 2/4 = 0.5)

	b.Report(false) // Total 5, Fail 3. Rate 0.6 > 0.5
	assert.Equal(t, VODStateOpen, b.state, "Should trip on failure rate")
}

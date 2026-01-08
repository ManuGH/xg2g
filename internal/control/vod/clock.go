package vod

import "time"

// Clock abstracts time for deterministic testing.
type Clock interface {
	// Now returns the current time.
	Now() time.Time

	// After waits for the duration to elapse and then sends the current time on the returned channel.
	After(d time.Duration) <-chan time.Time
}

// RealClock uses system time.
type RealClock struct{}

func (RealClock) Now() time.Time {
	return time.Now()
}

func (RealClock) After(d time.Duration) <-chan time.Time {
	return time.After(d)
}

// MockClock provides deterministic time control for testing.
type MockClock struct {
	now      time.Time
	afterChs []chan time.Time
}

// NewMockClock creates a mock clock starting at the given time.
func NewMockClock(start time.Time) *MockClock {
	return &MockClock{now: start}
}

func (m *MockClock) Now() time.Time {
	return m.now
}

func (m *MockClock) After(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	m.afterChs = append(m.afterChs, ch)
	return ch
}

// Advance advances the mock clock by the given duration and fires any pending timers.
func (m *MockClock) Advance(d time.Duration) {
	m.now = m.now.Add(d)
	// Fire all pending After channels
	for _, ch := range m.afterChs {
		select {
		case ch <- m.now:
		default:
		}
	}
	m.afterChs = nil
}

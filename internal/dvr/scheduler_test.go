// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package dvr

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockClock
type MockClock struct {
	mu    sync.Mutex
	Timer *MockTimer
}

func (m *MockClock) Now() time.Time {
	return time.Now()
}

func (m *MockClock) NewTimer(d time.Duration) Timer {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Timer == nil {
		m.Timer = &MockTimer{
			CBox: make(chan time.Time, 1),
		}
	}
	// On creation, we don't fire immediately unless duration is 0
	// But in scheduler we use nextDuration().
	return m.Timer
}

// GetTimer returns the timer safely
func (m *MockClock) GetTimer() *MockTimer {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Timer
}

// MockTimer
type MockTimer struct {
	CBox chan time.Time
}

func (m *MockTimer) C() <-chan time.Time {
	return m.CBox
}

func (m *MockTimer) Stop() bool {
	return true
}

func (m *MockTimer) Reset(d time.Duration) bool {
	return true
}

func (m *MockTimer) Trigger() {
	select {
	case m.CBox <- time.Now():
	default:
	}
}

func TestScheduler_Loop(t *testing.T) {
	// Setup Mocks
	tmpDir := t.TempDir()
	rm := NewManager(tmpDir)

	mockClient := new(MockClient)

	// Create Engine
	mockCfg := config.AppConfig{}
	engine := NewSeriesEngine(mockCfg, rm, func() OWIClient { return mockClient })

	// Create Scheduler with MockClock
	sched := NewScheduler(engine)
	mockClock := &MockClock{}
	sched.clock = mockClock
	sched.StartupDelay = 0

	runCh := make(chan struct{}, 2)
	mockClient.On("GetTimers", mock.Anything).Return([]openwebif.Timer{}, nil).Run(func(_ mock.Arguments) {
		runCh <- struct{}{}
	})

	ctx, cancel := context.WithCancel(context.Background())

	// Start Scheduler
	sched.Start(ctx)

	// Give goroutine a moment to settle (wait for timer creation)
	// Since we are not using time to drive logic, simple sleep is okay for goroutine scheduling
	// But better is to wait for MockClock to receive NewTimer call if we mocked it properly.
	// For now, small sleep to allow loop to block on select.
	time.Sleep(10 * time.Millisecond)

	// Verify Timer was created
	timer := mockClock.GetTimer()
	assert.NotNil(t, timer, "Timer should be created on start")

	// Trigger 1st Run
	timer.Trigger()
	select {
	case <-runCh:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for 1st run")
	}

	// Trigger 2nd Run
	timer.Trigger()

	select {
	case <-runCh:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for 2nd run")
	}

	cancel()
	time.Sleep(25 * time.Millisecond) // Allow loop to observe cancellation

	// Final assertion safe now that loop is cancelled
	mockClient.AssertNumberOfCalls(t, "GetTimers", 2)
}

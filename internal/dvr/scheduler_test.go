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
	mockEpg := new(MockEpg)

	// Create Engine
	// Create Engine
	mockClient = new(MockClient)
	mockCfg := config.AppConfig{}
	engine := NewSeriesEngine(mockCfg, rm, func() OWIClient { return mockClient })

	// Create Scheduler with MockClock
	sched := NewScheduler(engine)
	mockClock := &MockClock{}
	sched.clock = mockClock
	sched.StartupDelay = 1 * time.Second

	// Expectations
	// Start(ctx) calls nextDuration(true) -> creates timer
	// Loop waits for timer.C
	// We trigger timer.C manually -> RunOnce calls

	mockClient.On("GetTimers", mock.Anything).Return([]openwebif.Timer{}, nil)
	mockEpg.On("GetEvents", mock.Anything, mock.Anything).Return([]openwebif.EPGEvent{}, nil)

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

	// Wait for Run to complete safely
	// We can't safely inspect mockClient.Calls while it's being written to.
	// We need a synchronization mechanism.
	// Since we can't easily modify the engine execution for tests,
	// we can rely on the fact that RunOnce logs "Series Engine run completed"
	// OR, we can add a side-effect to the mock that allows us to wait.

	// Let's redefine the MockClient expectation to signal a channel
	runCh := make(chan struct{}, 10)

	// Clear previous expectations from setup
	mockClient.ExpectedCalls = nil
	mockClient.Calls = nil

	mockClient.On("GetTimers", mock.Anything).Return([]openwebif.Timer{}, nil).Run(func(args mock.Arguments) {
		runCh <- struct{}{}
	})

	// Trigger 2nd Run (or just count the first one)
	// Actually we triggered above.
	// Wait for signal
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
	time.Sleep(10 * time.Millisecond) // Allow exit log

	// Final assertion safe now that loop is cancelled
	mockClient.AssertNumberOfCalls(t, "GetTimers", 2)
}

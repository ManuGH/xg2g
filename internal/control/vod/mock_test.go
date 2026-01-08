package vod

import (
	"context"
	"sync"
	"time"
)

// MockRunner implements Runner interface for deterministic fault injection testing.
type MockRunner struct {
	mu             sync.Mutex
	startErr       error
	handleBehavior *MockHandleBehavior
}

// MockHandleBehavior configures the behavior of MockHandle.
type MockHandleBehavior struct {
	WaitErr      error              // Error to return from Wait() (nil = success)
	WaitBlocks   bool               // If true, Wait() blocks until Stop() is called
	ProgressChan chan ProgressEvent // Progress events (test controls sends)
	StopUnblocks bool               // If true, Stop() unblocks Wait()
}

// NewMockRunner creates a mock runner with configurable behavior.
func NewMockRunner(startErr error, behavior *MockHandleBehavior) *MockRunner {
	return &MockRunner{
		startErr:       startErr,
		handleBehavior: behavior,
	}
}

// Start implements Runner.Start for testing.
func (m *MockRunner) Start(ctx context.Context, spec Spec) (Handle, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.startErr != nil {
		return nil, m.startErr
	}

	return &MockHandle{
		behavior:  m.handleBehavior,
		stopCalls: []StopCall{},
	}, nil
}

// MockHandle implements Handle interface for testing.
type MockHandle struct {
	mu        sync.Mutex
	behavior  *MockHandleBehavior
	stopCalls []StopCall
	stopped   bool
	waitDone  chan struct{} // Signaled when Wait should unblock
}

// StopCall records a call to Stop() with its arguments.
type StopCall struct {
	Grace time.Duration
	Kill  time.Duration
}

func (m *MockHandle) Progress() <-chan ProgressEvent {
	if m.behavior.ProgressChan == nil {
		ch := make(chan ProgressEvent)
		close(ch) // Return closed channel if not configured
		return ch
	}
	return m.behavior.ProgressChan
}

func (m *MockHandle) Wait() error {
	if m.behavior.WaitBlocks {
		// Block until Stop() is called or test closes waitDone
		m.mu.Lock()
		if m.waitDone == nil {
			m.waitDone = make(chan struct{})
		}
		done := m.waitDone
		m.mu.Unlock()

		<-done
	}

	return m.behavior.WaitErr
}

func (m *MockHandle) Stop(grace, kill time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.stopCalls = append(m.stopCalls, StopCall{Grace: grace, Kill: kill})
	m.stopped = true

	if m.behavior.StopUnblocks && m.waitDone != nil {
		close(m.waitDone)
	}

	return nil
}

func (m *MockHandle) Diagnostics() []string {
	return []string{"mock=true"}
}

// GetStopCalls returns all recorded Stop() invocations for assertion.
func (m *MockHandle) GetStopCalls() []StopCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]StopCall{}, m.stopCalls...)
}

// WasStopped returns true if Stop() was called.
func (m *MockHandle) WasStopped() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopped
}

package vod

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// MockExec for testing
type MockExec struct {
	mu        sync.Mutex
	Log       Logger
	Runs      []string
	Wait      time.Duration
	CancelErr error
}

func (m *MockExec) Run(ctx context.Context, command string, args []string) error {
	m.mu.Lock()
	m.Runs = append(m.Runs, command)
	wait := m.Wait
	m.mu.Unlock()

	select {
	case <-time.After(wait):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestManager_Ensure(t *testing.T) {
	// Setup
	exec := &MockExec{Wait: 50 * time.Millisecond}
	logger := zerolog.Nop() // zerolog.Nop() returns Logger value
	mgr := NewManager(exec, logger)

	// Test 1: Start New Run
	id := "test-run-1"
	work := func(ctx context.Context) error {
		return exec.Run(ctx, "mock", nil)
	}

	run, isNew := mgr.Ensure(context.Background(), id, work)
	if !isNew {
		t.Error("expected isNew=true for first call")
	}
	if run == nil {
		t.Fatal("expected run object")
	}

	// Test 2: Deduplication
	run2, isNew2 := mgr.Ensure(context.Background(), id, work)
	if isNew2 {
		t.Error("expected isNew=false for second call")
	}
	if run2 != run {
		t.Error("expected same run object")
	}

	// Wait for completion
	select {
	case <-run.Done:
		if run.Err != nil {
			t.Errorf("unexpected error: %v", run.Err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for run")
	}

	// Verify manager map cleanup
	mgr.mu.Lock()
	if _, exists := mgr.runs[id]; exists {
		t.Error("run should be removed from map after completion")
	}
	mgr.mu.Unlock()
}

func TestManager_Cancel(t *testing.T) {
	// Setup
	exec := &MockExec{Wait: 500 * time.Millisecond}
	mgr := NewManager(exec, zerolog.Nop())

	id := "test-cancel"
	started := make(chan struct{})

	work := func(ctx context.Context) error {
		close(started)
		return exec.Run(ctx, "mock", nil)
	}

	run, _ := mgr.Ensure(context.Background(), id, work)

	<-started

	// Cancel
	mgr.Cancel(id)

	// Verify error
	select {
	case <-run.Done:
		if !errors.Is(run.Err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", run.Err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for cancel")
	}

	// Verify Cleanup
	mgr.mu.Lock()
	if _, exists := mgr.runs[id]; exists {
		t.Error("run should be removed from map after cancel")
	}
	mgr.mu.Unlock()
}

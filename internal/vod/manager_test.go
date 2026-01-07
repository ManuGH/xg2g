package vod

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// MockExec for testing
type MockExec struct {
	mu   sync.Mutex
	Runs []string
	Wait time.Duration
}

func (m *MockExec) Run(ctx context.Context, command string, args []string) error {
	m.mu.Lock()
	m.Runs = append(m.Runs, command)
	wait := m.Wait
	m.mu.Unlock()

	if wait > 0 {
		select {
		case <-time.After(wait):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func TestManager_Ensure(t *testing.T) {
	// Setup
	exec := &MockExec{Wait: 50 * time.Millisecond}
	mgr := NewManager(exec, zerolog.Nop())

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
	if err := run.Wait(context.Background()); err != nil {
		t.Errorf("unexpected error: %v", err)
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
	if err := run.Wait(context.Background()); !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}

	// Verify Cleanup
	mgr.mu.Lock()
	if _, exists := mgr.runs[id]; exists {
		t.Error("run should be removed from map after cancel")
	}
	mgr.mu.Unlock()
}

func TestManager_Concurrent(t *testing.T) {
	exec := &MockExec{Wait: 20 * time.Millisecond}
	mgr := NewManager(exec, zerolog.Nop())
	id := "concurrent-test"

	const count = 100
	var wg sync.WaitGroup
	wg.Add(count)

	results := make(chan bool, count)

	for i := 0; i < count; i++ {
		go func() {
			defer wg.Done()
			_, isNew := mgr.Ensure(context.Background(), id, func(ctx context.Context) error {
				return exec.Run(ctx, "mock", nil)
			})
			results <- isNew
		}()
	}

	wg.Wait()
	close(results)

	newCount := 0
	for isNew := range results {
		if isNew {
			newCount++
		}
	}
	if newCount != 1 {
		t.Errorf("expected exactly 1 new run, got %d", newCount)
	}
}

func TestManager_Panic(t *testing.T) {
	mgr := NewManager(&MockExec{}, zerolog.Nop())
	id := "panic-test"

	run, _ := mgr.Ensure(context.Background(), id, func(ctx context.Context) error {
		panic("boom")
	})

	err := run.Wait(context.Background())
	if err == nil || !strings.Contains(err.Error(), "panic: boom") {
		t.Errorf("expected panic error, got %v", err)
	}
}

func TestManager_Stale(t *testing.T) {
	mgr := NewManager(&MockExec{Wait: 10 * time.Millisecond}, zerolog.Nop())
	id := "stale-test"

	run1, isNew1 := mgr.Ensure(context.Background(), id, func(ctx context.Context) error {
		return nil
	})
	if !isNew1 {
		t.Fatal("expected isNew1")
	}
	_ = run1.Wait(context.Background())

	// At this point, the run might still be in the map (cleanup is async)
	// But Ensure should detect it's done and recreate.
	run2, isNew2 := mgr.Ensure(context.Background(), id, func(ctx context.Context) error {
		return nil
	})

	if !isNew2 {
		t.Error("expected second Ensure to be isNew=true (recreation of stale run)")
	}
	if run1 == run2 {
		t.Error("expected different run objects")
	}
}

func TestManager_CancelAll(t *testing.T) {
	mgr := NewManager(&MockExec{Wait: 100 * time.Millisecond}, zerolog.Nop())

	id1 := "all-1"
	id2 := "all-2"

	work := func(ctx context.Context) error {
		return mgr.exec.Run(ctx, "mock", nil)
	}

	run1, _ := mgr.Ensure(context.Background(), id1, work)
	run2, _ := mgr.Ensure(context.Background(), id2, work)

	// Wait a bit to ensure they started
	time.Sleep(10 * time.Millisecond)

	mgr.CancelAll()

	if err := run1.Wait(context.Background()); !errors.Is(err, context.Canceled) {
		t.Errorf("run1: expected context.Canceled, got %v", err)
	}
	if err := run2.Wait(context.Background()); !errors.Is(err, context.Canceled) {
		t.Errorf("run2: expected context.Canceled, got %v", err)
	}

	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	if len(mgr.runs) != 0 {
		t.Errorf("expected 0 runs, got %d", len(mgr.runs))
	}
}

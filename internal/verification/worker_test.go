package verification_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ManuGH/xg2g/internal/verification"
)

type mockStore struct {
	mu        sync.Mutex
	sets      int
	lastState verification.DriftState
}

func (m *mockStore) Get(ctx context.Context) (verification.DriftState, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastState, !m.lastState.LastCheck.IsZero()
}

func (m *mockStore) Set(ctx context.Context, st verification.DriftState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sets++
	m.lastState = st
	return nil
}

type mockChecker struct {
	mu         sync.Mutex
	mismatches []verification.Mismatch
	block      chan struct{} // explicit block
	calls      int
}

func (m *mockChecker) Check(ctx context.Context) ([]verification.Mismatch, error) {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()

	if m.block != nil {
		select {
		case <-m.block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return m.mismatches, nil
}

func TestWorker_SkipIfBusy(t *testing.T) {
	store := &mockStore{}
	blockCh := make(chan struct{})
	checker := &mockChecker{block: blockCh}

	// Very fast cadence to force overlap
	// But we use a worker that skips if busy.
	worker := verification.NewWorker(store, 1*time.Millisecond, checker)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start worker in background
	go worker.Start(ctx)

	// Wait a bit to let ticks pile up
	time.Sleep(50 * time.Millisecond)

	// We expect exactly 1 active call because the first one blocked
	// and subsequent ones should see busy=true and return (skipping).

	m := checker
	m.mu.Lock()
	count := m.calls
	m.mu.Unlock()

	close(blockCh) // Unblock to let worker finish

	assert.Equal(t, 1, count, "worker should have exactly 1 active call during busy block")
}

func TestWorker_ChangeOnlyWrite(t *testing.T) {
	store := &mockStore{}
	checker := &mockChecker{
		mismatches: []verification.Mismatch{
			{Kind: verification.KindConfig, Key: "k", Expected: "a", Actual: "b"},
		},
	}

	worker := verification.NewWorker(store, 100*time.Millisecond, checker)

	ctx, cancel := context.WithCancel(context.Background())

	go worker.Start(ctx)

	time.Sleep(150 * time.Millisecond) // enough for ~1-2 ticks + initial run
	cancel()

	// First run: writes (sets=1)
	// Second run: same mismatches -> content hash same -> no write
	// store.sets should be exactly 1
	assert.Equal(t, 1, store.sets, "should verify write-on-change only")
}

func TestWorker_ContentChangeTriggersWrite(t *testing.T) {
	store := &mockStore{}
	store.lastState = verification.DriftState{} // initial empty
	// Placeholder for future test
	assert.NotNil(t, store)
}

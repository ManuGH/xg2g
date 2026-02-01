// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package watchdog

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockClock struct {
	mu           sync.Mutex
	now          time.Time
	latestTicker *mockTicker
}

func (m *mockClock) Now() time.Time                         { m.mu.Lock(); defer m.mu.Unlock(); return m.now }
func (m *mockClock) After(d time.Duration) <-chan time.Time { return make(chan time.Time) }
func (m *mockClock) NewTicker(d time.Duration) ticker {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.latestTicker = &mockTicker{c: make(chan time.Time)}
	return m.latestTicker
}

type mockTicker struct {
	c chan time.Time
}

func (m *mockTicker) C() <-chan time.Time { return m.c }
func (m *mockTicker) Stop()               {}

func TestWatchdog_StartTimeout(t *testing.T) {
	clock := &mockClock{now: time.Now()}
	w := New(2*time.Second, 5*time.Second)
	w.clock = clock

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- w.Run(ctx)
	}()

	// Wait for Run to start and ticker to be created
	time.Sleep(50 * time.Millisecond)

	clock.mu.Lock()
	clock.now = clock.now.Add(3 * time.Second)
	ticker := clock.latestTicker
	clock.mu.Unlock()

	require.NotNil(t, ticker)
	ticker.c <- clock.Now()

	err := <-errCh
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, StateTimedOut, w.State())
}

func TestWatchdog_StallTimeout(t *testing.T) {
	clock := &mockClock{now: time.Now()}
	w := New(2*time.Second, 5*time.Second)
	w.clock = clock

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- w.Run(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	clock.mu.Lock()
	ticker := clock.latestTicker
	clock.mu.Unlock()
	require.NotNil(t, ticker)

	// 1. Initial heartbeat to move to Running
	w.ParseLine("out_time_ms=100")
	assert.Equal(t, StateRunning, w.State())

	// 2. Simulate time passing beyond stall timeout
	clock.mu.Lock()
	clock.now = clock.now.Add(6 * time.Second)
	clock.mu.Unlock()
	ticker.c <- clock.Now()

	err := <-errCh
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, StateStalled, w.State())
}

func TestWatchdog_MeaningfulProgress(t *testing.T) {
	clock := &mockClock{now: time.Now()}
	w := New(2*time.Second, 5*time.Second)
	w.clock = clock

	// 1. Test out_time_ms vs total_size
	w.ParseLine("frame=10")
	assert.Equal(t, StateStarting, w.State(), "frame= alone is not meaningful progress")

	w.ParseLine("out_time_ms=0")
	assert.Equal(t, StateStarting, w.State(), "out_time_ms=0 is not meaningful progress")

	w.ParseLine("total_size=123")
	assert.Equal(t, StateRunning, w.State(), "total_size > 0 IS meaningful progress")
}

func TestWatchdog_ParserRobustness(t *testing.T) {
	w := New(2*time.Second, 5*time.Second)

	// Handles NA
	w.ParseLine("out_time_ms=N/A")
	assert.Equal(t, int64(0), w.lastOutTimeMs)

	// Handles junk
	w.ParseLine("garbage")
	w.ParseLine("key=val=extra")

	// Handles monotonic size
	w.ParseLine("total_size=100")
	assert.Equal(t, int64(100), w.lastTotalSize)
	w.ParseLine("total_size=50")
	assert.Equal(t, int64(100), w.lastTotalSize, "Should not record non-monotonic size")
}

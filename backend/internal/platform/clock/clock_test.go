// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package clock

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRealClock(t *testing.T) {
	c := System
	now := c.Now()
	assert.False(t, now.IsZero())
	assert.True(t, c.Since(now) >= 0)

	timer := c.NewTimer(5 * time.Millisecond)
	select {
	case firedAt := <-timer.C():
		assert.False(t, firedAt.IsZero())
	case <-time.After(100 * time.Millisecond):
		t.Fatal("RealClock timer timed out")
	}

	ticker := c.NewTicker(5 * time.Millisecond)
	select {
	case <-ticker.C():
		ticker.Stop()
	case <-time.After(100 * time.Millisecond):
		t.Fatal("RealClock ticker timed out")
	}
}

func TestVirtualClock_AdvanceTimersChronologically(t *testing.T) {
	start := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	vc := NewVirtual(start)

	timer1 := vc.NewTimer(5 * time.Second)
	timer2 := vc.NewTimer(10 * time.Second)
	timer3 := vc.NewTimer(15 * time.Second)

	assert.Equal(t, 3, vc.ActiveWaiters())

	// Advance 4s: none should fire
	vc.Advance(4 * time.Second)
	select {
	case <-timer1.C():
		t.Fatal("timer1 should not have fired yet")
	default:
	}

	// Advance 2s (total 6s): timer1 must fire, timer2 and timer3 must not
	vc.Advance(2 * time.Second)
	select {
	case firedAt := <-timer1.C():
		assert.Equal(t, start.Add(5*time.Second), firedAt)
	default:
		t.Fatal("timer1 should have fired")
	}
	select {
	case <-timer2.C():
		t.Fatal("timer2 should not have fired yet")
	default:
	}

	// Advance 5s (total 11s): timer2 fires
	vc.Advance(5 * time.Second)
	select {
	case firedAt := <-timer2.C():
		assert.Equal(t, start.Add(10*time.Second), firedAt)
	default:
		t.Fatal("timer2 should have fired")
	}

	// Advance 5s (total 16s): timer3 fires
	vc.Advance(5 * time.Second)
	select {
	case firedAt := <-timer3.C():
		assert.Equal(t, start.Add(15*time.Second), firedAt)
	default:
		t.Fatal("timer3 should have fired")
	}

	assert.Equal(t, 0, vc.ActiveWaiters())
}

func TestVirtualClock_TimerStopAndReset(t *testing.T) {
	start := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	vc := NewVirtual(start)

	timer := vc.NewTimer(10 * time.Second)
	assert.True(t, timer.Stop())
	assert.False(t, timer.Stop()) // Second stop returns false

	// Advance past deadline
	vc.Advance(15 * time.Second)
	select {
	case <-timer.C():
		t.Fatal("Stopped timer must never fire")
	default:
	}

	// Reset timer to 5s from current virtual time
	assert.False(t, timer.Reset(5*time.Second))
	vc.Advance(5 * time.Second)

	select {
	case firedAt := <-timer.C():
		assert.Equal(t, start.Add(20*time.Second), firedAt)
	default:
		t.Fatal("Reset timer must fire at new deadline")
	}
}

func TestVirtualClock_Tickers(t *testing.T) {
	start := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	vc := NewVirtual(start)

	ticker := vc.NewTicker(1 * time.Second)
	assert.Equal(t, 1, vc.ActiveWaiters())

	// Advance 3.5s -> should produce ticks
	vc.Advance(3500 * time.Millisecond)

	ticks := 0
	for {
		select {
		case <-ticker.C():
			ticks++
		default:
			goto done
		}
	}
done:
	assert.GreaterOrEqual(t, ticks, 1, "Must have received at least 1 tick")

	ticker.Stop()
	vc.Advance(5 * time.Second)
	select {
	case <-ticker.C():
		t.Fatal("Stopped ticker must not produce ticks")
	default:
	}
}

func TestVirtualClock_BlockUntilWaiters(t *testing.T) {
	start := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	vc := NewVirtual(start)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)
		<-vc.After(1 * time.Hour)
	}()

	go func() {
		defer wg.Done()
		time.Sleep(20 * time.Millisecond)
		<-vc.After(2 * time.Hour)
	}()

	// Block until both background goroutines have registered their timers
	vc.BlockUntilWaiters(2)
	assert.Equal(t, 2, vc.ActiveWaiters())

	// Instantly advance 2 hours in zero real time
	vc.Advance(2 * time.Hour)
	wg.Wait()
}

func TestVirtualClock_ConcurrentRaceFree(t *testing.T) {
	start := time.Now()
	vc := NewVirtual(start)

	var wg sync.WaitGroup
	workers := 10

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = vc.Now()
				t := vc.NewTimer(time.Duration(j) * time.Millisecond)
				if j%2 == 0 {
					t.Stop()
				}
				tk := vc.NewTicker(time.Duration(j+1) * time.Millisecond)
				tk.Stop()
			}
		}(i)
	}

	for i := 0; i < 50; i++ {
		vc.Advance(1 * time.Millisecond)
	}

	wg.Wait()
	require.NotNil(t, vc)
}

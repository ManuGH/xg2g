// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package clock

import (
	"sort"
	"sync"
	"time"
)

// Clock abstracts time measurement and scheduling for deterministic, race-free testing.
type Clock interface {
	Now() time.Time
	Since(t time.Time) time.Duration
	Until(t time.Time) time.Duration
	Sleep(d time.Duration)
	After(d time.Duration) <-chan time.Time
	NewTimer(d time.Duration) Timer
	NewTicker(d time.Duration) Ticker
}

// Timer abstracts a cancellable single-shot timer.
type Timer interface {
	C() <-chan time.Time
	Reset(d time.Duration) bool
	Stop() bool
}

// Ticker abstracts a recurring interval ticker.
type Ticker interface {
	C() <-chan time.Time
	Reset(d time.Duration)
	Stop()
}

// RealClock implements Clock using the standard library time package.
type RealClock struct{}

// System is the default RealClock instance.
var System Clock = RealClock{}

func (RealClock) Now() time.Time                         { return time.Now() }
func (RealClock) Since(t time.Time) time.Duration        { return time.Since(t) }
func (RealClock) Until(t time.Time) time.Duration        { return time.Until(t) }
func (RealClock) Sleep(d time.Duration)                  { time.Sleep(d) }
func (RealClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

func (RealClock) NewTimer(d time.Duration) Timer {
	return &realTimer{Timer: time.NewTimer(d)}
}

func (RealClock) NewTicker(d time.Duration) Ticker {
	return &realTicker{Ticker: time.NewTicker(d)}
}

type realTimer struct {
	*time.Timer
}

func (r *realTimer) C() <-chan time.Time {
	return r.Timer.C
}

type realTicker struct {
	*time.Ticker
}

func (r *realTicker) C() <-chan time.Time {
	return r.Ticker.C
}

// VirtualClock provides a thread-safe, mockable time source that only advances when instructed.
type VirtualClock struct {
	mu      sync.Mutex
	current time.Time
	timers  []*virtualTimer
	tickers []*virtualTicker
	cond    *sync.Cond
}

// NewVirtual creates a new VirtualClock starting at start time.
func NewVirtual(start time.Time) *VirtualClock {
	vc := &VirtualClock{
		current: start,
	}
	vc.cond = sync.NewCond(&vc.mu)
	return vc
}

func (vc *VirtualClock) Now() time.Time {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	return vc.current
}

func (vc *VirtualClock) Since(t time.Time) time.Duration {
	return vc.Now().Sub(t)
}

func (vc *VirtualClock) Until(t time.Time) time.Duration {
	return t.Sub(vc.Now())
}

func (vc *VirtualClock) Sleep(d time.Duration) {
	<-vc.After(d)
}

func (vc *VirtualClock) After(d time.Duration) <-chan time.Time {
	return vc.NewTimer(d).C()
}

func (vc *VirtualClock) NewTimer(d time.Duration) Timer {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	ch := make(chan time.Time, 1)
	vt := &virtualTimer{
		vc:       vc,
		deadline: vc.current.Add(d),
		ch:       ch,
	}
	vc.timers = append(vc.timers, vt)
	vc.cond.Broadcast()
	return vt
}

func (vc *VirtualClock) NewTicker(d time.Duration) Ticker {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	ch := make(chan time.Time, 1)
	vt := &virtualTicker{
		vc:       vc,
		interval: d,
		nextTick: vc.current.Add(d),
		ch:       ch,
	}
	vc.tickers = append(vc.tickers, vt)
	vc.cond.Broadcast()
	return vt
}

// Advance moves virtual time forward by d and fires all due timers and tickers in chronological order.
func (vc *VirtualClock) Advance(d time.Duration) {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	targetTime := vc.current.Add(d)
	vc.current = targetTime

	// Fire due timers
	var remainingTimers []*virtualTimer
	// Sort timers by deadline to guarantee chronological dispatch
	sort.Slice(vc.timers, func(i, j int) bool {
		return vc.timers[i].deadline.Before(vc.timers[j].deadline)
	})

	for _, t := range vc.timers {
		if !t.stopped && !t.fired && !t.deadline.After(targetTime) {
			t.fired = true
			select {
			case t.ch <- t.deadline:
			default:
			}
		} else if !t.stopped && !t.fired {
			remainingTimers = append(remainingTimers, t)
		}
	}
	vc.timers = remainingTimers

	// Fire due tickers
	for _, tk := range vc.tickers {
		if tk.stopped {
			continue
		}
		for !tk.nextTick.After(targetTime) {
			select {
			case tk.ch <- tk.nextTick:
			default:
			}
			tk.nextTick = tk.nextTick.Add(tk.interval)
		}
	}

	vc.cond.Broadcast()
}

// Set explicitly sets the current virtual time.
func (vc *VirtualClock) Set(t time.Time) {
	vc.mu.Lock()
	diff := t.Sub(vc.current)
	vc.mu.Unlock()
	if diff > 0 {
		vc.Advance(diff)
	} else {
		vc.mu.Lock()
		vc.current = t
		vc.mu.Unlock()
	}
}

// ActiveWaiters returns the number of active timers and tickers registered with the clock.
func (vc *VirtualClock) ActiveWaiters() int {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	return len(vc.timers) + len(vc.tickers)
}

// BlockUntilWaiters blocks until at least n timers/tickers are waiting.
func (vc *VirtualClock) BlockUntilWaiters(n int) {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	for (len(vc.timers) + len(vc.tickers)) < n {
		vc.cond.Wait()
	}
}

type virtualTimer struct {
	vc       *VirtualClock
	deadline time.Time
	ch       chan time.Time
	stopped  bool
	fired    bool
}

func (vt *virtualTimer) C() <-chan time.Time {
	return vt.ch
}

func (vt *virtualTimer) Stop() bool {
	vt.vc.mu.Lock()
	defer vt.vc.mu.Unlock()
	wasActive := !vt.stopped && !vt.fired
	vt.stopped = true
	return wasActive
}

func (vt *virtualTimer) Reset(d time.Duration) bool {
	vt.vc.mu.Lock()
	defer vt.vc.mu.Unlock()
	wasActive := !vt.stopped && !vt.fired
	vt.stopped = false
	vt.fired = false
	vt.deadline = vt.vc.current.Add(d)

	// Ensure timer is in active timers list
	found := false
	for _, t := range vt.vc.timers {
		if t == vt {
			found = true
			break
		}
	}
	if !found {
		vt.vc.timers = append(vt.vc.timers, vt)
		vt.vc.cond.Broadcast()
	}

	return wasActive
}

type virtualTicker struct {
	vc       *VirtualClock
	interval time.Duration
	nextTick time.Time
	ch       chan time.Time
	stopped  bool
}

func (vt *virtualTicker) C() <-chan time.Time {
	return vt.ch
}

func (vt *virtualTicker) Stop() {
	vt.vc.mu.Lock()
	defer vt.vc.mu.Unlock()
	vt.stopped = true
}

func (vt *virtualTicker) Reset(d time.Duration) {
	vt.vc.mu.Lock()
	defer vt.vc.mu.Unlock()
	vt.interval = d
	vt.nextTick = vt.vc.current.Add(d)
	vt.stopped = false
}

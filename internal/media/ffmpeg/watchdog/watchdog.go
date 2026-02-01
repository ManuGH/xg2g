// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package watchdog

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
)

type State int

type clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
	NewTicker(d time.Duration) ticker
}

type ticker interface {
	C() <-chan time.Time
	Stop()
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }
func (realClock) NewTicker(d time.Duration) ticker       { return &realTicker{time.NewTicker(d)} }

type realTicker struct {
	*time.Ticker
}

func (rt *realTicker) C() <-chan time.Time { return rt.Ticker.C }

const (
	StateStarting State = iota
	StateRunning
	StateStalled
	StateTimedOut
	StateCompleted
	StateFailed
)

// Watchdog monitors FFmpeg progress and enforces start/stall timeouts.
type Watchdog struct {
	mu sync.RWMutex

	startTimeout time.Duration
	stallTimeout time.Duration

	lastOutTimeMs int64
	lastTotalSize int64
	lastHeartbeat time.Time

	state       State
	hasProgress bool

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	clock clock
}

// New creates a new watchdog with given timeouts.
func New(startTimeout, stallTimeout time.Duration) *Watchdog {
	return &Watchdog{
		startTimeout: startTimeout,
		stallTimeout: stallTimeout,
		done:         make(chan struct{}),
		clock:        realClock{},
	}
}

// Run starts the watchdog loop.
// It returns an error if a timeout or stall is detected.
func (w *Watchdog) Run(ctx context.Context) error {
	w.mu.Lock()
	w.ctx, w.cancel = context.WithCancel(ctx)
	w.lastHeartbeat = w.clock.Now()
	w.state = StateStarting
	w.mu.Unlock()

	ticker := w.clock.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return nil
		case <-ticker.C():
			if err := w.check(); err != nil {
				return err
			}
		}
	}
}

// ParseLine processes a line from FFmpeg -progress pipe.
func (w *Watchdog) ParseLine(line string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	parts := strings.Split(line, "=")
	if len(parts) != 2 {
		return
	}

	key := strings.TrimSpace(parts[0])
	val := strings.TrimSpace(parts[1])

	switch key {
	case "out_time_ms":
		ms, _ := strconv.ParseInt(val, 10, 64)
		if ms > w.lastOutTimeMs {
			w.lastOutTimeMs = ms
			w.recordHeartbeat()
		}
	case "total_size":
		size, _ := strconv.ParseInt(val, 10, 64)
		if size > w.lastTotalSize {
			w.lastTotalSize = size
			w.recordHeartbeat()
		}
	case "progress":
		if val == "end" {
			w.state = StateCompleted
			w.cancel()
		}
	}
}

func (w *Watchdog) recordHeartbeat() {
	w.lastHeartbeat = w.clock.Now()
	if !w.hasProgress && (w.lastOutTimeMs > 0 || w.lastTotalSize > 0) {
		w.hasProgress = true
		w.state = StateRunning
		log.L().Debug().Msg("watchdog: meaningful progress detected")
	}
}

func (w *Watchdog) check() error {
	w.mu.RLock()
	defer w.mu.RUnlock()

	now := w.clock.Now()
	elapsed := now.Sub(w.lastHeartbeat)

	switch w.state {
	case StateStarting:
		if elapsed > w.startTimeout {
			w.state = StateTimedOut
			return context.DeadlineExceeded // Maps to 504
		}
	case StateRunning:
		if elapsed > w.stallTimeout {
			w.state = StateStalled
			return context.DeadlineExceeded // Maps to 504
		}
	}

	return nil
}

// State returns current watchdog state.
func (w *Watchdog) State() State {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.state
}

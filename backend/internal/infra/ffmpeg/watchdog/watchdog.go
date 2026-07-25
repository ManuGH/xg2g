// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package watchdog

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
)

// ErrCorruptInput is returned by Run when the source keeps decoding to garbage
// (see ObserveCorruptInput) even though frame=/out_time_ms is still advancing,
// so the ordinary progress-based stall check never trips.
var ErrCorruptInput = errors.New("corrupt input: decode failures exceeded threshold")

// corruptInputThreshold/-Window bound a sliding count of per-frame decode
// corruption signals (e.g. "non-existing PPS", "no frame!") observed in
// ffmpeg's stderr. A broadcast source delivering trickle/poison data during a
// bad tune-in produces exactly this pattern: ffmpeg's own -progress output
// keeps advancing (it's still consuming input and emitting frames), so
// StateRunning never times out, but every frame is undecodable and the client
// never gets real picture. First-pass values, not yet tuned against
// production traffic: chosen well below the ~78-in-1s burst observed during
// the incident that motivated this, while still requiring several seconds of
// sustained corruption so an isolated benign glitch can't trip it.
const (
	corruptInputThreshold = 40
	corruptInputWindow    = 5 * time.Second
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

	corruptInputCount       int
	corruptInputWindowStart time.Time

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
			w.recordParsedProgress()
		}
	case "total_size":
		size, _ := strconv.ParseInt(val, 10, 64)
		if size > w.lastTotalSize {
			w.lastTotalSize = size
			w.recordParsedProgress()
		}
	case "progress":
		if val == "end" {
			w.state = StateCompleted
			// cancel is only set once Run() starts; ParseLine can be driven by the
			// stderr scanner before then, so a nil cancel would panic.
			if w.cancel != nil {
				w.cancel()
			}
		}
	}
}

// ObserveProgress records externally observed startup/runtime progress such as
// first encoded frames or the first HLS segment write.
func (w *Watchdog) ObserveProgress() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.recordExternalProgress()
}

// ObserveCorruptInput records a per-line decode-corruption signal from
// ffmpeg's stderr (non-existing PPS/SPS, "no frame!", corrupt reference
// frames). Unlike ObserveProgress, this deliberately does NOT count as
// progress — a source that keeps advancing frame=/out_time_ms while decoding
// garbage must still be caught, which is exactly what this independent
// density check is for.
func (w *Watchdog) ObserveCorruptInput() {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.clock.Now()
	if now.Sub(w.corruptInputWindowStart) > corruptInputWindow {
		w.corruptInputWindowStart = now
		w.corruptInputCount = 0
	}
	w.corruptInputCount++
}

func (w *Watchdog) recordParsedProgress() {
	w.lastHeartbeat = w.clock.Now()
	if !w.hasProgress && (w.lastOutTimeMs > 0 || w.lastTotalSize > 0) {
		w.hasProgress = true
		w.state = StateRunning
		log.L().Debug().Msg("watchdog: meaningful progress detected")
	}
}

func (w *Watchdog) recordExternalProgress() {
	w.lastHeartbeat = w.clock.Now()
	if !w.hasProgress {
		w.hasProgress = true
		w.state = StateRunning
		log.L().Debug().Msg("watchdog: meaningful progress detected")
	}
}

func (w *Watchdog) check() error {
	w.mu.Lock()
	defer w.mu.Unlock()

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
		if w.corruptInputCount >= corruptInputThreshold && now.Sub(w.corruptInputWindowStart) <= corruptInputWindow {
			w.state = StateStalled
			return ErrCorruptInput
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

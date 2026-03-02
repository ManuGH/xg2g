package resilience

import (
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/metrics"
)

// VODState represents the circuit breaker state.
type VODState int

const (
	VODStateClosed VODState = iota
	VODStateOpen
	VODStateHalfOpen
)

// VODConfig holds circuit breaker configuration.
type VODConfig struct {
	Name        string
	Window      time.Duration
	MinRequests int
	FailureRate float64 // 0.0-1.0
	Consecutive int
	RetryAfter  time.Duration
}

// VODBreaker implements a stateful circuit breaker for VOD.
type VODBreaker struct {
	mu          sync.RWMutex
	name        string
	state       VODState
	counts      *windowCounts
	consecutive int
	expiry      time.Time
	cfg         VODConfig
}

type windowCounts struct {
	buckets        [10]bucket
	currentIdx     int
	lastRotate     time.Time
	bucketDuration time.Duration
	mu             sync.Mutex
}

type bucket struct {
	success int
	failure int
}

// ... bucket struct

func newWindowCounts(bucketDuration time.Duration) *windowCounts {
	if bucketDuration == 0 {
		bucketDuration = 1 * time.Minute
	}
	return &windowCounts{
		lastRotate:     time.Now(),
		bucketDuration: bucketDuration,
	}
}

func (w *windowCounts) add(success bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.rotateIfNeeded()
	if success {
		w.buckets[w.currentIdx].success++
	} else {
		w.buckets[w.currentIdx].failure++
	}
}

func (w *windowCounts) rotateIfNeeded() {
	now := time.Now()
	elapsed := now.Sub(w.lastRotate)
	bucketsToRotate := int(elapsed / w.bucketDuration)

	if bucketsToRotate > 0 {
		for i := 0; i < bucketsToRotate && i < 10; i++ {
			w.currentIdx = (w.currentIdx + 1) % 10
			w.buckets[w.currentIdx] = bucket{} // Reset new bucket
		}
		w.lastRotate = now
	}
}

func (w *windowCounts) stats() (int, int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.rotateIfNeeded() // Ensure current view is fresh

	s, f := 0, 0
	for _, b := range w.buckets {
		s += b.success
		f += b.failure
	}
	return s, f
}

// NewVODBreaker creates a new circuit breaker.
func NewVODBreaker(cfg VODConfig) *VODBreaker {
	if cfg.Window == 0 {
		cfg.Window = 10 * time.Minute
	}
	// Buckets are fixed at 10.
	// We want total window = cfg.Window.
	// So each bucket covers cfg.Window / 10.
	// But current implementation relies on `minutes := int(now.Sub(w.lastRotate).Minutes())`.
	// This implies fixed 1-minute buckets (total 10 minute window).
	// To respect cfg.Window, we should adjust the rotation logic.
	// Minimal intrusive fix: Document limitation or adjust rotate logic.
	// User requested "implement Window correctly or remove".
	// Let's adjust rotate logic to use `cfg.Window / 10`.
	return &VODBreaker{
		name:   cfg.Name,
		state:  VODStateClosed,
		counts: newWindowCounts(cfg.Window / 10),
		cfg:    cfg,
	}
}

// Allow checks if a request can proceed.
func (b *VODBreaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()

	switch b.state {
	case VODStateOpen:
		if now.After(b.expiry) {
			b.state = VODStateHalfOpen
			metrics.IncVODCircuitHalfOpen()
			log.L().Info().Str("breaker", b.name).Msg("circuit breaker entering HALF-OPEN state")
			return true // Allow ONE probe
		}
		return false
	case VODStateHalfOpen:
		return true
	default:
		return true
	}
}

// Report records the result of a request.
func (b *VODBreaker) Report(success bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state == VODStateHalfOpen {
		if success {
			b.state = VODStateClosed
			b.consecutive = 0
			log.L().Info().Str("breaker", b.name).Msg("circuit breaker CLOSED (probe succeeded)")
		} else {
			b.state = VODStateOpen
			b.expiry = time.Now().Add(b.cfg.RetryAfter)
			metrics.IncVODCircuitOpen(b.name)
			log.L().Warn().Str("breaker", b.name).Msg("circuit breaker RE-OPENED (probe failed)")
		}
		return
	}

	b.counts.add(success)

	if success {
		b.consecutive = 0
	} else {
		b.consecutive++
	}

	if b.state == VODStateClosed {
		if b.consecutive >= b.cfg.Consecutive {
			b.trip("consecutive_failures")
			return
		}

		totalS, totalF := b.counts.stats()
		total := totalS + totalF
		if total >= b.cfg.MinRequests {
			rate := float64(totalF) / float64(total)
			if rate > b.cfg.FailureRate {
				b.trip("failure_rate")
			}
		}
	}
}

func (b *VODBreaker) trip(reason string) {
	b.state = VODStateOpen
	b.expiry = time.Now().Add(b.cfg.RetryAfter)
	metrics.IncVODCircuitTrips(reason)
	metrics.IncVODCircuitOpen(b.name)
	log.L().Error().
		Str("breaker", b.name).
		Str("reason", reason).
		Msg("circuit breaker TRIPPED (OPEN)")
}

// VODRegistry manages breakers by key.
type VODRegistry struct {
	mu       sync.RWMutex
	breakers map[string]*VODBreaker
}

var GlobalVODRegistry = &VODRegistry{
	breakers: make(map[string]*VODBreaker),
}

func GetOrRegisterVOD(name string, cfg VODConfig) *VODBreaker {
	GlobalVODRegistry.mu.Lock()
	defer GlobalVODRegistry.mu.Unlock()

	if b, ok := GlobalVODRegistry.breakers[name]; ok {
		return b
	}

	cfg.Name = name
	b := NewVODBreaker(cfg)
	GlobalVODRegistry.breakers[name] = b
	return b
}

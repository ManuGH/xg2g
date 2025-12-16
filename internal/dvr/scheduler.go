package dvr

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/rs/zerolog"
)

// Scheduler manages the periodic execution of the Series Engine.
type Scheduler struct {
	engine *SeriesEngine
	logger zerolog.Logger

	// Config
	BaseInterval time.Duration
	MaxInterval  time.Duration
	Jitter       time.Duration
	StartupDelay time.Duration

	// Dependencies
	clock Clock

	// State
	mu              sync.Mutex
	currentInterval time.Duration
}

// Clock interface for mocking time
type Clock interface {
	Now() time.Time
	NewTimer(d time.Duration) Timer
}

// Timer interface for mocking time.Timer
type Timer interface {
	C() <-chan time.Time
	Stop() bool
	Reset(d time.Duration) bool
}

// RealClock implements Clock using standard time package
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now() }
func (RealClock) NewTimer(d time.Duration) Timer {
	return &RealTimer{t: time.NewTimer(d)}
}

// RealTimer wraps time.Timer
type RealTimer struct {
	t *time.Timer
}

func (r *RealTimer) C() <-chan time.Time        { return r.t.C }
func (r *RealTimer) Stop() bool                 { return r.t.Stop() }
func (r *RealTimer) Reset(d time.Duration) bool { return r.t.Reset(d) }

// NewScheduler creates a new scheduler for the Series Engine.
func NewScheduler(engine *SeriesEngine) *Scheduler {
	return &Scheduler{
		engine:       engine,
		logger:       log.WithComponent("dvr.scheduler"),
		BaseInterval: 10 * time.Minute,
		MaxInterval:  60 * time.Minute,
		Jitter:       60 * time.Second,
		StartupDelay: 10 * time.Second,
		clock:        RealClock{},
	}
}

// Start begins the scheduling loop in a background goroutine.
// It returns immediately. The loop stops when ctx is cancelled.
func (s *Scheduler) Start(ctx context.Context) {
	go s.loop(ctx)
}

func (s *Scheduler) loop(ctx context.Context) {
	s.logger.Info().Msg("Series Scheduler started")

	// Use the clock dependency to create the timer
	timer := s.clock.NewTimer(s.nextDuration(true))
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info().Msg("Series Scheduler stopping")
			return
		case <-timer.C():
			// Execute Run
			s.logger.Debug().Msg("Triggering Series Engine run")

			// We use a separate context for the actual run execution to ensure it completes
			// even if the scheduler loop is shutting down, OR we respect the parent context?
			// Respecting parent context is saftey.

			reports, err := s.engine.RunOnce(ctx, "auto", "")

			// Backoff Logic & Telemetry
			if err != nil {
				s.logger.Error().Err(err).Msg("Series Engine run failed, backing off")
				s.increaseBackoff()
			} else {
				hasCriticalError := false

				// Log One-Line Summary per Rule Report
				for _, r := range reports {
					hasRun := r.Summary.TimersAttempted > 0 || r.Summary.EpgItemsMatched > 0 || r.Summary.MaxTimersGlobalPerRunHit

					// Should we log every run even if nothing happened?
					// "One line per run": The user likely means per Engine Execution or per Rule execution?
					// "runId, ruleId..." suggests per rule.
					// But if we have 50 rules and 0 matches, 50 log lines every 10 mins might be noisy.
					// Standard practice: Log if Action Taken or Error or Conflict.
					// If pure check/idle, log debug or summary.

					if hasRun || r.Status != "success" || r.Summary.ReceiverUnreachable {
						evt := s.logger.Info()
						if r.Status != "success" {
							evt = s.logger.Warn()
						}

						evt.
							Str("run_id", r.RunID).
							Str("rule_id", r.RuleID).
							Int("created", r.Summary.TimersCreated).
							Int("skipped", r.Summary.TimersSkipped).
							Int("conflicts", r.Summary.TimersConflicted).
							Int("errors", r.Summary.TimersErrored).
							Int64("duration_ms", r.DurationMs).
							Bool("receiver_unreachable", r.Summary.ReceiverUnreachable).
							Msg("Series Rule Executed")
					} else {
						// Debug for idle runs
						s.logger.Debug().
							Str("rule_id", r.RuleID).
							Msg("Series Rule Idle")
					}

					if r.Summary.ReceiverUnreachable {
						hasCriticalError = true
					}
				}

				if hasCriticalError {
					s.logger.Warn().Msg("Series Engine run had critical errors (Receiver Unreachable), backing off")
					s.increaseBackoff()
				} else {
					s.resetBackoff()
				}
			}

			// Schedule next run
			timer.Reset(s.nextDuration(false))
		}
	}
}

func (s *Scheduler) nextDuration(isFirst bool) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()

	if isFirst {
		return s.StartupDelay + s.jitterDuration()
	}

	interval := s.currentInterval
	if interval == 0 {
		interval = s.BaseInterval
	}

	// Add Jitter
	return interval + s.jitterDuration()
}

func (s *Scheduler) jitterDuration() time.Duration {
	// Random duration between -Jitter and +Jitter
	if s.Jitter == 0 {
		return 0
	}
	ms := int64(s.Jitter / time.Millisecond)
	delta := rand.Int63n(ms*2) - ms // -ms to +ms
	return time.Duration(delta) * time.Millisecond
}

func (s *Scheduler) increaseBackoff() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.currentInterval == 0 {
		s.currentInterval = s.BaseInterval
	}

	s.currentInterval *= 2
	if s.currentInterval > s.MaxInterval {
		s.currentInterval = s.MaxInterval
	}
	s.logger.Info().Str("next_interval", s.currentInterval.String()).Msg("Increased scheduler backoff")
}

func (s *Scheduler) resetBackoff() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.currentInterval != s.BaseInterval {
		s.logger.Info().Str("next_interval", s.BaseInterval.String()).Msg("Reset scheduler backoff")
		s.currentInterval = s.BaseInterval
	}
}

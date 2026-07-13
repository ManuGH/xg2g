package playbackshadow

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/playbackplanner"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
)

// ShadowObservation represents a single playback planning attempt to be evaluated by the Shadow Mode Observer.
type ShadowObservation struct {
	Evidence playbackplanner.PlaybackEvidence
	Legacy   ComparablePlaybackPlan
}

// PlannerShadowObserver allows recording a new observation without blocking.
type PlannerShadowObserver interface {
	TryObserve(obs ShadowObservation) bool
}

// ObserverConfig configures the shadow mode worker.
type ObserverConfig struct {
	Enabled       bool `json:"enabled"`
	QueueCapacity int  `json:"queueCapacity"`
}

// Worker is the background component evaluating shadow planner decisions and recording metrics.
type Worker struct {
	config    ObserverConfig
	queue     chan ShadowObservation
	done      chan struct{}
	activeCtx context.Context
	planner   func(playbackplanner.PlaybackEvidence) (playbackplanner.PlanningResult, error) // Injectable for tests
	logger    zerolog.Logger

	mu      sync.Mutex
	wg      sync.WaitGroup
	started bool
	closed  bool

	diffLogCount    atomic.Uint64
	lastDiffLogNano atomic.Int64

	observationsTotal     *prometheus.CounterVec
	diffsTotal            *prometheus.CounterVec
	acceptedDiffsTotal    *prometheus.CounterVec
	unexplainedDiffsTotal *prometheus.CounterVec
	errorsTotal           *prometheus.CounterVec
	queueDroppedTotal     prometheus.Counter
	durationSeconds       prometheus.Histogram
}

// NoopObserver is used when shadow mode is disabled.
type NoopObserver struct{}

// TryObserve does nothing for NoopObserver.
func (n NoopObserver) TryObserve(obs ShadowObservation) bool {
	return false
}

// NewWorker initializes a new shadow observer worker but does not start it yet.
func NewWorker(config ObserverConfig, reg prometheus.Registerer, logger zerolog.Logger) (*Worker, error) {
	if config.QueueCapacity <= 0 {
		config.QueueCapacity = 256
	}

	w := &Worker{
		config:  config,
		queue:   make(chan ShadowObservation, config.QueueCapacity),
		done:    make(chan struct{}),
		planner: playbackplanner.Plan,
		logger:  logger.With().Str("component", "planner_shadow").Logger(),

		observationsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "xg2g",
				Subsystem: "planner_shadow",
				Name:      "observations_total",
				Help:      "Total number of playback shadow observations evaluated.",
			},
			[]string{"legacy_outcome", "planner_outcome", "scope"},
		),
		diffsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "xg2g",
				Subsystem: "planner_shadow",
				Name:      "diffs_total",
				Help:      "Total number of diverging attributes between legacy and new planner.",
			},
			[]string{"diff_type", "scope"},
		),
		acceptedDiffsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "xg2g",
				Subsystem: "planner_shadow",
				Name:      "accepted_diffs_total",
				Help:      "Total number of explicitly classified and accepted planner shadow differences.",
			},
			[]string{"diff_type", "scope"},
		),
		unexplainedDiffsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "xg2g",
				Subsystem: "planner_shadow",
				Name:      "unexplained_diffs_total",
				Help:      "Total number of planner shadow differences that block cutover.",
			},
			[]string{"diff_type", "scope"},
		),
		errorsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "xg2g",
				Subsystem: "planner_shadow",
				Name:      "errors_total",
				Help:      "Total number of errors encountered during shadow planning.",
			},
			[]string{"error_code", "scope"},
		),
		queueDroppedTotal: prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: "xg2g",
				Subsystem: "planner_shadow",
				Name:      "queue_dropped_total",
				Help:      "Total number of observations dropped due to full queue.",
			},
		),
		durationSeconds: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Namespace: "xg2g",
				Subsystem: "planner_shadow",
				Name:      "duration_seconds",
				Help:      "Duration of local shadow planning evaluations.",
				Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
			},
		),
	}

	if reg != nil {
		var err error
		w.observationsTotal, err = registerOrGet(reg, w.observationsTotal, (*prometheus.CounterVec)(nil))
		if err != nil {
			return nil, err
		}
		w.diffsTotal, err = registerOrGet(reg, w.diffsTotal, (*prometheus.CounterVec)(nil))
		if err != nil {
			return nil, err
		}
		w.acceptedDiffsTotal, err = registerOrGet(reg, w.acceptedDiffsTotal, (*prometheus.CounterVec)(nil))
		if err != nil {
			return nil, err
		}
		w.unexplainedDiffsTotal, err = registerOrGet(reg, w.unexplainedDiffsTotal, (*prometheus.CounterVec)(nil))
		if err != nil {
			return nil, err
		}
		w.errorsTotal, err = registerOrGet(reg, w.errorsTotal, (*prometheus.CounterVec)(nil))
		if err != nil {
			return nil, err
		}
		w.queueDroppedTotal, err = registerOrGet(reg, w.queueDroppedTotal, (prometheus.Counter)(nil))
		if err != nil {
			return nil, err
		}
		w.durationSeconds, err = registerOrGet(reg, w.durationSeconds, (prometheus.Histogram)(nil))
		if err != nil {
			return nil, err
		}
	}

	return w, nil
}

func registerOrGet[T prometheus.Collector](reg prometheus.Registerer, c T, _ T) (T, error) {
	if err := reg.Register(c); err != nil {
		if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
			if existing, ok := are.ExistingCollector.(T); ok {
				return existing, nil
			}
			var zero T
			return zero, fmt.Errorf("collector registered under different type: %T vs %T", are.ExistingCollector, zero)
		}
		var zero T
		return zero, err
	}
	return c, nil
}

// TryObserve enqueues an observation non-blockingly. Returns false if the queue was full or closed.
func (w *Worker) TryObserve(obs ShadowObservation) bool {
	select {
	case <-w.done:
		return false
	default:
	}

	select {
	case <-w.done:
		return false
	case w.queue <- obs:
		return true
	default:
		w.queueDroppedTotal.Inc()
		return false
	}
}

// Start begins processing observations in the background. If called after a previous context cancelled, it rebinds and restarts processing.
func (w *Worker) Start(ctx context.Context) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	// If already started and active context is not done, return.
	if w.started && w.activeCtx != nil && w.activeCtx.Err() == nil {
		w.mu.Unlock()
		return
	}
	wasStarted := w.started
	w.started = true
	w.activeCtx = ctx
	w.mu.Unlock()

	if wasStarted {
		w.wg.Wait()
	}

	w.wg.Add(1)
	go func() {
		defer w.wg.Done()

		for {
			select {
			case <-ctx.Done():
				// Context cancelled, exit immediately. Queued items waiting are dropped (not drained).
				return
			case <-w.done:
				// Worker closed explicitly. Queued items waiting are dropped (not drained).
				return
			case obs, ok := <-w.queue:
				if !ok {
					return
				}
				w.processOne(obs)
			}
		}
	}()
}

// Close immediately stops the worker and waits for active processing of the current item to complete.
// Note: pending observations waiting in the queue upon Close are dropped and not drained.
func (w *Worker) Close(ctx context.Context) error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	close(w.done)
	started := w.started
	w.mu.Unlock()

	if !started {
		return nil
	}

	// Wait for processing goroutine to finish or context to cancel
	waitChan := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(waitChan)
	}()

	select {
	case <-waitChan:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Worker) processOne(obs ShadowObservation) {
	defer func() {
		if r := recover(); r != nil {
			w.logger.Error().
				Interface("panic", r).
				Str("stack", string(debug.Stack())).
				Msg("Recovered panic in planner shadow worker")
			w.errorsTotal.WithLabelValues("panic", obs.Evidence.Scope).Inc()
		}
	}()

	start := time.Now()
	res, err := w.planner(obs.Evidence)
	w.durationSeconds.Observe(time.Since(start).Seconds())

	if err != nil {
		// Log predefined error codes, never raw error text as label.
		errCode := "unknown_error"
		if errors.Is(err, playbackplanner.ErrRuleNotImplemented) {
			errCode = "rule_not_implemented"
		} else if errors.Is(err, playbackplanner.ErrInvalidEvidence) {
			errCode = "invalid_evidence"
		}
		w.errorsTotal.WithLabelValues(errCode, obs.Evidence.Scope).Inc()
		return
	}

	plannerComp := ComparableFromPlanner(res.Plan)

	w.observationsTotal.WithLabelValues(obs.Legacy.Outcome, plannerComp.Outcome, obs.Evidence.Scope).Inc()

	if !obs.Legacy.IsValid {
		// If legacy had an invalid state prior to planning (e.g. failed earlier), it's not a real diff.
		return
	}

	classified := ClassifyComparableDiffs(obs.Legacy, plannerComp)
	unexplained := make([]string, 0, len(classified))
	for _, diff := range classified {
		w.diffsTotal.WithLabelValues(diff.Code, obs.Evidence.Scope).Inc()
		if diff.Disposition == DiffAccepted {
			w.acceptedDiffsTotal.WithLabelValues(diff.Code, obs.Evidence.Scope).Inc()
			continue
		}
		w.unexplainedDiffsTotal.WithLabelValues(diff.Code, obs.Evidence.Scope).Inc()
		unexplained = append(unexplained, diff.Code)
	}

	if len(unexplained) > 0 && w.shouldLogDiff() {
		evHash, _ := obs.Evidence.Hash()
		w.logger.Warn().
			Str("scope", obs.Evidence.Scope).
			Strs("diff_codes", unexplained).
			Str("legacy_outcome", obs.Legacy.Outcome).
			Str("planner_outcome", plannerComp.Outcome).
			Str("legacy_mode", obs.Legacy.Mode).
			Str("planner_mode", plannerComp.Mode).
			Str("legacy_engine", obs.Legacy.Engine).
			Str("planner_engine", plannerComp.Engine).
			Str("legacy_packaging", obs.Legacy.Container).
			Str("planner_packaging", plannerComp.Container).
			Str("planner_reason_code", res.Plan.ReasonCode).
			Str("planner_version", res.Trace.PlannerVersion).
			Str("policy_version", obs.Evidence.PolicyVersion).
			Str("evidence_hash", evHash).
			Msg("Planner shadow observation diff detected")
	}
}

func (w *Worker) shouldLogDiff() bool {
	count := w.diffLogCount.Add(1)
	if count > 5 && count%10 != 0 {
		return false
	}
	now := time.Now().UnixNano()
	for {
		last := w.lastDiffLogNano.Load()
		if now-last < 500*1000*1000 {
			return false // Rate limit: max 2 logs/sec across worker
		}
		if w.lastDiffLogNano.CompareAndSwap(last, now) {
			return true
		}
	}
}

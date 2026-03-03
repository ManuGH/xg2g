package verification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"sync/atomic"
	"time"

	xglog "github.com/ManuGH/xg2g/internal/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"
)

// Checker performs a specific verification logic.
// It returns a list of mismatches found, or error if the check execution failed.
type Checker interface {
	Check(ctx context.Context) ([]Mismatch, error)
}

// Worker manages the periodic verification loop.
type Worker struct {
	store    Store
	checkers []Checker
	cadence  time.Duration
	busy     atomic.Bool
	lastHash atomic.Value // types: string

	// Observability
	logger     zerolog.Logger
	lastState  atomic.Value // DriftState tracked for edge-triggered logging
	driftGauge *prometheus.GaugeVec
}

var (
	// Metric definition (singleton registration)
	driftGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "xg2g_drift_detected",
		Help: "Indicates if drift is detected (1) or clean (0) per kind.",
	}, []string{"kind"})
)

// NewWorker creates a new verification worker.
func NewWorker(store Store, cadence time.Duration, checkers ...Checker) *Worker {
	w := &Worker{
		store:      store,
		cadence:    cadence,
		checkers:   checkers,
		logger:     xglog.WithComponent("verification"),
		driftGauge: driftGauge,
	}
	// Seal-Check 1: Type Safety
	w.lastState.Store(DriftState{})
	// Ensure metrics are initialized
	InitMetrics()
	return w
}

// InitMetrics sets all known drift metrics to 0 (Clean).
// Call this on startup to ensure no "missing data" gaps / stale alerts.
func InitMetrics() {
	driftGauge.WithLabelValues(string(KindConfig)).Set(0)
	driftGauge.WithLabelValues(string(KindRuntime)).Set(0)
	driftGauge.WithLabelValues(string(KindBinary)).Set(0)
}

// Start begins the verification loop. It blocks until context is canceled.
func (w *Worker) Start(ctx context.Context) {
	ticker := time.NewTicker(w.cadence)
	defer ticker.Stop()

	// Initial run directly
	w.tryRun(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Run synchronously in the loop.
			// tryRun is protected by atomic busy flag, so it will skip if already running.
			// Ticker will naturally drop ticks if we block here longer than cadence.
			w.tryRun(ctx)
		}
	}
}

// tryRun attempts to run checking logic if not already running.
func (w *Worker) tryRun(ctx context.Context) {
	if !w.busy.CompareAndSwap(false, true) {
		return // Skip if busy
	}
	defer w.busy.Store(false)

	w.runOnce(ctx)
}

func (w *Worker) runOnce(ctx context.Context) {
	// Bounded execution time for checks
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var allMismatches []Mismatch

	for _, c := range w.checkers {
		if ctx.Err() != nil {
			return
		}
		mismatches, err := c.Check(ctx)
		if err != nil {
			// Report error as a mismatch to ensure visibility directly in status
			allMismatches = append(allMismatches, Mismatch{
				Kind:     KindRuntime,
				Key:      "verification.error",
				Expected: "success",
				Actual:   err.Error(),
			})
			continue
		}
		allMismatches = append(allMismatches, mismatches...)
	}

	newState := DriftState{
		Version:    1,
		LastCheck:  time.Now().UTC(),
		Mismatches: allMismatches,
		Detected:   len(allMismatches) > 0,
	}

	normalizeState(&newState)

	// Change Detection & Heartbeat Logic
	shouldWrite := false

	contentHash := hashContent(newState)
	lastContentHash, _ := w.lastHash.Load().(string)

	if contentHash != lastContentHash {
		shouldWrite = true
	} else {
		// Content unchanged, check heartbeat (5x cadence)
		// Get last persisted state to check timestamp
		oldState, ok := w.store.Get(ctx)
		if !ok {
			shouldWrite = true
		} else {
			if time.Since(oldState.LastCheck) >= w.cadence*5 {
				shouldWrite = true
			}
		}
	}

	w.handleStateChange(newState) // Log & Metrics (Decoupled from store write)
	w.updateMetrics(newState)     // Metrics update
	w.lastState.Store(newState)   // Update in-memory state

	if shouldWrite {
		if err := w.store.Set(ctx, newState); err == nil {
			w.lastHash.Store(contentHash)
		}
	}
}

func (w *Worker) handleStateChange(newState DriftState) {
	// Detect edges per kind
	currentKinds := countKinds(newState)

	// Safe load of last state (initialized in NewWorker, safe cast)
	lastKinds := countKinds(w.lastState.Load().(DriftState))

	// Check for introduced drift
	for k, count := range currentKinds {
		if lastKinds[k] == 0 {
			w.logger.Warn().
				Str("event", "drift_detected").
				Str("kind", string(k)).
				Int("mismatches", count).
				Msg("verification drift detected")
		}
	}

	// Check for resolved drift
	for k := range lastKinds {
		if currentKinds[k] == 0 {
			w.logger.Info().
				Str("event", "drift_resolved").
				Str("kind", string(k)).
				Msg("verification drift resolved")
		}
	}
}

func (w *Worker) updateMetrics(state DriftState) {
	counts := countKinds(state)
	// Known kinds to always report (or just report what we see + explicit 0 for resolved?)
	// User said "xg2g_drift_detected{kind=config} 0|1".
	// We should probably init the gauge with 0 for all KNOWN kinds if possible, but dynamic is safer.
	// For now, iterate known kinds from counts + last state to ensure 0s are set.

	// Union of all kinds we've ever seen?
	// Or just "config", "runtime"?
	// Let's rely on constants if possible, but map iteration is generic.
	allKinds := map[MismatchKind]bool{
		KindConfig:  true,
		KindRuntime: true,
		KindBinary:  true,
	}
	// Add any dynamic ones encountered
	for k := range counts {
		allKinds[k] = true
	}

	for k := range allKinds {
		val := 0.0
		if counts[k] > 0 {
			val = 1.0
		}
		w.driftGauge.WithLabelValues(string(k)).Set(val)
	}
}

func countKinds(st DriftState) map[MismatchKind]int {
	m := make(map[MismatchKind]int)
	for _, mis := range st.Mismatches {
		m[mis.Kind]++
	}
	return m
}

// normalizeState sorts mismatches to ensure determinism.
func normalizeState(st *DriftState) {
	sort.Slice(st.Mismatches, func(i, j int) bool {
		mi, mj := st.Mismatches[i], st.Mismatches[j]
		if mi.Kind != mj.Kind {
			return mi.Kind < mj.Kind
		}
		if mi.Key != mj.Key {
			return mi.Key < mj.Key
		}
		if mi.Expected != mj.Expected {
			return mi.Expected < mj.Expected
		}
		return mi.Actual < mj.Actual
	})
}

// hashContent computes a hash of the state IGNORING LastCheck.
func hashContent(st DriftState) string {
	clone := st
	clone.LastCheck = time.Time{}
	bytes, err := json.Marshal(clone)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(bytes)
	return hex.EncodeToString(sum[:])
}

package verification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"sync/atomic"
	"time"
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
}

// NewWorker creates a new verification worker.
func NewWorker(store Store, cadence time.Duration, checkers ...Checker) *Worker {
	return &Worker{
		store:    store,
		cadence:  cadence,
		checkers: checkers,
	}
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

	if shouldWrite {
		if err := w.store.Set(ctx, newState); err == nil {
			w.lastHash.Store(contentHash)
		}
	}
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
	bytes, _ := json.Marshal(clone)
	sum := sha256.Sum256(bytes)
	return hex.EncodeToString(sum[:])
}

// hashState computes hash of full state (unused if we split logic)
// Deprecated: used hashContent instead
func hashState(st DriftState) string {
	bytes, _ := json.Marshal(st)
	sum := sha256.Sum256(bytes)
	return hex.EncodeToString(sum[:])
}

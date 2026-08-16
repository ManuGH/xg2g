// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package epg

import (
	"context"
	"fmt"
	"sync"
	"time"

	xglog "github.com/ManuGH/xg2g/internal/log"
)

// EnrichmentStoreReader is the minimal store interface required by the queue worker.
type EnrichmentStoreReader interface {
	Get(ctx context.Context, fp ProgrammeFingerprint) (*EnrichmentData, bool, error)
	Put(ctx context.Context, data *EnrichmentData) error
}

// QueueConfig holds operational parameters for the enrichment queue.
type QueueConfig struct {
	Capacity    int           // Channel buffer capacity (e.g. 500)
	WorkerCount int           // Number of concurrent worker goroutines
	JobTimeout  time.Duration // Max duration for a single enrichment lookup
}

// DefaultQueueConfig returns conservative, safe defaults for background processing.
func DefaultQueueConfig() QueueConfig {
	return QueueConfig{
		Capacity:    500,
		WorkerCount: 1, // Start with single worker for polite rate control
		JobTimeout:  10 * time.Second,
	}
}

// EnrichmentQueue manages non-blocking job intake, in-flight deduplication, and worker lifecycle.
type EnrichmentQueue struct {
	cfg        QueueConfig
	store      EnrichmentStoreReader
	provider   MetadataProvider
	jobs       chan ProgrammeFingerprint
	activeKeys map[string]struct{}
	mu         sync.Mutex

	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	isRunning bool
	isStopped bool
}

// NewEnrichmentQueue creates a new deduplicated enrichment queue.
func NewEnrichmentQueue(cfg QueueConfig, store EnrichmentStoreReader, provider MetadataProvider) *EnrichmentQueue {
	if cfg.Capacity <= 0 {
		cfg.Capacity = 500
	}
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = 1
	}
	if cfg.JobTimeout <= 0 {
		cfg.JobTimeout = 10 * time.Second
	}

	return &EnrichmentQueue{
		cfg:        cfg,
		store:      store,
		provider:   provider,
		jobs:       make(chan ProgrammeFingerprint, cfg.Capacity),
		activeKeys: make(map[string]struct{}),
	}
}

// Start launches background worker goroutines bound to the provided context.
func (q *EnrichmentQueue) Start(parentCtx context.Context) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.isRunning {
		return fmt.Errorf("enrichment queue already running")
	}
	if q.isStopped {
		return fmt.Errorf("enrichment queue has been stopped")
	}

	q.ctx, q.cancel = context.WithCancel(parentCtx)
	q.isRunning = true

	for i := 0; i < q.cfg.WorkerCount; i++ {
		q.wg.Add(1)
		go q.workerLoop(i)
	}

	return nil
}

// Enqueue attempts non-blocking job intake with in-flight deduplication.
// Returns (enqueued, reason):
// - (true, ""): Successfully queued.
// - (false, "already_active"): Key is already in-flight or in queue.
// - (false, "queue_full"): Capacity exceeded, key dropped (retryable on next EPG cycle).
// - (false, "stopped"): Queue is shut down.
func (q *EnrichmentQueue) Enqueue(fp ProgrammeFingerprint) (bool, string) {
	key := fp.Key()

	q.mu.Lock()
	if !q.isRunning || q.isStopped {
		q.mu.Unlock()
		return false, "stopped"
	}

	if _, exists := q.activeKeys[key]; exists {
		q.mu.Unlock()
		return false, "already_active"
	}

	// Try non-blocking send
	select {
	case q.jobs <- fp:
		q.activeKeys[key] = struct{}{}
		q.mu.Unlock()
		return true, ""
	default:
		// Queue full: do NOT leak key in activeKeys
		q.mu.Unlock()
		return false, "queue_full"
	}
}

func (q *EnrichmentQueue) workerLoop(workerID int) {
	defer q.wg.Done()

	for {
		select {
		case <-q.ctx.Done():
			return
		case fp, ok := <-q.jobs:
			if !ok {
				return
			}
			q.processJob(fp)
		}
	}
}

func (q *EnrichmentQueue) processJob(fp ProgrammeFingerprint) {
	key := fp.Key()
	defer func() {
		// INVARIANT: Deduplication key is ALWAYS released, even on panic or error
		q.mu.Lock()
		delete(q.activeKeys, key)
		q.mu.Unlock()
	}()

	logger := xglog.FromContext(q.ctx)

	// 1. Check Store first: if already cached and valid, skip provider lookup
	if q.store != nil {
		if _, found, err := q.store.Get(q.ctx, fp); err == nil && found {
			return
		}
	}

	if q.provider == nil {
		return
	}

	// 2. Perform provider lookup with isolated job timeout
	jobCtx, jobCancel := context.WithTimeout(q.ctx, q.cfg.JobTimeout)
	defer jobCancel()

	data, err := q.provider.Lookup(jobCtx, fp)
	if err != nil {
		if jobCtx.Err() != nil {
			logger.Debug().Str("key", key).Msg("Enrichment lookup timed out or canceled")
		} else {
			logger.Warn().Err(err).Str("key", key).Msg("Enrichment provider lookup failed")
		}
		return
	}

	if data == nil {
		return
	}

	// 3. Persist valid matches or deterministic negative matches to store
	if q.store != nil && (data.Status == MatchStatusFound || data.Status == MatchStatusNoMatch) {
		if putErr := q.store.Put(q.ctx, data); putErr != nil {
			logger.Warn().Err(putErr).Str("key", key).Msg("Failed to persist enrichment record")
		}
	}
}

// Stop cleanly terminates workers, cancels in-flight jobs, and waits for all goroutines.
func (q *EnrichmentQueue) Stop() {
	q.mu.Lock()
	if !q.isRunning || q.isStopped {
		q.mu.Unlock()
		return
	}
	q.isStopped = true
	q.isRunning = false
	q.cancel()
	q.mu.Unlock()

	q.wg.Wait()
}

// PendingCount returns the number of jobs currently waiting in the channel buffer.
func (q *EnrichmentQueue) PendingCount() int {
	return len(q.jobs)
}

// ActiveKeyCount returns the number of unique fingerprints currently queued or in-flight.
func (q *EnrichmentQueue) ActiveKeyCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.activeKeys)
}

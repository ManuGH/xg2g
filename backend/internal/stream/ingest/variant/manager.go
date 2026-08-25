// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package variant

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
)

// AudioVariantManager manages active AudioVariantWorkers for a single SessionPipeline.
// It ensures that multiple concurrent clients requesting the same variant key share
// the exact same transcode worker, avoiding duplicate FFmpeg processes and CPU overhead.
type AudioVariantManager struct {
	mu         sync.Mutex
	masterRing *ring.MasterRing
	workers    map[string]*AudioVariantWorker
	closed     bool
}

// NewAudioVariantManager creates a manager downstream of the given MasterRing.
func NewAudioVariantManager(masterRing *ring.MasterRing) *AudioVariantManager {
	return &AudioVariantManager{
		masterRing: masterRing,
		workers:    make(map[string]*AudioVariantWorker),
	}
}

// GetOrCreateWorker retrieves an existing running worker for the given key, or spawns and starts a new one.
// The returned worker has its subscriber count incremented. The caller MUST call ReleaseWorker when done.
func (m *AudioVariantManager) GetOrCreateWorker(ctx context.Context, key AudioVariantKey) (*AudioVariantWorker, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, ErrWorkerClosed
	}

	keyStr := key.String()
	if worker, exists := m.workers[keyStr]; exists {
		if !worker.Terminated() {
			worker.AddSubscriber()
			return worker, nil
		}

		// A worker serves exactly one upstream generation. Once it has ended, the
		// cached entry is a closed VariantRing and a dead process, so it is
		// replaced rather than handed out. Which of the two reasons it was is worth
		// keeping apart: a topology change is the system working as designed, a
		// failure is not, and a PMT bump must not read as a crash.
		reason := worker.Err()
		event := log.L().Warn()
		if errors.Is(reason, ErrUpstreamGenerationChanged) {
			event = log.L().Info()
		}
		event.Err(reason).
			Str("variant", keyStr).
			Msg("replacing terminated variant worker")

		worker.Stop()
		delete(m.workers, keyStr)
	}

	worker := NewAudioVariantWorker(key, m.masterRing, 8*1024*1024)
	worker.AddSubscriber()
	worker.Start(ctx)

	m.workers[keyStr] = worker
	return worker, nil
}

// ReleaseWorker decrements the subscriber count on the worker currently mapped to
// the given key.
//
// Prefer ReleaseWorkerInstance when the caller still holds the worker it attached
// to. A key no longer identifies one worker for all time: a generation cut replaces
// the entry, and releasing by key then credits the decrement to the replacement -
// pushing a worker toward idle on behalf of subscribers that never used it.
func (m *AudioVariantManager) ReleaseWorker(key AudioVariantKey) {
	m.mu.Lock()
	defer m.mu.Unlock()

	keyStr := key.String()
	if worker, exists := m.workers[keyStr]; exists {
		worker.RemoveSubscriber()
	}
}

// ReleaseWorkerInstance decrements the subscriber count on the worker the caller
// actually attached to, whether or not it is still the one mapped to its key.
func (m *AudioVariantManager) ReleaseWorkerInstance(worker *AudioVariantWorker) {
	if worker == nil {
		return
	}
	worker.RemoveSubscriber()
}

// CleanupIdle stops and removes any workers that have had 0 active subscribers for at least idleTimeout.
func (m *AudioVariantManager) CleanupIdle(idleTimeout time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for keyStr, worker := range m.workers {
		if worker.IdleDuration() >= idleTimeout {
			worker.Stop()
			delete(m.workers, keyStr)
		}
	}
}

// ActiveWorkerCount returns the number of active variant workers.
func (m *AudioVariantManager) ActiveWorkerCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.workers)
}

// Close stops all running variant workers and shuts down the manager.
func (m *AudioVariantManager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	workersToStop := make([]*AudioVariantWorker, 0, len(m.workers))
	for _, w := range m.workers {
		workersToStop = append(workersToStop, w)
	}
	m.workers = make(map[string]*AudioVariantWorker)
	m.mu.Unlock()

	for _, w := range workersToStop {
		w.Stop()
	}
}

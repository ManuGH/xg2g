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

	// workerCtx is the lifetime every worker runs on, and it belongs to this
	// manager rather than to whichever subscriber happened to create the worker.
	//
	// Starting a worker on a caller's context made an FFmpeg process the property
	// of one client: when that client's HTTP request ended, its context was
	// cancelled, exec.CommandContext killed the process, and every other client
	// coalesced onto the same worker lost its stream - reported as a clean shutdown,
	// because from the process's point of view it was one. Sharing a worker is the
	// entire purpose of this manager, so its lifetime cannot hang off one of the
	// things being shared between.
	workerCtx      context.Context
	stopAllWorkers context.CancelFunc
}

// NewAudioVariantManager creates a manager downstream of the given MasterRing.
func NewAudioVariantManager(masterRing *ring.MasterRing) *AudioVariantManager {
	workerCtx, stopAllWorkers := context.WithCancel(context.Background())
	return &AudioVariantManager{
		masterRing:     masterRing,
		workers:        make(map[string]*AudioVariantWorker),
		workerCtx:      workerCtx,
		stopAllWorkers: stopAllWorkers,
	}
}

// GetOrCreateWorker retrieves an existing running worker for the given key, or spawns and starts a new one.
// The returned worker has its subscriber count incremented. The caller MUST call
// ReleaseWorkerInstance when done.
//
// ctx is the caller's, and it governs only this call: a caller that has already
// given up does not get an FFmpeg process started on its behalf. It deliberately
// does not govern the worker, which outlives any single subscriber and is stopped
// by ReleaseWorkerInstance at refcount zero, by Close, by a topology generation
// cut, or by its own failure.
func (m *AudioVariantManager) GetOrCreateWorker(ctx context.Context, key AudioVariantKey) (*AudioVariantWorker, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

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
	worker.Start(m.workerCtx)

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
	worker := m.workers[key.String()]
	m.mu.Unlock()

	m.ReleaseWorkerInstance(worker)
}

// ReleaseWorkerInstance releases one subscriber's hold on the worker it actually
// attached to, whether or not it is still the one mapped to its key.
//
// The last hold to go stops the worker. That is the whole reclaim path: a
// subscriber's own context ends its own subscription and nothing more, so the
// refcount is what decides when the process is no longer needed. Without this the
// ownership fix would trade one fault for another - no client could kill a shared
// worker any more, and nothing else would ever reclaim one either, because
// CleanupIdle has no caller.
func (m *AudioVariantManager) ReleaseWorkerInstance(worker *AudioVariantWorker) {
	if worker == nil {
		return
	}

	m.mu.Lock()
	worker.RemoveSubscriber()
	reclaim := worker.SubscriberCount() == 0
	if reclaim {
		// Only if it is still the mapped one. After a generation cut the key points
		// at the replacement, and the entry being dropped here must not be it.
		keyStr := worker.Key().String()
		if current, ok := m.workers[keyStr]; ok && current == worker {
			delete(m.workers, keyStr)
		}
	}
	m.mu.Unlock()

	// Outside the lock: Stop waits for the process to go, and holding the manager
	// mutex across that would block every other client's attach behind one
	// disconnect.
	if reclaim {
		worker.Stop()
	}
}

// CleanupIdle stops and removes any workers that have had 0 active subscribers for
// at least idleTimeout.
//
// It has no production caller, and with reclaim happening at refcount zero it
// normally finds nothing: a worker does not survive its last subscriber long
// enough to become idle. It is kept as a sweep for a worker whose release path was
// somehow missed, and is where a warm-hold policy would go if variant workers ever
// need to outlive a zap the way ingest sessions already do.
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
	m.stopAllWorkers()
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

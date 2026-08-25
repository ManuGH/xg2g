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

const (
	// idleWorkerTimeout is how long a worker with no subscribers is kept before it
	// is stopped. A worker costs one FFmpeg process, so it cannot be held
	// indefinitely - and it is not free to recreate either, being a process start
	// plus the wait for the next random access point, so a client that switches
	// audio track and back finds it still running. The ingest sessions upstream
	// hold open for a comparable window for the same reason.
	idleWorkerTimeout = 10 * time.Second

	// idleSweepInterval is how often idle workers are looked for. The last
	// subscriber's release is deliberately not the moment to reap: the point of the
	// timeout is the window after it.
	idleSweepInterval = 2 * time.Second
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

	// The idle policy is what reclaims a worker now that no subscriber's context
	// can. It is held per manager rather than as a constant so a test does not have
	// to wait out the production timeout to observe it.
	idleTimeout   time.Duration
	sweepInterval time.Duration
	sweepDone     chan struct{}
}

// NewAudioVariantManager creates a manager downstream of the given MasterRing.
func NewAudioVariantManager(masterRing *ring.MasterRing) *AudioVariantManager {
	return newAudioVariantManager(masterRing, idleWorkerTimeout, idleSweepInterval)
}

// newAudioVariantManager takes the idle policy as parameters so it can be exercised
// without a test waiting out the production timeout.
func newAudioVariantManager(masterRing *ring.MasterRing, idleTimeout, sweepInterval time.Duration) *AudioVariantManager {
	workerCtx, stopAllWorkers := context.WithCancel(context.Background())

	m := &AudioVariantManager{
		masterRing:     masterRing,
		workers:        make(map[string]*AudioVariantWorker),
		workerCtx:      workerCtx,
		stopAllWorkers: stopAllWorkers,
		idleTimeout:    idleTimeout,
		sweepInterval:  sweepInterval,
		sweepDone:      make(chan struct{}),
	}

	go m.sweepIdle()

	return m
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
// Releasing does not stop anything. Whether the worker is still wanted is a
// question about all of its subscribers, not about the one that just left, and
// answering it here would make a client that switches audio track pay for a new
// FFmpeg process every time. The idle policy decides, once the worker has actually
// been unwanted for a while.
func (m *AudioVariantManager) ReleaseWorkerInstance(worker *AudioVariantWorker) {
	if worker == nil {
		return
	}
	worker.RemoveSubscriber()
}

// CleanupIdle stops and removes any workers that have had 0 active subscribers for
// at least idleTimeout. It is driven by sweepIdle, and stays exported so a caller
// can force the policy without waiting for the next tick.
func (m *AudioVariantManager) CleanupIdle(idleTimeout time.Duration) {
	m.mu.Lock()
	var idle []*AudioVariantWorker
	for keyStr, worker := range m.workers {
		// Subscribed is not idle, whatever the timeout says. At idleTimeout 0 the
		// duration alone would match a worker clients are watching right now, which
		// is the failure the ownership rule exists to prevent, reached from the
		// other direction.
		if worker.SubscriberCount() > 0 {
			continue
		}
		if worker.IdleDuration() >= idleTimeout {
			idle = append(idle, worker)
			delete(m.workers, keyStr)
		}
	}
	m.mu.Unlock()

	// Stop outside the lock: it waits for FFmpeg to exit, and a client attaching to
	// an unrelated variant must not queue behind that. Removal happened under the
	// lock, so a worker being stopped can no longer be handed out.
	for _, worker := range idle {
		worker.Stop()
	}
}

// sweepIdle enforces the idle policy until the manager is closed. Nothing else
// drives it: a subscriber's own context ends its own subscription and nothing more.
func (m *AudioVariantManager) sweepIdle() {
	defer close(m.sweepDone)

	ticker := time.NewTicker(m.sweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.workerCtx.Done():
			return
		case <-ticker.C:
			m.CleanupIdle(m.idleTimeout)
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

	// Cancelling first starts every worker's teardown at once, so the Stop calls
	// below only wait for them rather than serialize them. The sweep is joined
	// before that, so it cannot be reaping into a map that is being torn down.
	m.stopAllWorkers()
	<-m.sweepDone

	for _, w := range workersToStop {
		w.Stop()
	}
}

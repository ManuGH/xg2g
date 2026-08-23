// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package variant

import (
	"context"
	"sync"
	"time"

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
		worker.AddSubscriber()
		return worker, nil
	}

	worker := NewAudioVariantWorker(key, m.masterRing, 8*1024*1024)
	worker.AddSubscriber()
	worker.Start(ctx)

	m.workers[keyStr] = worker
	return worker, nil
}

// ReleaseWorker decrements the subscriber count on the worker for the given key.
func (m *AudioVariantManager) ReleaseWorker(key AudioVariantKey) {
	m.mu.Lock()
	defer m.mu.Unlock()

	keyStr := key.String()
	if worker, exists := m.workers[keyStr]; exists {
		worker.RemoveSubscriber()
	}
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

package vod

import (
	"context"
	"errors"
	"fmt"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/singleflight"
	"sort"
	"sync"
)

// Manager handles VOD build jobs.
type Manager struct {
	runner     Runner
	prober     Prober
	mu         sync.Mutex
	jobs       map[string]*BuildMonitor
	metadata   map[string]Metadata // Cached artifact metadata
	probeCh    chan probeRequest
	workerWg   sync.WaitGroup
	buildWg    sync.WaitGroup
	sfg        singleflight.Group
	pathMapper PathMapper
	started    bool
	ctx        context.Context
	cancel     context.CancelFunc
}

// PathMapper abstracts local path resolution.
type PathMapper interface {
	ResolveLocalExisting(receiverPath string) (string, bool)
}

func NewManager(runner Runner, prober Prober, pathMapper PathMapper) (*Manager, error) {
	if runner == nil {
		return nil, errors.New("NewManager: runner is nil")
	}
	if prober == nil {
		return nil, errors.New("NewManager: prober is nil")
	}

	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		runner:     runner,
		prober:     prober,
		pathMapper: pathMapper,
		jobs:       make(map[string]*BuildMonitor),
		metadata:   make(map[string]Metadata),
		probeCh:    make(chan probeRequest, ProbeQueueSize),
		ctx:        ctx,
		cancel:     cancel,
	}

	return m, nil
}

// Shutdown stops the manager and cancels all background contexts.
func (m *Manager) Shutdown() {
	if m == nil {
		return
	}
	m.cancel()
	m.CancelAll()
	m.workerWg.Wait()
	m.buildWg.Wait()
}

// ShutdownContext stops all background work and waits for worker drain or context timeout.
func (m *Manager) ShutdownContext(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("shutdown context is nil")
	}
	m.cancel()
	m.CancelAll()

	var errs []error
	if err := waitGroupWithContext(ctx, &m.workerWg); err != nil {
		errs = append(errs, fmt.Errorf("prober workers: %w", err))
	}
	if err := waitGroupWithContext(ctx, &m.buildWg); err != nil {
		errs = append(errs, fmt.Errorf("build workers: %w", err))
	}
	if len(errs) > 0 {
		return fmt.Errorf("vod manager shutdown errors: %w", errors.Join(errs...))
	}
	return nil
}

// ActiveJobIDs returns a snapshot of currently running build job IDs.
func (m *Manager) ActiveJobIDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	ids := make([]string, 0, len(m.jobs))
	for id := range m.jobs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// MetadataPruneResult captures the outcome of a metadata cache prune.
type MetadataPruneResult struct {
	RemovedTTL        int
	RemovedMaxEntries int
	Remaining         int
}

// Prober interface to be injected into API if needed, or Manager exposes Probe?
// For Gate A compliance, Control must handle Probing via Infra.
// Prober interface to be injected into API
type Prober interface {
	Probe(ctx context.Context, path string) (*StreamInfo, error)
}

// SetProber allows injecting a mock prober for testing.
func (m *Manager) SetProber(p Prober) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prober = p
}

// CancelAll stops all active jobs.
// CancelAll stops all active jobs.
func (m *Manager) CancelAll() {
	m.mu.Lock()
	// Fix 24: Fix Deadlock in CancelAll
	// Copy map values to avoid holding lock while calling StopGracefully
	monitors := make([]*BuildMonitor, 0, len(m.jobs))
	for _, mon := range m.jobs {
		monitors = append(monitors, mon)
	}
	m.jobs = make(map[string]*BuildMonitor) // Clear while holding lock
	m.mu.Unlock()

	for _, mon := range monitors {
		if err := mon.StopGracefully(); err != nil {
			log.Error().Err(err).Str("job_id", mon.jobID).Msg("failed to stop monitor")
		}
	}
}

func waitGroupWithContext(ctx context.Context, wg *sync.WaitGroup) error {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

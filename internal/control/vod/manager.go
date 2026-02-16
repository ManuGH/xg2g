package vod

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	xlog "github.com/ManuGH/xg2g/internal/log"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/singleflight"
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
		return fmt.Errorf("vod manager shutdown errors: %v", errs)
	}
	return nil
}

// Probe delegates to the infra prober
func (m *Manager) Probe(ctx context.Context, path string) (*StreamInfo, error) {
	if m.prober == nil {
		return nil, errors.New("prober not configured")
	}
	return m.prober.Probe(ctx, path)
}

// GetMetadata returns cached metadata for an artifact.
func (m *Manager) GetMetadata(id string) (Metadata, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	meta, ok := m.metadata[id]
	return meta, ok
}

// MetadataPruneResult captures the outcome of a metadata cache prune.
type MetadataPruneResult struct {
	RemovedTTL        int
	RemovedMaxEntries int
	Remaining         int
}

// PruneMetadata evicts cached metadata using TTL and max entries.
// TTL eviction is applied first, then oldest-first to enforce maxEntries.
func (m *Manager) PruneMetadata(now time.Time, ttl time.Duration, maxEntries int) MetadataPruneResult {
	if maxEntries <= 0 {
		return MetadataPruneResult{}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.metadata) == 0 {
		return MetadataPruneResult{}
	}

	type entry struct {
		id        string
		updatedAt int64
		ttl       bool
	}

	entries := make([]entry, 0, len(m.metadata))
	cutoff := now.Add(-ttl).UnixNano()
	hasTTL := ttl > 0

	for id, meta := range m.metadata {
		expired := hasTTL && meta.UpdatedAt < cutoff
		entries = append(entries, entry{
			id:        id,
			updatedAt: meta.UpdatedAt,
			ttl:       expired,
		})
	}

	// Single sort: Oldest first
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].updatedAt == entries[j].updatedAt {
			return entries[i].id < entries[j].id
		}
		return entries[i].updatedAt < entries[j].updatedAt
	})

	removedTTL := 0
	removedMax := 0

	// Count valid (non-TTL) entries to determine overflow
	validCount := 0
	for _, e := range entries {
		if !e.ttl {
			validCount++
		}
	}
	overflow := 0
	if validCount > maxEntries {
		overflow = validCount - maxEntries
	}

	for _, e := range entries {
		if e.ttl {
			delete(m.metadata, e.id)
			removedTTL++
		} else if overflow > 0 {
			// Evict oldest non-TTL items to satisfy maxEntries
			delete(m.metadata, e.id)
			removedMax++
			overflow--
		}
		// Else keep (it's one of the newest maxEntries items)
	}

	return MetadataPruneResult{
		RemovedTTL:        removedTTL,
		RemovedMaxEntries: removedMax,
		Remaining:         len(m.metadata),
	}
}

// SeedMetadata directly sets the metadata cache for an artifact.
// WARNING: This is a destructive overwrite. Use only for testing or initial seeding.
// For production updates, use atomic methods like MarkProbed or MarkFailure.
func (m *Manager) SeedMetadata(id string, meta Metadata) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.metadata == nil {
		m.metadata = make(map[string]Metadata)
	}
	m.metadata[id] = meta
}

// enqueueProbe attempts to add a probe request to the queue without blocking.
// Returns true if enqueued, false if dropped.
func (m *Manager) enqueueProbe(req probeRequest) bool {
	select {
	case m.probeCh <- req:
		probeQueueLength.Set(float64(len(m.probeCh)))
		return true
	default:
		probeDropped.Inc()
		return false
	}
}

// MarkUnknown safely transitions state to UNKNOWN without wiping other fields.
func (m *Manager) MarkUnknown(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if meta, ok := m.metadata[id]; ok {
		meta.State = ArtifactStateUnknown
		m.touch(&meta)
		m.metadata[id] = meta
	} else {
		meta := Metadata{
			State: ArtifactStateUnknown,
		}
		m.touch(&meta)
		m.metadata[id] = meta
	}
}

// DemoteOnOpenFailure atomically demotes readiness state if I/O fails.
// This prevents infinite loops where handlers repeatedly see READY but fail to open file.
func (m *Manager) DemoteOnOpenFailure(id string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	meta, ok := m.metadata[id]
	if !ok || meta.State != ArtifactStateReady {
		return // Already demoted or unknown
	}

	// Demote to PREPARING to signal "momentary unavailability"
	// The next loop will see PREPARING and return 503 strictly.
	meta.State = ArtifactStatePreparing
	meta.Error = "reconcile: open failed in READY state: " + err.Error()
	m.touch(&meta)
	if meta.ResolvedPath == "" && m.pathMapper == nil {
		meta.State = ArtifactStateFailed
		meta.Error = "reconcile: missing input path for probe"
		m.touch(&meta)
		m.metadata[id] = meta
		return
	}
	m.metadata[id] = meta

	// Attempt reconciliation (best effort, non-blocking)
	m.enqueueProbe(probeRequest{ServiceRef: id, InputPath: meta.ResolvedPath})
}

// MarkPreparingIfState transitions a matching state to PREPARING (reconcile/rebuild).
// Returns updated metadata and true if the transition was applied.
func (m *Manager) MarkPreparingIfState(id string, expected ArtifactState, reason string) (Metadata, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	meta, ok := m.metadata[id]
	if !ok || meta.State != expected {
		return Metadata{}, false
	}

	meta.State = ArtifactStatePreparing
	if reason != "" {
		meta.Error = reason
	}
	m.touch(&meta)
	m.metadata[id] = meta

	return meta, true
}

// PromoteFailedToReadyIfPlaylist transitions FAILED -> READY when a playlist path is already known.
// Returns updated metadata and true if the transition was applied.
func (m *Manager) PromoteFailedToReadyIfPlaylist(id string) (Metadata, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	meta, ok := m.metadata[id]
	if !ok || meta.State != ArtifactStateFailed || !meta.HasPlaylist() {
		return Metadata{}, false
	}

	meta.State = ArtifactStateReady
	meta.Error = ""
	m.touch(&meta)
	m.metadata[id] = meta

	return meta, true
}

// SetResolvedPathIfEmpty stores a resolved input path if none is set yet.
func (m *Manager) SetResolvedPathIfEmpty(id string, resolved string) bool {
	if resolved == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	meta, ok := m.metadata[id]
	if !ok || meta.ResolvedPath != "" {
		return false
	}
	meta.ResolvedPath = resolved
	m.touch(&meta)
	m.metadata[id] = meta
	return true
}

// touch updates timestamp and generation
func (m *Manager) touch(meta *Metadata) {
	meta.UpdatedAt = time.Now().UnixNano() // Use Nano for higher resolution
	meta.StateGen++
}

// TriggerProbe initiates an async background probe for a recording.
func (m *Manager) TriggerProbe(id string, input string) {
	m.mu.Lock()
	meta, ok := m.metadata[id]
	if ok && meta.State == ArtifactStatePreparing {
		m.mu.Unlock()
		return // Already in progress
	}

	// Immediate state transition to PREPARING
	if !ok {
		meta = Metadata{State: ArtifactStateUnknown} // Initialize if missing
	}

	previousState := meta.State
	if !ok { // Handle new items correctly
		previousState = ArtifactStateUnknown
	}

	if input != "" {
		meta.ResolvedPath = input
	}
	if input == "" && meta.ResolvedPath == "" && m.pathMapper == nil {
		meta.State = ArtifactStateFailed
		meta.Error = "missing input path for probe"
		m.touch(&meta)
		m.metadata[id] = meta
		m.mu.Unlock()
		return
	}

	meta.State = ArtifactStatePreparing
	m.touch(&meta)
	m.metadata[id] = meta
	capturedGen := meta.StateGen
	resolvedPath := meta.ResolvedPath // Capture under lock
	m.mu.Unlock()

	// Enqueue background processing
	if !m.enqueueProbe(probeRequest{ServiceRef: id, InputPath: resolvedPath}) {
		// Queue full - revert state to avoid stuck PREPARING
		m.revertStateGuard(id, capturedGen, previousState)
	}
}

// revertStateGuard atomically reverts state if generation matches (race guard)
func (m *Manager) revertStateGuard(id string, guardedGen uint64, targetState ArtifactState) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Only revert if generation matches (strict guard)
	if current, exists := m.metadata[id]; exists &&
		current.State == ArtifactStatePreparing &&
		current.StateGen == guardedGen {

		current.State = targetState
		m.touch(&current)
		m.metadata[id] = current
	}
}

// RevertPreparingIfGen reverts PREPARING to a target state if generation matches.
func (m *Manager) RevertPreparingIfGen(id string, guardedGen uint64, targetState ArtifactState, reason string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if current, exists := m.metadata[id]; exists &&
		current.State == ArtifactStatePreparing &&
		current.StateGen == guardedGen {

		current.State = targetState
		if reason != "" {
			current.Error = reason
		}
		m.touch(&current)
		m.metadata[id] = current
		return true
	}
	return false
}

// markReadyFromBuild updates metadata to READY on successful build completion.
// Must be fast and must not do I/O.
func (m *Manager) markReadyFromBuild(jobID string, metaID string, spec Spec, finalPath string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	log.Debug().Str(xlog.FieldJobID, jobID).Str(xlog.FieldMetaID, metaID).Str(xlog.FieldFinalPath, finalPath).Msg("VOD manager: markReadyFromBuild called")

	meta := m.metadata[metaID] // zero value if absent; ok
	oldState := meta.State

	meta.State = ArtifactStateReady
	meta.Error = ""

	// Heuristic: if FinalPath points to an m3u8 file -> PlaylistPath.
	// Otherwise treat FinalPath as artifact path.
	if strings.HasSuffix(finalPath, ".m3u8") {
		meta.PlaylistPath = finalPath
	} else if strings.HasSuffix(finalPath, ".mp4") {
		meta.ArtifactPath = finalPath
	}

	if !meta.HasPlaylist() && !meta.HasArtifact() {
		meta.State = ArtifactStateFailed
		meta.Error = "build completed without artifact path"
	}
	m.touch(&meta)
	m.metadata[metaID] = meta

	log.Info().Str(xlog.FieldJobID, jobID).Str(xlog.FieldMetaID, metaID).Str(xlog.FieldOldState, string(oldState)).Str(xlog.FieldNewState, string(meta.State)).Str(xlog.FieldPlaylistPath, meta.PlaylistPath).Uint64("stateGen", meta.StateGen).Msg("VOD manager: metadata updated")

	// Job is finished; remove from jobs map to avoid leaks/stale BUILDING.
	delete(m.jobs, jobID)
	log.Debug().Str("jobId", jobID).Msg("VOD manager: job removed from jobs map")
}

// MarkFailed atomically transitions state to FAILED without wiping existing metadata.
func (m *Manager) MarkFailed(id string, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	meta, ok := m.metadata[id]
	if !ok {
		meta = Metadata{
			State: ArtifactStateUnknown,
		}
	}

	meta.State = ArtifactStateFailed
	meta.Error = reason
	m.touch(&meta)
	m.metadata[id] = meta
}

// markFailedFromBuild updates metadata to FAILED on build failure.
func (m *Manager) markFailedFromBuild(jobID string, metaID string, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	log.Debug().Str(xlog.FieldJobID, jobID).Str(xlog.FieldMetaID, metaID).Str("reason", reason).Msg("VOD manager: markFailedFromBuild called")

	meta := m.metadata[metaID]
	oldState := meta.State
	meta.State = ArtifactStateFailed
	meta.Error = reason
	m.touch(&meta)
	m.metadata[metaID] = meta

	log.Info().Str(xlog.FieldJobID, jobID).Str(xlog.FieldMetaID, metaID).Str(xlog.FieldOldState, string(oldState)).Str(xlog.FieldNewState, string(meta.State)).Str("error", meta.Error).Uint64("stateGen", meta.StateGen).Msg("VOD manager: metadata updated to FAILED")

	delete(m.jobs, jobID)
	log.Debug().Str("jobId", jobID).Msg("VOD manager: job removed from jobs map")
}

// MarkFailure updates metadata with a specific failure state and reason.
//
// Contract (Invariants):
//   - PRESERVES: Duration (failure paths never degrade Duration to 0/unknown)
//   - MUTATES: State, Error, ResolvedPath (if provided), Fingerprint (if provided), UpdatedAt
//   - Concurrency: Read-modify-write under mutex; last write wins
//
// Note: Duration may be updated on successful probe via MarkProbed (zero-guarded).
// See: TestTruth_Write_B5_ProbeFailPreservesDuration
func (m *Manager) MarkFailure(id string, state ArtifactState, reason string, resolvedPath string, fp *Fingerprint) {
	m.mu.Lock()
	defer m.mu.Unlock()

	meta, ok := m.metadata[id]
	if !ok {
		meta = Metadata{
			State: ArtifactStateUnknown,
		}
	}

	meta.State = state
	meta.Error = reason
	if resolvedPath != "" {
		meta.ResolvedPath = resolvedPath
	}
	if fp != nil {
		meta.Fingerprint = *fp
	}
	m.touch(&meta)
	m.metadata[id] = meta
}

// MarkProbed atomically updates metadata with probe results while preserving existing fields.
// This ensures that success paths (like failure paths) are non-destructive Read-Modify-Write operations.
func (m *Manager) MarkProbed(id string, resolvedPath string, info *StreamInfo, fp *Fingerprint) {
	m.mu.Lock()
	defer m.mu.Unlock()

	meta, ok := m.metadata[id]
	if !ok {
		meta = Metadata{
			State: ArtifactStateUnknown,
		}
	}

	// Update ResolvedPath and infer ArtifactPath if provided
	if resolvedPath != "" {
		meta.ResolvedPath = resolvedPath
		switch {
		case strings.HasSuffix(resolvedPath, ".mp4"):
			meta.ArtifactPath = resolvedPath
			meta.PlaylistPath = ""
		case strings.HasSuffix(resolvedPath, ".m3u8"):
			meta.PlaylistPath = resolvedPath
			meta.ArtifactPath = ""
		default:
			meta.ArtifactPath = ""
			meta.PlaylistPath = ""
		}
	}

	// Update only the fields that the probe provides
	if info != nil {
		if info.Container != "" {
			meta.Container = info.Container
		}
		if info.Video.CodecName != "" {
			meta.VideoCodec = info.Video.CodecName
		}
		if info.Audio.CodecName != "" {
			meta.AudioCodec = info.Audio.CodecName
		}
		if info.Video.Duration > 0 {
			// Validation: prevent zeroing out or wild regression if existing is valid
			if meta.Duration == 0 || math.Abs(float64(meta.Duration-int64(info.Video.Duration))) < float64(meta.Duration)*0.5 {
				meta.Duration = int64(math.Round(info.Video.Duration))
			}
		}
		if info.Video.Width > 0 {
			meta.Width = info.Video.Width
		}
		if info.Video.Height > 0 {
			meta.Height = info.Video.Height
		}
		if info.Video.FPS > 0 {
			meta.FPS = info.Video.FPS
		}
		if info.Video.Interlaced {
			meta.Interlaced = true
		}
	}

	if fp != nil {
		meta.Fingerprint = *fp
	}

	// Probe success implies the artifact (or source) is accessible/ready
	meta.State = ArtifactStateReady
	// Clear any previous error
	meta.Error = ""

	m.touch(&meta)
	m.metadata[id] = meta
}

// StartBuild initiates a VOD build.
// jobID identifies the build workspace (e.g., cacheDir), metaID identifies the recording (serviceRef).
// finalPath: the final destination for atomic publish.
func (m *Manager) StartBuild(ctx context.Context, jobID, metaID, input, workDir, outputTemp, finalPath string, profile Profile) (*BuildMonitor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if metaID == "" {
		return nil, errors.New("metaID required")
	}
	if h, exists := m.jobs[jobID]; exists {
		return h, nil
	}

	// StartBuild may receive non-local sources (e.g. callback/mock flows).
	// Filesystem-boundary validation remains enforced in probe/stat paths.
	spec := Spec{
		Input:      input,
		WorkDir:    workDir,
		OutputTemp: outputTemp,
		Profile:    profile,
	}

	mon := NewBuildMonitor(BuildMonitorConfig{
		JobID:     jobID,
		Spec:      spec,
		FinalPath: finalPath,
		Runner:    m.runner,
		Clock:     RealClock{},
		OnSucceeded: func(jobID string, spec Spec, finalPath string) {
			m.markReadyFromBuild(jobID, metaID, spec, finalPath)
		},
		OnFailed: func(jobID string, spec Spec, finalPath string, reason string) {
			m.markFailedFromBuild(jobID, metaID, reason)
		},
	})

	// Capture monitor to return
	m.jobs[jobID] = mon

	// Run monitor in background
	// Use manager context so we can cancel it on Shutdown
	runCtx := m.ctx
	m.buildWg.Add(1)
	go func() {
		defer m.buildWg.Done()
		mon.Run(runCtx)
	}()

	return mon, nil
}

// Get returns the status of a job if it exists.
func (m *Manager) Get(ctx context.Context, id string) (*JobStatus, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	mon, exists := m.jobs[id]
	if !exists {
		return nil, false
	}

	s := mon.GetStatus()
	status := &JobStatus{
		UpdatedAt: time.Now().Unix(),
	}

	switch s.State {
	case StateIdle:
		status.State = JobStateIdle
	case StateBuilding:
		status.State = JobStateBuilding
	case StateFinalizing:
		status.State = JobStateFinalizing
	case StateSucceeded:
		status.State = JobStateSucceeded
	case StateFailed:
		status.State = JobStateFailed
		status.Reason = string(s.Reason)
	case StateCanceled:
		status.State = JobStateFailed
		status.Reason = "CANCELED"
	}

	return status, true
}

// EnsureSpec validates context and prepares a Spec, serving as a gateway.
func (m *Manager) EnsureSpec(ctx context.Context, jobID, metaID, input, workDir, outputTemp, finalPath string, profile Profile) (Spec, error) {
	_, err := m.StartBuild(ctx, jobID, metaID, input, workDir, outputTemp, finalPath, profile)
	if err != nil {
		return Spec{}, err
	}
	// Return the Spec that was used/created
	return Spec{
		Input:      input,
		WorkDir:    workDir,
		OutputTemp: outputTemp,
		Profile:    profile,
	}, nil
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

package vod

import (
	"math"
	"sort"
	"strings"
	"time"
)

// GetMetadata returns cached metadata for an artifact.
func (m *Manager) GetMetadata(id string) (Metadata, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	meta, ok := m.metadata[id]
	return meta, ok
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
			// Trust the latest probe's positive duration. The previous guard rejected any
			// change larger than 50% of the stored value, which pinned a stale duration
			// after a legitimate re-cut/transcode. Zeroing is already prevented by the
			// outer `> 0` check.
			meta.Duration = int64(math.Round(info.Video.Duration))
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

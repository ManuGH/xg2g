package vod

import (
	"context"
	"errors"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile/ports"
	xlog "github.com/ManuGH/xg2g/internal/log"
	"github.com/rs/zerolog/log"
	"strings"
	"time"
)

var ErrMissingTarget = errors.New("missing target playback profile")

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

// StartBuild initiates a VOD build with a concrete playback target profile.
// jobID identifies the build workspace (e.g., cacheDir), metaID identifies the recording (serviceRef).
// finalPath: the final destination for atomic publish.
func (m *Manager) StartBuild(ctx context.Context, jobID, metaID, input, workDir, outputTemp, finalPath string, intent *ports.BuildIntent) (*BuildMonitor, error) {
	if intent == nil {
		return nil, ErrMissingTarget
	}

	internalProfile := ProfileDefault
	if intent.Target.Video.Mode == ports.MediaModeTranscode {
		internalProfile = ProfileHigh
	}

	return m.startBuildWithSpec(ctx, jobID, metaID, finalPath, Spec{
		Input:      input,
		WorkDir:    workDir,
		OutputTemp: outputTemp,
		Profile:    internalProfile,
		Intent:     intent,
	})
}

func (m *Manager) startBuildWithSpec(ctx context.Context, jobID, metaID, finalPath string, spec Spec) (*BuildMonitor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if metaID == "" {
		return nil, errors.New("metaID required")
	}
	if h, exists := m.jobs[jobID]; exists {
		return h, nil
	}

	mon := NewBuildMonitor(BuildMonitorConfig{
		JobID:     jobID,
		Spec:      spec,
		FinalPath: finalPath,
		Runner:    m.runner,
		Clock:     RealClock{},
		Prober:    m.prober,
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

// EnsureSpec validates context and prepares a Spec with a concrete target playback profile, serving as a gateway.
func (m *Manager) EnsureSpec(ctx context.Context, jobID, metaID, input, workDir, outputTemp, finalPath string, intent *ports.BuildIntent) (Spec, error) {
	if intent == nil {
		return Spec{}, ErrMissingTarget
	}

	internalProfile := ProfileDefault
	if intent.Target.Video.Mode == ports.MediaModeTranscode {
		internalProfile = ProfileHigh
	}

	spec := Spec{
		Input:      input,
		WorkDir:    workDir,
		OutputTemp: outputTemp,
		Profile:    internalProfile,
		Intent:     intent,
	}
	_, err := m.startBuildWithSpec(ctx, jobID, metaID, finalPath, spec)
	if err != nil {
		return Spec{}, err
	}
	return spec, nil
}

// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package staging

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ManuGH/xg2g/internal/domain/recording"
)

var (
	ErrWorkspaceConflict = errors.New("workspace directory exists with conflicting job manifest")
	ErrInvalidFilename  = errors.New("invalid output filename for staging")
)

// StagingManager manages per-job local NVMe workspace subdirectories with fine-grained per-job locks.
type StagingManager struct {
	globalMu    sync.Mutex
	jobLocks    map[string]*sync.Mutex
	stagingRoot string
	repo        recording.JobRepository
	finalizer   Finalizer
}

// NewStagingManager initializes a StagingManager bound to stagingRoot.
func NewStagingManager(stagingRoot string, repo recording.JobRepository) (*StagingManager, error) {
	if stagingRoot == "" {
		return nil, fmt.Errorf("stagingRoot cannot be empty")
	}
	if repo == nil {
		var err error
		repo, err = recording.NewDiskJobRepository(stagingRoot)
		if err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(stagingRoot, 0755); err != nil {
		return nil, fmt.Errorf("failed to create stagingRoot %s: %w", stagingRoot, err)
	}
	return &StagingManager{
		jobLocks:    make(map[string]*sync.Mutex),
		stagingRoot: stagingRoot,
		repo:        repo,
		finalizer:   NewTSFinalizer(),
	}, nil
}

func (sm *StagingManager) getJobLock(jobID string) *sync.Mutex {
	sm.globalMu.Lock()
	defer sm.globalMu.Unlock()

	l, ok := sm.jobLocks[jobID]
	if !ok {
		l = &sync.Mutex{}
		sm.jobLocks[jobID] = l
	}
	return l
}

// JobDir returns the dedicated local staging workspace directory for jobID.
func (sm *StagingManager) JobDir(jobID string) string {
	return filepath.Join(sm.stagingRoot, "jobs", jobID)
}

// SegmentsDir returns the input segments directory for jobID.
func (sm *StagingManager) SegmentsDir(jobID string) string {
	return filepath.Join(sm.JobDir(jobID), "segments")
}

// FinalizedDir returns the output finalized directory for jobID.
func (sm *StagingManager) FinalizedDir(jobID string) string {
	return filepath.Join(sm.JobDir(jobID), "finalized")
}

// PrepareWorkspace creates a clean, structured workspace (segments/, work/, finalized/) for a job.
func (sm *StagingManager) PrepareWorkspace(ctx context.Context, job *recording.RecordingJob) (string, error) {
	if err := recording.ValidateJobID(job.ID); err != nil {
		return "", err
	}

	jobLock := sm.getJobLock(job.ID)
	jobLock.Lock()
	defer jobLock.Unlock()

	jobDir := sm.JobDir(job.ID)
	manifestFile := sm.repo.(*recording.DiskJobRepository).ManifestPath(job.ID)

	// Safe workspace recovery check: if workspace exists, verify manifest
	if _, err := os.Stat(manifestFile); err == nil {
		existingJob, err := sm.repo.Get(ctx, job.ID)
		if err == nil && existingJob.ID == job.ID {
			// Workspace belongs to same job: reuse existing directory safely!
			job.LocalStagingPath = jobDir
			return jobDir, nil
		}
		return "", fmt.Errorf("%w: jobID '%s'", ErrWorkspaceConflict, job.ID)
	}

	// Create subdirectories safely without blind os.RemoveAll
	segsDir := sm.SegmentsDir(job.ID)
	workDir := filepath.Join(jobDir, "work")
	finDir := sm.FinalizedDir(job.ID)

	if err := os.MkdirAll(segsDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create segments dir: %w", err)
	}
	_ = os.MkdirAll(workDir, 0755)
	_ = os.MkdirAll(finDir, 0755)

	job.LocalStagingPath = jobDir
	if err := sm.repo.Save(ctx, job); err != nil {
		return "", fmt.Errorf("failed to persist job manifest during workspace prep: %w", err)
	}

	return jobDir, nil
}

// AssembleAndFinalize concatenates segments in SegmentsDir into FinalizedDir and updates job manifest atomicaly.
func (sm *StagingManager) AssembleAndFinalize(ctx context.Context, jobID string, outputFilename string) (*AssemblyReport, error) {
	if err := recording.ValidateJobID(jobID); err != nil {
		return nil, err
	}

	// Defense-in-depth: sanitize outputFilename against path traversal
	cleaned := filepath.Base(outputFilename)
	if cleaned != outputFilename || strings.Contains(outputFilename, "/") || strings.Contains(outputFilename, "..") {
		return nil, fmt.Errorf("%w: '%s'", ErrInvalidFilename, outputFilename)
	}

	jobLock := sm.getJobLock(jobID)
	jobLock.Lock()
	defer jobLock.Unlock()

	job, err := sm.repo.Get(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch job for finalization: %w", err)
	}

	if err := job.CanTransitionTo(recording.StateFinalizing); err != nil {
		return nil, err
	}

	job.State = recording.StateFinalizing
	if err := sm.repo.Save(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to update job state to FINALIZING: %w", err)
	}

	segsDir := sm.SegmentsDir(jobID)
	targetFilePath := filepath.Join(sm.FinalizedDir(jobID), outputFilename)

	report, err := sm.finalizer.Finalize(ctx, jobID, segsDir, targetFilePath)
	if err != nil {
		job.State = recording.StateFailed
		job.ErrorDetail = fmt.Sprintf("assembly failed: %v", err)
		_ = sm.repo.Save(ctx, job)
		return nil, err
	}

	job.FinalizedPath = report.FinalizedPath
	if report.Complete {
		job.State = recording.StateCompleted
	} else {
		job.State = recording.StatePartial
	}

	if err := sm.repo.Save(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to save finalized job manifest: %w", err)
	}

	return report, nil
}

// CleanupWorkspace removes job workspace directory and releases per-job lock.
func (sm *StagingManager) CleanupWorkspace(ctx context.Context, jobID string) error {
	if err := recording.ValidateJobID(jobID); err != nil {
		return err
	}

	jobLock := sm.getJobLock(jobID)
	jobLock.Lock()
	defer jobLock.Unlock()

	sm.globalMu.Lock()
	delete(sm.jobLocks, jobID)
	sm.globalMu.Unlock()

	return sm.repo.Delete(ctx, jobID)
}

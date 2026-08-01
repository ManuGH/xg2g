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
	ErrWorkspaceConflict = errors.New("workspace directory conflict for job")
	ErrInvalidFilename   = errors.New("invalid or unsafe output filename")
)

// StagingManager manages workspace directories and finalization assembly under domain repository isolation.
type StagingManager struct {
	stagingRoot string
	repo        recording.JobRepository
	finalizer   Finalizer
	globalMu    sync.Mutex
	jobLocks    map[string]*sync.Mutex
}

// NewStagingManager initializes a StagingManager.
func NewStagingManager(stagingRoot string, repo recording.JobRepository) (*StagingManager, error) {
	if stagingRoot == "" {
		return nil, fmt.Errorf("stagingRoot cannot be empty")
	}
	if repo == nil {
		return nil, fmt.Errorf("job repository cannot be nil")
	}
	if err := os.MkdirAll(stagingRoot, 0755); err != nil {
		return nil, fmt.Errorf("failed to create stagingRoot: %w", err)
	}
	return &StagingManager{
		stagingRoot: stagingRoot,
		repo:        repo,
		finalizer:   NewTSFinalizer(),
		jobLocks:    make(map[string]*sync.Mutex),
	}, nil
}

func (sm *StagingManager) getJobLock(jobID string) *sync.Mutex {
	sm.globalMu.Lock()
	defer sm.globalMu.Unlock()
	lock, ok := sm.jobLocks[jobID]
	if !ok {
		lock = &sync.Mutex{}
		sm.jobLocks[jobID] = lock
	}
	return lock
}

// JobDir returns the absolute path to a job's workspace directory.
func (sm *StagingManager) JobDir(jobID string) string {
	return filepath.Join(sm.stagingRoot, "jobs", jobID)
}

// SegmentsDir returns the absolute path to a job's segments directory.
func (sm *StagingManager) SegmentsDir(jobID string) string {
	return filepath.Join(sm.stagingRoot, "jobs", jobID, "segments")
}

// FinalizedDir returns the absolute path to a job's finalized output directory.
func (sm *StagingManager) FinalizedDir(jobID string) string {
	return filepath.Join(sm.stagingRoot, "jobs", jobID, "finalized")
}

// PrepareWorkspace creates a clean, structured workspace (segments/, work/, finalized/) for a job without type assertions.
func (sm *StagingManager) PrepareWorkspace(ctx context.Context, job *recording.RecordingJob) (string, error) {
	if job == nil {
		return "", fmt.Errorf("cannot prepare workspace for nil job")
	}
	if err := recording.ValidateJobID(job.ID); err != nil {
		return "", err
	}

	jobLock := sm.getJobLock(job.ID)
	jobLock.Lock()
	defer jobLock.Unlock()

	jobDir := sm.JobDir(job.ID)

	// Safe workspace recovery check via domain repository interface (no concrete type assertions)
	existingJob, err := sm.repo.Get(ctx, job.ID)
	if err == nil && existingJob != nil && existingJob.ID == job.ID {
		// Workspace belongs to same job: reuse existing directory safely!
		job.LocalStagingPath = jobDir
		return jobDir, nil
	}

	// Create subdirectories safely with strict error checking
	segsDir := sm.SegmentsDir(job.ID)
	workDir := filepath.Join(jobDir, "work")
	finDir := sm.FinalizedDir(job.ID)

	if err := os.MkdirAll(segsDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create segments dir: %w", err)
	}
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create work dir: %w", err)
	}
	if err := os.MkdirAll(finDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create finalized dir: %w", err)
	}

	job.LocalStagingPath = jobDir
	if err := sm.repo.Save(ctx, job, job.Version); err != nil {
		return "", fmt.Errorf("failed to persist job manifest during workspace prep: %w", err)
	}

	return jobDir, nil
}

// AssembleAndFinalize concatenates segments in SegmentsDir into FinalizedDir and updates job manifest atomically.
func (sm *StagingManager) AssembleAndFinalize(ctx context.Context, jobID string, outputFilename string) (*AssemblyReport, error) {
	if err := recording.ValidateJobID(jobID); err != nil {
		return nil, err
	}

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

	finalizingJob, err := job.TransitionState(recording.StateFinalizing, "")
	if err != nil {
		return nil, err
	}
	if err := sm.repo.Save(ctx, finalizingJob, job.Version); err != nil {
		return nil, fmt.Errorf("failed to update job state to FINALIZING: %w", err)
	}

	segsDir := sm.SegmentsDir(jobID)
	targetFilePath := filepath.Join(sm.FinalizedDir(jobID), outputFilename)

	report, err := sm.finalizer.Finalize(ctx, jobID, segsDir, targetFilePath)
	if err != nil {
		failedJob, transErr := finalizingJob.TransitionState(recording.StateFailed, fmt.Sprintf("assembly failed: %v", err))
		if transErr == nil {
			saveErr := sm.repo.Save(ctx, failedJob, finalizingJob.Version)
			if saveErr != nil {
				return nil, errors.Join(fmt.Errorf("assembly failed: %w", err), fmt.Errorf("failed to save FAILED job state: %w", saveErr))
			}
		}
		return nil, err
	}

	// Update job finalized path under CAS lock. If assembly was incomplete due to missing segment gaps, transition to StatePartial.
	finalizingJob.FinalizedPath = report.FinalizedPath
	if !report.Complete {
		if partialJob, transErr := finalizingJob.TransitionState(recording.StatePartial, "assembly completed with missing segment gaps"); transErr == nil {
			finalizingJob = partialJob
		}
	}
	if err := sm.repo.Save(ctx, finalizingJob, finalizingJob.Version); err != nil {
		return nil, fmt.Errorf("failed to save finalized job path: %w", err)
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

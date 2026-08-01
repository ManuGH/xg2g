// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package recording

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/recording"
	"github.com/ManuGH/xg2g/internal/infra/storage"
)

// TransferWorker processes persistent TransferTasks to commit staged files to target StorageBackends.
type TransferWorker struct {
	workerID    string
	stagingRoot string
	taskRepo    recording.TransferTaskRepository
	jobRepo     recording.JobRepository
	assetRepo   recording.AssetRepository
	backends    map[string]storage.StorageBackend
}

// NewTransferWorker initializes TransferWorker.
func NewTransferWorker(
	workerID, stagingRoot string,
	taskRepo recording.TransferTaskRepository,
	jobRepo recording.JobRepository,
	assetRepo recording.AssetRepository,
	backends []storage.StorageBackend,
) (*TransferWorker, error) {
	if workerID == "" {
		return nil, fmt.Errorf("workerID cannot be empty")
	}
	if stagingRoot == "" {
		return nil, fmt.Errorf("stagingRoot cannot be empty")
	}
	backendMap := make(map[string]storage.StorageBackend)
	for _, b := range backends {
		if b != nil {
			backendMap[b.ID()] = b
		}
	}
	return &TransferWorker{
		workerID:    workerID,
		stagingRoot: stagingRoot,
		taskRepo:    taskRepo,
		jobRepo:     jobRepo,
		assetRepo:   assetRepo,
		backends:    backendMap,
	}, nil
}

// ProcessNextTask claims and processes one TransferTask. Returns true if a task was processed.
func (w *TransferWorker) ProcessNextTask(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	leaseDuration := 10 * time.Minute
	task, err := w.taskRepo.ClaimTask(ctx, w.workerID, leaseDuration)
	if err != nil {
		if err == recording.ErrNoTransferTaskToClaim {
			return false, nil
		}
		return false, err
	}

	if err := w.executeTransfer(ctx, task); err != nil {
		// Record attempt failure
		task.LastError = err.Error()
		now := time.Now()
		if task.AttemptCount >= task.MaxAttempts {
			task.State = recording.TransferFailed
		} else {
			task.State = recording.TransferRetrying
			task.NextAttemptAt = now.Add(time.Duration(task.AttemptCount*10) * time.Second)
		}
		_ = w.taskRepo.Save(ctx, task)
		return true, fmt.Errorf("transfer task %s failed (attempt %d): %w", task.ID, task.AttemptCount, err)
	}

	return true, nil
}

func (w *TransferWorker) executeTransfer(ctx context.Context, task *recording.TransferTask) error {
	backend, ok := w.backends[task.TargetBackendID]
	if !ok || backend == nil {
		return fmt.Errorf("target storage backend '%s' offline or unavailable", task.TargetBackendID)
	}

	// 1. Resolve source file path strictly relative to staging root (no raw absolute paths allowed)
	srcRel := filepath.Clean(filepath.Join("jobs", task.SourceWorkspaceID, "finalized", task.SourceObjectKey))
	srcAbsPath, err := recording.SanitizeAndValidateRelativePath(w.stagingRoot, srcRel)
	if err != nil {
		return fmt.Errorf("failed to sanitize source staging path: %w", err)
	}
	fullSrcPath := filepath.Join(w.stagingRoot, srcAbsPath)

	info, err := os.Stat(fullSrcPath)
	if err != nil {
		return fmt.Errorf("source staged file not found: %w", err)
	}
	if info.Size() != task.ExpectedSize {
		return fmt.Errorf("staged source size mismatch: got %d, expected %d", info.Size(), task.ExpectedSize)
	}

	// 2. Commit file to StorageBackend
	if err := backend.CommitFile(ctx, fullSrcPath, task.TargetObjectKey); err != nil {
		return fmt.Errorf("failed to commit file to backend: %w", err)
	}

	// 3. Stat Verification on Target Backend
	targetInfo, err := backend.Stat(ctx, task.TargetObjectKey)
	if err != nil {
		return fmt.Errorf("failed to verify committed file via backend.Stat: %w", err)
	}
	if targetInfo.SizeBytes != task.ExpectedSize {
		return fmt.Errorf("target committed size mismatch: got %d, expected %d", targetInfo.SizeBytes, task.ExpectedSize)
	}

	// 4. Transition Asset to AVAILABLE
	asset, err := w.assetRepo.Get(ctx, task.AssetID)
	if err != nil {
		return fmt.Errorf("failed to fetch asset %s: %w", task.AssetID, err)
	}
	availableAsset, err := asset.TransitionState(recording.AssetAvailable)
	if err != nil {
		return fmt.Errorf("failed to transition asset to AVAILABLE: %w", err)
	}
	availableAsset.SizeBytes = targetInfo.SizeBytes
	finTime := time.Now()
	availableAsset.FinalizedAt = &finTime

	if err := w.assetRepo.Save(ctx, availableAsset, asset.Version); err != nil {
		return fmt.Errorf("failed to save AVAILABLE asset: %w", err)
	}

	// 5. Transition Job to COMPLETED
	job, err := w.jobRepo.Get(ctx, task.JobID)
	if err == nil && job != nil {
		job.State = recording.StateCompleted
		_ = w.jobRepo.Save(ctx, job)
	}

	// 6. Mark TransferTask COMPLETED
	task.State = recording.TransferCompleted
	if err := w.taskRepo.Save(ctx, task); err != nil {
		return fmt.Errorf("failed to save COMPLETED transfer task: %w", err)
	}

	// 7. Safe Cleanup of Staged Workspace temporary subdirectories ONLY AFTER verified completion
	jobDir := filepath.Join(w.stagingRoot, "jobs", task.SourceWorkspaceID)
	_ = os.RemoveAll(filepath.Join(jobDir, "segments"))
	_ = os.RemoveAll(filepath.Join(jobDir, "finalized"))
	_ = os.RemoveAll(filepath.Join(jobDir, "work"))

	return nil
}

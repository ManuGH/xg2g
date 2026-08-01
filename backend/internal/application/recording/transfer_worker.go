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

// ProcessNextTask claims and processes one TransferTask idempotently. Returns true if a task was processed.
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
		// Record attempt failure under lease ownership
		task.LastError = err.Error()
		now := time.Now()
		if task.AttemptCount >= task.MaxAttempts {
			task.State = recording.TransferFailed
		} else {
			task.State = recording.TransferRetrying
			task.NextAttemptAt = now.Add(time.Duration(task.AttemptCount*10) * time.Second)
		}
		saveErr := w.taskRepo.SaveTaskLeased(ctx, task, w.workerID, task.LeaseExpiresAt)
		if saveErr != nil {
			return true, fmt.Errorf("transfer task %s failed (attempt %d): %v; AND lease save failed: %w", task.ID, task.AttemptCount, err, saveErr)
		}
		return true, fmt.Errorf("transfer task %s failed (attempt %d): %w", task.ID, task.AttemptCount, err)
	}

	return true, nil
}

func (w *TransferWorker) executeTransfer(ctx context.Context, task *recording.TransferTask) error {
	backend, ok := w.backends[task.TargetBackendID]
	if !ok || backend == nil {
		return fmt.Errorf("target storage backend '%s' offline or unavailable", task.TargetBackendID)
	}

	// 1. Idempotency Pre-Check: Check if target file already exists on backend
	alreadyCommitted := false
	targetInfo, statErr := backend.Stat(ctx, task.TargetObjectKey)
	if statErr == nil && targetInfo.SizeBytes == task.ExpectedSize {
		alreadyCommitted = true
	}

	// 2. Perform CommitFile only if not already committed
	if !alreadyCommitted {
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

		if err := backend.CommitFile(ctx, fullSrcPath, task.TargetObjectKey); err != nil {
			return fmt.Errorf("failed to commit file to backend: %w", err)
		}

		// Stat Verification on Target Backend
		targetInfo, statErr = backend.Stat(ctx, task.TargetObjectKey)
		if statErr != nil {
			return fmt.Errorf("failed to verify committed file via backend.Stat: %w", statErr)
		}
		if targetInfo.SizeBytes != task.ExpectedSize {
			return fmt.Errorf("target committed size mismatch: got %d, expected %d", targetInfo.SizeBytes, task.ExpectedSize)
		}
	}

	// 3. Transition Asset to AVAILABLE idempotently
	asset, err := w.assetRepo.Get(ctx, task.AssetID)
	if err != nil {
		return fmt.Errorf("failed to fetch asset %s: %w", task.AssetID, err)
	}

	if asset.State != recording.AssetAvailable {
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
	}

	// 4. Transition Job to COMPLETED idempotently
	job, err := w.jobRepo.Get(ctx, task.JobID)
	if err == nil && job != nil && job.State != recording.StateCompleted {
		job.State = recording.StateCompleted
		if err := w.jobRepo.Save(ctx, job); err != nil {
			return fmt.Errorf("failed to save COMPLETED job: %w", err)
		}
	}

	// 5. Mark TransferTask COMPLETED under CAS lease ownership
	task.State = recording.TransferCompleted
	if err := w.taskRepo.SaveTaskLeased(ctx, task, w.workerID, task.LeaseExpiresAt); err != nil {
		return fmt.Errorf("failed to save COMPLETED transfer task under lease: %w", err)
	}

	// 6. Safe Cleanup of Staged Workspace temporary subdirectories ONLY AFTER verified completion & CAS save
	jobDir := filepath.Join(w.stagingRoot, "jobs", task.SourceWorkspaceID)
	_ = os.RemoveAll(filepath.Join(jobDir, "segments"))
	_ = os.RemoveAll(filepath.Join(jobDir, "finalized"))
	_ = os.RemoveAll(filepath.Join(jobDir, "work"))

	return nil
}

// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package staging

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ManuGH/xg2g/internal/domain/recording"
	"github.com/ManuGH/xg2g/internal/infra/storage"
)

// StartupReconciler scans jobs, assets, and transfer tasks on startup to recover orphaned states per the 5-case matrix.
type StartupReconciler struct {
	jobRepo     recording.JobRepository
	assetRepo   recording.AssetRepository
	taskRepo    recording.TransferTaskRepository
	stagingRoot string
	backends    map[string]storage.StorageBackend
}

// NewStartupReconciler initializes StartupReconciler.
func NewStartupReconciler(
	jobRepo recording.JobRepository,
	assetRepo recording.AssetRepository,
	taskRepo recording.TransferTaskRepository,
	stagingRoot string,
	backends []storage.StorageBackend,
) (*StartupReconciler, error) {
	if jobRepo == nil || assetRepo == nil || taskRepo == nil {
		return nil, fmt.Errorf("repositories cannot be nil")
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
	return &StartupReconciler{
		jobRepo:     jobRepo,
		assetRepo:   assetRepo,
		taskRepo:    taskRepo,
		stagingRoot: stagingRoot,
		backends:    backendMap,
	}, nil
}

// ReconcileAll scans and resolves all incomplete job and asset states across the system.
func (r *StartupReconciler) ReconcileAll(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	jobs, err := r.jobRepo.ListRecoverable(ctx)
	if err != nil {
		return fmt.Errorf("failed to list recoverable jobs during reconciliation: %w", err)
	}

	allAssets, err := r.assetRepo.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list assets during reconciliation: %w", err)
	}

	assetsByJobID := make(map[string]*recording.RecordingAsset)
	for _, a := range allAssets {
		assetsByJobID[a.JobID] = a
	}

	tasksList, err := r.taskRepo.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list transfer tasks during reconciliation: %w", err)
	}

	taskMap := make(map[string]*recording.TransferTask)
	for _, t := range tasksList {
		taskMap[t.JobID] = t
	}

	for _, job := range jobs {
		if job.State == recording.StateCompleted || job.State == recording.StateFailed {
			continue
		}

		asset, hasAsset := assetsByJobID[job.ID]
		if !hasAsset || asset == nil {
			continue
		}

		backend, backendAvailable := r.backends[job.TargetBackendID]
		targetExists := false
		var targetSize int64

		if backendAvailable && backend != nil {
			info, statErr := backend.Stat(ctx, asset.ObjectKey)
			if statErr == nil && info.SizeBytes > 0 {
				targetExists = true
				targetSize = info.SizeBytes
			}
		}

		stagedRelPath := filepath.Join("jobs", job.ID, "finalized", filepath.Base(asset.ObjectKey))
		stagedAbsPath := filepath.Join(r.stagingRoot, stagedRelPath)
		stagedInfo, stagedErr := os.Stat(stagedAbsPath)
		stagedExists := (stagedErr == nil && stagedInfo.Size() > 0)

		task := taskMap[job.ID]

		// Case 1: Target file exists with expected size + Asset TRANSFER_PENDING
		if targetExists && asset.State == recording.AssetTransferPending {
			availAsset, _ := asset.TransitionState(recording.AssetAvailable)
			availAsset.SizeBytes = targetSize
			_ = r.assetRepo.Save(ctx, availAsset, asset.Version)

			job.State = recording.StateCompleted
			_ = r.jobRepo.Save(ctx, job)

			if task != nil && task.State != recording.TransferCompleted {
				task.State = recording.TransferCompleted
				_ = r.taskRepo.SaveTaskLeased(ctx, task, task.LockedBy, task.LeaseToken)
			}
			continue
		}

		// Case 2: Asset AVAILABLE + Job TRANSFERRING / WAITING_FOR_TARGET
		if asset.State == recording.AssetAvailable && job.State != recording.StateCompleted {
			job.State = recording.StateCompleted
			_ = r.jobRepo.Save(ctx, job)
			continue
		}

		// Case 3: Asset TRANSFER_PENDING + Job WAITING_FOR_TARGET + No TransferTask + Staged file exists
		if asset.State == recording.AssetTransferPending && task == nil && stagedExists {
			newTask, taskErr := recording.NewTransferTask(
				"task_rec_"+job.ID,
				job.ID,
				asset.ID,
				job.ID,
				filepath.Base(asset.ObjectKey),
				job.TargetBackendID,
				asset.ObjectKey,
				stagedInfo.Size(),
			)
			if taskErr == nil {
				_ = r.taskRepo.CreateTask(ctx, newTask)
			}
			continue
		}

		// Case 4: Task COMPLETED + Asset TRANSFER_PENDING
		if task != nil && task.State == recording.TransferCompleted && asset.State != recording.AssetAvailable {
			availAsset, _ := asset.TransitionState(recording.AssetAvailable)
			availAsset.SizeBytes = task.ExpectedSize
			_ = r.assetRepo.Save(ctx, availAsset, asset.Version)

			job.State = recording.StateCompleted
			_ = r.jobRepo.Save(ctx, job)
			continue
		}

		// Case 5: Staging output missing AND target missing + Asset TRANSFER_PENDING
		if !stagedExists && !targetExists && asset.State == recording.AssetTransferPending {
			corruptAsset, _ := asset.TransitionState(recording.AssetMissing)
			_ = r.assetRepo.Save(ctx, corruptAsset, asset.Version)

			job.State = recording.StateFailed
			_ = r.jobRepo.Save(ctx, job)

			if task != nil {
				task.State = recording.TransferFailed
				task.LastError = "staged source file and target backend object both missing during recovery"
				_ = r.taskRepo.SaveTaskLeased(ctx, task, task.LockedBy, task.LeaseToken)
			}
		}
	}

	return nil
}

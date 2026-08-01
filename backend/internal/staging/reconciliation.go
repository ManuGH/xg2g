// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package staging

import (
	"context"
	"errors"
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

// ReconcileAll scans and resolves all incomplete job and asset states across the system without error swallowing.
func (r *StartupReconciler) ReconcileAll(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	var errs []error

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

	// Deterministic Task Resolution: group tasks by JobID, keeping the latest task and marking duplicate tasks FAILED
	taskMap := make(map[string]*recording.TransferTask)
	for _, t := range tasksList {
		if existing, ok := taskMap[t.JobID]; ok {
			// Duplicate task detected: keep newer, mark older FAILED
			older := existing
			newer := t
			if t.CreatedAt.Before(existing.CreatedAt) {
				older = t
				newer = existing
			}
			taskMap[t.JobID] = newer
			if older.State != recording.TransferFailed {
				if recErr := r.taskRepo.RecoverTaskState(ctx, older.ID, recording.TransferFailed, "duplicate task resolved during startup reconciliation"); recErr != nil {
					errs = append(errs, fmt.Errorf("failed to mark duplicate task %s FAILED: %w", older.ID, recErr))
				}
			}
		} else {
			taskMap[t.JobID] = t
		}
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

		// Determine expected size for strict target validation
		expectedSize := asset.SizeBytes
		task := taskMap[job.ID]
		if task != nil && task.ExpectedSize > 0 {
			expectedSize = task.ExpectedSize
		}

		targetValid := (targetExists && (expectedSize == 0 || targetSize == expectedSize))

		// Resolve staged source file using asset's persistent SourceFilename
		srcFilename := asset.SourceFilename
		if srcFilename == "" && task != nil && task.SourceFilename != "" {
			srcFilename = task.SourceFilename
		}
		if srcFilename == "" {
			srcFilename = filepath.Base(asset.ObjectKey)
		}

		stagedRelPath := filepath.Join("jobs", job.ID, "finalized", srcFilename)
		stagedAbsPath := filepath.Join(r.stagingRoot, stagedRelPath)
		stagedInfo, stagedErr := os.Stat(stagedAbsPath)
		stagedExists := (stagedErr == nil && stagedInfo.Size() > 0)

		// Case 1: Target file exists with exact expected size + Asset TRANSFER_PENDING
		if targetValid && asset.State == recording.AssetTransferPending {
			availAsset, transErr := asset.TransitionState(recording.AssetAvailable)
			if transErr != nil {
				errs = append(errs, fmt.Errorf("case 1 asset transition failed: %w", transErr))
				continue
			}
			availAsset.SizeBytes = targetSize
			if saveErr := r.assetRepo.Save(ctx, availAsset, asset.Version); saveErr != nil {
				errs = append(errs, fmt.Errorf("case 1 asset save failed: %w", saveErr))
				continue
			}

			job.State = recording.StateCompleted
			if saveErr := r.jobRepo.Save(ctx, job); saveErr != nil {
				errs = append(errs, fmt.Errorf("case 1 job save failed: %w", saveErr))
				continue
			}

			if task != nil && task.State != recording.TransferCompleted {
				if recErr := r.taskRepo.RecoverTaskState(ctx, task.ID, recording.TransferCompleted, ""); recErr != nil {
					errs = append(errs, fmt.Errorf("case 1 task recover failed: %w", recErr))
				}
			}
			continue
		}

		// Case 2: Asset AVAILABLE + Job TRANSFERRING / WAITING_FOR_TARGET
		if asset.State == recording.AssetAvailable && job.State != recording.StateCompleted {
			job.State = recording.StateCompleted
			if saveErr := r.jobRepo.Save(ctx, job); saveErr != nil {
				errs = append(errs, fmt.Errorf("case 2 job save failed: %w", saveErr))
			}
			continue
		}

		// Case 3: Asset TRANSFER_PENDING + Job WAITING_FOR_TARGET + No TransferTask + Staged file exists
		if asset.State == recording.AssetTransferPending && task == nil && stagedExists {
			newTask, taskErr := recording.NewTransferTask(
				"task_rec_"+job.ID,
				job.ID,
				asset.ID,
				job.ID,
				srcFilename,
				job.TargetBackendID,
				asset.ObjectKey,
				stagedInfo.Size(),
			)
			if taskErr != nil {
				errs = append(errs, fmt.Errorf("case 3 new task creation failed: %w", taskErr))
				continue
			}
			if createErr := r.taskRepo.CreateTask(ctx, newTask); createErr != nil {
				errs = append(errs, fmt.Errorf("case 3 task repo CreateTask failed: %w", createErr))
			}
			continue
		}

		// Case 4: Task COMPLETED + Asset TRANSFER_PENDING
		if task != nil && task.State == recording.TransferCompleted && asset.State != recording.AssetAvailable {
			availAsset, transErr := asset.TransitionState(recording.AssetAvailable)
			if transErr != nil {
				errs = append(errs, fmt.Errorf("case 4 asset transition failed: %w", transErr))
				continue
			}
			availAsset.SizeBytes = task.ExpectedSize
			if saveErr := r.assetRepo.Save(ctx, availAsset, asset.Version); saveErr != nil {
				errs = append(errs, fmt.Errorf("case 4 asset save failed: %w", saveErr))
				continue
			}

			job.State = recording.StateCompleted
			if saveErr := r.jobRepo.Save(ctx, job); saveErr != nil {
				errs = append(errs, fmt.Errorf("case 4 job save failed: %w", saveErr))
			}
			continue
		}

		// Case 5: Staging output missing AND target missing/invalid + Asset TRANSFER_PENDING
		if !stagedExists && !targetValid && asset.State == recording.AssetTransferPending {
			corruptAsset, transErr := asset.TransitionState(recording.AssetMissing)
			if transErr != nil {
				errs = append(errs, fmt.Errorf("case 5 asset transition failed: %w", transErr))
				continue
			}
			if saveErr := r.assetRepo.Save(ctx, corruptAsset, asset.Version); saveErr != nil {
				errs = append(errs, fmt.Errorf("case 5 asset save failed: %w", saveErr))
				continue
			}

			job.State = recording.StateFailed
			if saveErr := r.jobRepo.Save(ctx, job); saveErr != nil {
				errs = append(errs, fmt.Errorf("case 5 job save failed: %w", saveErr))
				continue
			}

			if task != nil {
				if recErr := r.taskRepo.RecoverTaskState(ctx, task.ID, recording.TransferFailed, "staged source file and target backend object both missing/invalid during recovery"); recErr != nil {
					errs = append(errs, fmt.Errorf("case 5 task recover failed: %w", recErr))
				}
			}
		}
	}

	return errors.Join(errs...)
}

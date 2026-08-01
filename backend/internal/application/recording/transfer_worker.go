// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package recording

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/recording"
	"github.com/ManuGH/xg2g/internal/infra/storage"
)

const FinalizationLeaseDuration = 2 * time.Minute

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

// ProcessNextTask claims and processes one TransferTask idempotently under lease heartbeat. Returns true if a task was processed.
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
		saveErr := w.taskRepo.SaveTaskLeased(ctx, task, w.workerID, task.LeaseToken)
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

	// Create cancelable sub-context bound to worker lease heartbeat
	transferCtx, cancelTransfer := context.WithCancel(ctx)
	defer cancelTransfer()

	var heartbeatErrMu sync.Mutex
	var heartbeatErr error

	// Launch background heartbeat ticker renewing task lease every 15 seconds
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	stopHeartbeat := make(chan struct{})
	var heartbeatWg sync.WaitGroup
	var stopOnce sync.Once
	heartbeatWg.Add(1)

	go func() {
		defer heartbeatWg.Done()
		for {
			select {
			case <-ticker.C:
				_, renewErr := w.taskRepo.RenewTaskLease(ctx, task.ID, w.workerID, task.LeaseToken, 10*time.Minute)
				if renewErr != nil {
					// Store exact heartbeat failure error thread-safely
					heartbeatErrMu.Lock()
					heartbeatErr = fmt.Errorf("lease heartbeat renewal failed: %w", renewErr)
					heartbeatErrMu.Unlock()

					// CANCEL TRANSFER CONTEXT IMMEDIATELY!
					cancelTransfer()
					return
				}
			case <-stopHeartbeat:
				return
			case <-transferCtx.Done():
				return
			}
		}
	}()

	stopHeartbeatAndJoin := func() {
		stopOnce.Do(func() {
			close(stopHeartbeat)
		})
		heartbeatWg.Wait()
	}

	finishWithCheck := func(execErr error) error {
		stopHeartbeatAndJoin()
		heartbeatErrMu.Lock()
		hbErr := heartbeatErr
		heartbeatErrMu.Unlock()
		if hbErr != nil {
			if execErr != nil {
				return fmt.Errorf("%w (execution error: %v)", hbErr, execErr)
			}
			return hbErr
		}
		return execErr
	}

	// 1. Idempotency Pre-Check: Check if target file already exists on backend
	alreadyCommitted := false
	targetInfo, statErr := backend.Stat(transferCtx, task.TargetObjectKey)
	if statErr == nil && targetInfo.SizeBytes == task.ExpectedSize {
		alreadyCommitted = true
	}

	// 2. Perform CommitFile only if not already committed
	if !alreadyCommitted {
		srcFilename := task.SourceFilename
		if srcFilename == "" {
			srcFilename = filepath.Base(task.TargetObjectKey)
		}
		srcRel := filepath.Clean(filepath.Join("jobs", task.SourceWorkspaceID, "finalized", srcFilename))
		srcAbsPath, err := recording.SanitizeAndValidateRelativePath(w.stagingRoot, srcRel)
		if err != nil {
			return finishWithCheck(fmt.Errorf("failed to sanitize source staging path: %w", err))
		}
		fullSrcPath := filepath.Join(w.stagingRoot, srcAbsPath)

		info, err := os.Stat(fullSrcPath)
		if err != nil {
			return finishWithCheck(fmt.Errorf("source staged file not found: %w", err))
		}
		if info.Size() != task.ExpectedSize {
			return finishWithCheck(fmt.Errorf("staged source size mismatch: got %d, expected %d", info.Size(), task.ExpectedSize))
		}

		if err := backend.CommitFile(transferCtx, fullSrcPath, task.TargetObjectKey); err != nil {
			return finishWithCheck(fmt.Errorf("failed to commit file to backend: %w", err))
		}

		// Stat Verification on Target Backend
		targetInfo, statErr = backend.Stat(transferCtx, task.TargetObjectKey)
		if statErr != nil {
			return finishWithCheck(fmt.Errorf("failed to verify committed file via backend.Stat: %w", statErr))
		}
		if targetInfo.SizeBytes != task.ExpectedSize {
			return finishWithCheck(fmt.Errorf("target committed size mismatch: got %d, expected %d", targetInfo.SizeBytes, task.ExpectedSize))
		}
	}

	// MANDATORY EXPLICIT FINALIZATION LEASE RENEWAL:
	// Prior to stopping heartbeat, guarantee a full 2-minute finalization lease extension
	_, finalRenewErr := w.taskRepo.RenewTaskLease(ctx, task.ID, w.workerID, task.LeaseToken, FinalizationLeaseDuration)
	if finalRenewErr != nil {
		stopHeartbeatAndJoin()
		return fmt.Errorf("failed to secure finalization lease before saving completion states: %w", finalRenewErr)
	}

	// Stop & join heartbeat goroutine BEFORE executing persistent state transitions
	if hbCheckErr := finishWithCheck(nil); hbCheckErr != nil {
		return hbCheckErr
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

	// 4. Transition Job to StateCompleted idempotently using domain transition
	job, err := w.jobRepo.Get(ctx, task.JobID)
	if err != nil {
		return fmt.Errorf("failed to fetch job %s: %w", task.JobID, err)
	}
	if job.State != recording.StateCompleted {
		completedJob, err := job.TransitionState(recording.StateCompleted, "")
		if err != nil {
			return fmt.Errorf("failed to transition job to COMPLETED: %w", err)
		}
		if err := w.jobRepo.Save(ctx, completedJob); err != nil {
			return fmt.Errorf("failed to save COMPLETED job: %w", err)
		}
	}

	// 5. Mark TransferTask COMPLETED under CAS lease ownership
	task.State = recording.TransferCompleted
	if err := w.taskRepo.SaveTaskLeased(ctx, task, w.workerID, task.LeaseToken); err != nil {
		return fmt.Errorf("failed to save COMPLETED transfer task under lease: %w", err)
	}

	// 6. Safe Cleanup of Staged Workspace temporary subdirectories (logged warnings on failure)
	jobDir := filepath.Join(w.stagingRoot, "jobs", task.SourceWorkspaceID)
	for _, sub := range []string{"segments", "finalized", "work"} {
		subDir := filepath.Join(jobDir, sub)
		if err := os.RemoveAll(subDir); err != nil && !os.IsNotExist(err) {
			log.Printf("WARN: failed to cleanup staged workspace directory '%s': %v", subDir, err)
		}
	}

	return nil
}

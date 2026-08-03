// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package staging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/recording"
	"github.com/ManuGH/xg2g/internal/infra/storage"
)

var (
	ErrMultipleAssetsForJob = errors.New("multiple recording assets found for single job during reconciliation")
	ErrUnverifiableTarget   = errors.New("expected target size <= 0; target cannot be verified")
	ErrTargetSizeMismatch   = errors.New("target file size mismatch on storage backend during reconciliation")
	ErrManualIntervention   = errors.New("recovery requires manual intervention: finalization manifest or staged file unrecoverable")
)

// ObjectProbeState explicitly classifies physical target object status on storage backends.
type ObjectProbeState int

const (
	ProbeValid ObjectProbeState = iota
	ProbeSizeMismatch
	ProbeNotFound
	ProbeBackendOffline
	ProbeInvalidMetadata
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

// ReconcileAll scans and resolves all incomplete job, asset, and task states across equal 3-inventories without error swallowing.
func (r *StartupReconciler) ReconcileAll(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	var errs []error

	// 1. Load Job Inventory with Explicit Manifest Error Reporting
	inventory, err := r.jobRepo.ListAllInventory(ctx)
	if err != nil {
		return fmt.Errorf("failed to scan job inventory during reconciliation: %w", err)
	}
	for _, issue := range inventory.Issues {
		errs = append(errs, fmt.Errorf("corrupted manifest issue at %s: %s", issue.Path, issue.Error))
	}

	allAssets, err := r.assetRepo.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list assets during reconciliation: %w", err)
	}

	tasksList, err := r.taskRepo.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list transfer tasks during reconciliation: %w", err)
	}

	// 2. Build UNION Set of All Job IDs across all 3 inventories
	jobIDSet := make(map[string]bool)
	jobsMap := make(map[string]*recording.RecordingJob)
	for _, j := range inventory.Jobs {
		jobIDSet[j.ID] = true
		jobsMap[j.ID] = j
	}
	assetsMap := make(map[string][]*recording.RecordingAsset)
	for _, a := range allAssets {
		jobIDSet[a.JobID] = true
		assetsMap[a.JobID] = append(assetsMap[a.JobID], a)
	}
	rawTasksMap := make(map[string][]*recording.TransferTask)
	for _, t := range tasksList {
		jobIDSet[t.JobID] = true
		rawTasksMap[t.JobID] = append(rawTasksMap[t.JobID], t)
	}

	// 3. Global Multi-Asset Conflict Detection
	for jobID, jobAssets := range assetsMap {
		if len(jobAssets) > 1 {
			errs = append(errs, fmt.Errorf("%w: job %s has %d assets", ErrMultipleAssetsForJob, jobID, len(jobAssets)))
		}
	}

	// 4. Evidentiary Task Ranking per JobID incorporating Physical Probe & Staging Evidence + Tie Breakers
	taskMap := make(map[string]*recording.TransferTask)
	abortedJobIDs := make(map[string]bool)

	for jobID, tasks := range rawTasksMap {
		var best *recording.TransferTask
		var conflictAborted bool

		for _, t := range tasks {
			if best == nil {
				best = t
				continue
			}

			scoreT := r.scoreTaskEvidence(ctx, t, assetsMap[jobID])
			scoreBest := r.scoreTaskEvidence(ctx, best, assetsMap[jobID])

			tIsBetter := false
			if scoreT > scoreBest {
				tIsBetter = true
			} else if scoreT == scoreBest {
				// Deterministic Tie-Breaker: score > UpdatedAt > CreatedAt > ID
				if t.UpdatedAt.After(best.UpdatedAt) {
					tIsBetter = true
				} else if t.UpdatedAt.Equal(best.UpdatedAt) {
					if t.CreatedAt.After(best.CreatedAt) {
						tIsBetter = true
					} else if t.CreatedAt.Equal(best.CreatedAt) && t.ID < best.ID {
						tIsBetter = true
					}
				}
			}

			inferior := best
			winner := t
			if !tIsBetter {
				inferior = t
				winner = best
			}

			// Active Lease Conflict Abort Check: If inferior has active lease, ABORT reconciliation for this JobID!
			recErr := r.taskRepo.RecoverTaskState(ctx, inferior.ID, nil, true, recording.TransferFailed, "superseded by higher evidentiary rank task")
			if recErr != nil {
				errs = append(errs, fmt.Errorf("active lease conflict on task %s for job %s: %w", inferior.ID, jobID, recErr))
				conflictAborted = true
				break
			}
			best = winner
		}

		if conflictAborted {
			abortedJobIDs[jobID] = true
		} else {
			taskMap[jobID] = best
		}
	}

	// 5. Process Union Set of Job IDs
	for jobID := range jobIDSet {
		if abortedJobIDs[jobID] {
			continue // Abort processing for JobIDs with active lease conflicts
		}

		job := jobsMap[jobID]
		jobAssets := assetsMap[jobID]
		task := taskMap[jobID]

		// Skip completed/failed jobs if assets are also clean
		if job != nil && (job.State == recording.StateCompleted || job.State == recording.StateFailed) {
			if len(jobAssets) == 1 && (jobAssets[0].State == recording.AssetAvailable || jobAssets[0].State == recording.AssetMissing) {
				continue
			}
		}

		// Handle "Job Present, Asset Missing" vs "Orphaned Task"
		if len(jobAssets) == 0 {
			if job != nil {
				// Attempt asset reconstruction from FinalizationManifest if staged source exists
				reconstructedAsset, recErr := r.attemptAssetReconstruction(ctx, job, task)
				if recErr == nil && reconstructedAsset != nil {
					if saveErr := r.assetRepo.Save(ctx, reconstructedAsset, 0); saveErr != nil {
						errs = append(errs, fmt.Errorf("failed to save reconstructed asset for job %s: %w", jobID, saveErr))
						continue
					}
					jobAssets = []*recording.RecordingAsset{reconstructedAsset}
				} else {
					if job.State != recording.StateFailed {
						failedJob, transErr := job.TransitionState(recording.StateFailed, "recovery requires manual intervention: finalization manifest or staged file unrecoverable")
						if transErr == nil {
							if saveErr := r.jobRepo.Save(ctx, failedJob, job.Version); saveErr != nil {
								errs = append(errs, fmt.Errorf("failed to save FAILED job %s: %w", jobID, saveErr))
							}
						}
					}
					errs = append(errs, fmt.Errorf("%w for job %s: %v", ErrManualIntervention, jobID, recErr))
					continue
				}
			} else {
				// True Orphaned Task without Job or Asset
				if task != nil && task.State != recording.TransferFailed {
					if recErr := r.taskRepo.RecoverTaskState(ctx, task.ID, nil, true, recording.TransferFailed, "orphaned task without asset or job record"); recErr != nil {
						errs = append(errs, fmt.Errorf("failed to recover orphaned task %s: %w", task.ID, recErr))
					}
				}
				continue
			}
		}

		if len(jobAssets) > 1 {
			continue // Already appended ErrMultipleAssetsForJob error above
		}

		asset := jobAssets[0]

		// Determine expected size strictly
		expectedSize := asset.SizeBytes
		if task != nil && task.ExpectedSize > 0 {
			expectedSize = task.ExpectedSize
		}

		if expectedSize <= 0 {
			errs = append(errs, fmt.Errorf("%w for asset %s", ErrUnverifiableTarget, asset.ID))
			continue
		}

		// Probe Storage Backend Explicitly
		targetBackendID := asset.BackendID
		if targetBackendID == "" && job != nil {
			targetBackendID = job.TargetBackendID
		}

		probe := r.probeTargetObject(ctx, targetBackendID, asset.ObjectKey, expectedSize)

		// Resolve staged source file using asset's persistent SourceFilename
		srcFilename := asset.SourceFilename
		if srcFilename == "" && task != nil && task.SourceFilename != "" {
			srcFilename = task.SourceFilename
		}
		if srcFilename == "" {
			srcFilename = filepath.Base(asset.ObjectKey)
		}

		stagedRelPath := filepath.Join("jobs", jobID, "finalized", srcFilename)
		stagedAbsPath := filepath.Join(r.stagingRoot, stagedRelPath)
		stagedInfo, stagedErr := os.Stat(stagedAbsPath)
		stagedExists := (stagedErr == nil && stagedInfo.Size() == expectedSize)

		// Capability-Based Actionable Policy for ProbeSizeMismatch
		if probe == ProbeSizeMismatch {
			backend := r.backends[targetBackendID]
			if stagedExists && backend != nil && backend.Capabilities().SupportsAtomicReplace {
				// Capability supports atomic replace: set task RETRYING for re-commit
				if task != nil {
					if recErr := r.taskRepo.RecoverTaskState(ctx, task.ID, nil, true, recording.TransferRetrying, "target size mismatch: atomic re-commit scheduled"); recErr != nil {
						errs = append(errs, fmt.Errorf("failed to recover task to RETRYING for asset %s: %w", asset.ID, recErr))
					}
				}
				errs = append(errs, fmt.Errorf("%w for asset %s (scheduled atomic replace retry)", ErrTargetSizeMismatch, asset.ID))
			} else {
				// Atomic replace not supported or staging file missing: transition to AssetCorrupt, StateFailed
				corruptAsset, transErr := asset.TransitionState(recording.AssetCorrupt)
				if transErr == nil {
					if saveErr := r.assetRepo.Save(ctx, corruptAsset, asset.Version); saveErr != nil {
						errs = append(errs, fmt.Errorf("failed to save CORRUPT asset %s: %w", asset.ID, saveErr))
					}
				}
				if job != nil {
					failedJob, transErr := job.TransitionState(recording.StateFailed, "target file size mismatch and atomic replace unrecoverable")
					if transErr == nil {
						if saveErr := r.jobRepo.Save(ctx, failedJob, job.Version); saveErr != nil {
							errs = append(errs, fmt.Errorf("failed to save FAILED job %s: %w", job.ID, saveErr))
						}
					}
				}
				if task != nil {
					if recErr := r.taskRepo.RecoverTaskState(ctx, task.ID, nil, true, recording.TransferFailed, "target file size mismatch and atomic replace unrecoverable"); recErr != nil {
						errs = append(errs, fmt.Errorf("failed to set FAILED task %s: %w", task.ID, recErr))
					}
				}
				errs = append(errs, fmt.Errorf("%w for asset %s (marked CORRUPT / FAILED)", ErrTargetSizeMismatch, asset.ID))
			}
			continue
		}

		if probe == ProbeInvalidMetadata {
			errs = append(errs, fmt.Errorf("invalid metadata for asset %s (missing backend ID or object key); skipping recovery", asset.ID))
			continue
		}

		// If backend is temporary offline, NEVER perform destructive state transitions
		if probe == ProbeBackendOffline {
			errs = append(errs, fmt.Errorf("storage backend '%s' offline or un-reachable for asset %s; skipping destructive recovery", targetBackendID, asset.ID))
			continue
		}

		// Case 1: Target file exists with EXACT expected size + Asset TRANSFER_PENDING
		if probe == ProbeValid && asset.State == recording.AssetTransferPending {
			availAsset, transErr := asset.TransitionState(recording.AssetAvailable)
			if transErr != nil {
				errs = append(errs, fmt.Errorf("case 1 asset transition failed: %w", transErr))
				continue
			}
			availAsset.SizeBytes = expectedSize
			if saveErr := r.assetRepo.Save(ctx, availAsset, asset.Version); saveErr != nil {
				errs = append(errs, fmt.Errorf("case 1 asset save failed: %w", saveErr))
				continue
			}

			if job != nil {
				completedJob, transErr := job.TransitionState(recording.StateCompleted, "")
				if transErr == nil {
					if saveErr := r.jobRepo.Save(ctx, completedJob, job.Version); saveErr != nil {
						errs = append(errs, fmt.Errorf("case 1 job save failed: %w", saveErr))
						continue
					}
				}
			}

			if task != nil && task.State != recording.TransferCompleted {
				if recErr := r.taskRepo.RecoverTaskState(ctx, task.ID, nil, true, recording.TransferCompleted, ""); recErr != nil {
					errs = append(errs, fmt.Errorf("case 1 task recover failed: %w", recErr))
				}
			}
			continue
		}

		// Case 2: Asset AVAILABLE + Job TRANSFERRING / WAITING_FOR_TARGET (Strict Target Verification Required!)
		if asset.State == recording.AssetAvailable && (job == nil || job.State != recording.StateCompleted) {
			if probe == ProbeValid {
				if job != nil {
					completedJob, transErr := job.TransitionState(recording.StateCompleted, "")
					if transErr == nil {
						if saveErr := r.jobRepo.Save(ctx, completedJob, job.Version); saveErr != nil {
							errs = append(errs, fmt.Errorf("case 2 job save failed: %w", saveErr))
						}
					}
				}
				if task != nil && task.State != recording.TransferCompleted {
					if recErr := r.taskRepo.RecoverTaskState(ctx, task.ID, nil, true, recording.TransferCompleted, ""); recErr != nil {
						errs = append(errs, fmt.Errorf("case 2 task recover failed: %w", recErr))
					}
				}
			} else {
				errs = append(errs, fmt.Errorf("case 2: asset %s is AVAILABLE but target file missing or size mismatch on backend", asset.ID))
			}
			continue
		}

		// Case 3: Asset TRANSFER_PENDING + Job WAITING_FOR_TARGET + No TransferTask + Staged file exists
		if asset.State == recording.AssetTransferPending && task == nil && stagedExists {
			targetBackend := targetBackendID
			if targetBackend == "" {
				targetBackend = "default"
			}
			newTask, taskErr := recording.NewTransferTask(
				"task_rec_"+jobID,
				jobID,
				asset.ID,
				jobID,
				srcFilename,
				targetBackend,
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

		// Case 4: Task COMPLETED + Asset TRANSFER_PENDING (Strict Target Verification Required!)
		if task != nil && task.State == recording.TransferCompleted && asset.State != recording.AssetAvailable {
			if probe == ProbeValid {
				availAsset, transErr := asset.TransitionState(recording.AssetAvailable)
				if transErr != nil {
					errs = append(errs, fmt.Errorf("case 4 asset transition failed: %w", transErr))
					continue
				}
				availAsset.SizeBytes = expectedSize
				if saveErr := r.assetRepo.Save(ctx, availAsset, asset.Version); saveErr != nil {
					errs = append(errs, fmt.Errorf("case 4 asset save failed: %w", saveErr))
					continue
				}

				if job != nil {
					completedJob, transErr := job.TransitionState(recording.StateCompleted, "")
					if transErr == nil {
						if saveErr := r.jobRepo.Save(ctx, completedJob, job.Version); saveErr != nil {
							errs = append(errs, fmt.Errorf("case 4 job save failed: %w", saveErr))
						}
					}
				}
			} else {
				errs = append(errs, fmt.Errorf("case 4: task %s is COMPLETED but target file missing or size mismatch on backend", task.ID))
			}
			continue
		}

		// Case 5: ONLY on confirmed ProbeNotFound AND staging missing
		if probe == ProbeNotFound && !stagedExists && asset.State == recording.AssetTransferPending {
			corruptAsset, transErr := asset.TransitionState(recording.AssetMissing)
			if transErr != nil {
				errs = append(errs, fmt.Errorf("case 5 asset transition failed: %w", transErr))
				continue
			}
			if saveErr := r.assetRepo.Save(ctx, corruptAsset, asset.Version); saveErr != nil {
				errs = append(errs, fmt.Errorf("case 5 asset save failed: %w", saveErr))
				continue
			}

			if job != nil {
				failedJob, transErr := job.TransitionState(recording.StateFailed, "staged source file and target backend object confirmed missing during recovery")
				if transErr == nil {
					if saveErr := r.jobRepo.Save(ctx, failedJob, job.Version); saveErr != nil {
						errs = append(errs, fmt.Errorf("case 5 job save failed: %w", saveErr))
						continue
					}
				}
			}

			if task != nil {
				if recErr := r.taskRepo.RecoverTaskState(ctx, task.ID, nil, true, recording.TransferFailed, "staged source file and target backend object confirmed missing during recovery"); recErr != nil {
					errs = append(errs, fmt.Errorf("case 5 task recover failed: %w", recErr))
				}
			}
		}
	}

	return errors.Join(errs...)
}

func (r *StartupReconciler) attemptAssetReconstruction(ctx context.Context, job *recording.RecordingJob, task *recording.TransferTask) (*recording.RecordingAsset, error) {
	manifestPath := filepath.Join(r.stagingRoot, "jobs", job.ID, "finalized", "finalization_manifest.json")
	data, err := os.ReadFile(manifestPath) //nolint:gosec // G304: manifest path
	if err != nil {
		return nil, fmt.Errorf("finalization manifest missing: %w", err)
	}

	var m FinalizationManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("finalization manifest corrupt: %w", err)
	}

	srcAbsPath := filepath.Join(r.stagingRoot, "jobs", job.ID, "finalized", m.SourceFilename)
	info, err := os.Stat(srcAbsPath)
	if err != nil || info.Size() != m.SizeBytes {
		return nil, fmt.Errorf("staged source file missing or size mismatch")
	}

	asset, err := recording.NewRecordingAsset(m.AssetID, m.JobID, m.Title, m.ServiceRef, m.TargetBackendID, m.TargetObjectKey, m.Container)
	if err != nil {
		return nil, err
	}
	asset.ProfileID = m.ProfileID
	asset.SourceFilename = m.SourceFilename
	asset.SizeBytes = m.SizeBytes
	asset.ManagementMode = m.ManagementMode
	asset.DeletePolicy = m.DeletePolicy
	asset.DurationSeconds = m.DurationSeconds
	asset.RecordedStart = m.RecordedStart
	asset.RecordedEnd = m.RecordedEnd
	asset.Completeness = m.Completeness
	asset.State = recording.AssetTransferPending

	return asset, nil
}

func (r *StartupReconciler) probeTargetObject(ctx context.Context, backendID, objectKey string, expectedSize int64) ObjectProbeState {
	if backendID == "" || objectKey == "" {
		return ProbeInvalidMetadata
	}
	backend, ok := r.backends[backendID]
	if !ok || backend == nil {
		return ProbeBackendOffline
	}

	info, err := backend.Stat(ctx, objectKey)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotFound) {
			return ProbeNotFound
		}
		return ProbeBackendOffline
	}

	if info.SizeBytes == expectedSize {
		return ProbeValid
	}
	return ProbeSizeMismatch
}

func (r *StartupReconciler) scoreTaskEvidence(ctx context.Context, t *recording.TransferTask, assets []*recording.RecordingAsset) int {
	var asset *recording.RecordingAsset
	if len(assets) == 1 {
		asset = assets[0]
	}

	expectedSize := t.ExpectedSize
	if expectedSize <= 0 && asset != nil {
		expectedSize = asset.SizeBytes
	}

	backendID := t.TargetBackendID
	objKey := t.TargetObjectKey
	if backendID == "" && asset != nil {
		backendID = asset.BackendID
	}
	if objKey == "" && asset != nil {
		objKey = asset.ObjectKey
	}

	probe := r.probeTargetObject(ctx, backendID, objKey, expectedSize)
	if t.State == recording.TransferCompleted && probe == ProbeValid {
		return 100
	}

	now := time.Now()
	if t.State == recording.TransferRunning && now.Before(t.LeaseExpiresAt) {
		return 80
	}

	// Check if staged source file exists on disk with exact expected size
	srcFilename := t.SourceFilename
	if srcFilename == "" && asset != nil {
		srcFilename = asset.SourceFilename
	}
	if srcFilename == "" {
		srcFilename = filepath.Base(objKey)
	}
	srcAbsPath := filepath.Join(r.stagingRoot, "jobs", t.JobID, "finalized", srcFilename)
	info, err := os.Stat(srcAbsPath)
	stagedValid := (err == nil && info.Size() == expectedSize)

	if (t.State == recording.TransferPending || t.State == recording.TransferRetrying) && stagedValid {
		return 60
	}
	if t.State == recording.TransferPending || t.State == recording.TransferRetrying {
		return 40
	}
	if t.State == recording.TransferCompleted && probe == ProbeNotFound {
		return 20
	}
	return 10
}

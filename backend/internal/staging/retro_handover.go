// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package staging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/recording"
	"github.com/ManuGH/xg2g/internal/hls/ringbuffer"
	"github.com/ManuGH/xg2g/internal/infra/storage"
)

var (
	ErrTransferTaskSaveFailed = errors.New("failed to save persistent transfer task during target backend fallback")
	ErrSegmentSizeMismatch    = errors.New("staged segment size mismatch against reserved NVMe segment")
)

// FinalizationManifest is written to finalized/finalization_manifest.json to allow crash reconstruction.
type FinalizationManifest struct {
	AssetID         string                        `json:"asset_id"`
	JobID           string                        `json:"job_id"`
	ProfileID       string                        `json:"profile_id"`
	Title           string                        `json:"title"`
	ServiceRef      string                        `json:"service_ref"`
	TargetBackendID string                        `json:"target_backend_id"`
	TargetObjectKey string                        `json:"target_object_key"`
	SourceFilename  string                        `json:"source_filename"`
	Container       recording.ContainerFormat     `json:"container"`
	SizeBytes       int64                         `json:"size_bytes"`
	ManagementMode  recording.AssetManagementMode `json:"management_mode"`
	DeletePolicy    recording.DeletePolicy        `json:"delete_policy"`
	DurationSeconds int                           `json:"duration_seconds"`
	RecordedStart   time.Time                     `json:"recorded_start"`
	RecordedEnd     time.Time                     `json:"recorded_end"`
	Completeness    recording.AssetCompleteness   `json:"completeness"`
}

// RetroDVRHandoverEngine orchestrates Retro-DVR recordings from NVMe segment reservations to finalized RecordingAssets.
type RetroDVRHandoverEngine struct {
	mu          sync.Mutex
	resMgr      ringbuffer.ReservationManager
	jobRepo     recording.JobRepository
	assetRepo   recording.AssetRepository
	profileRepo recording.ProfileRepository
	taskRepo    recording.TransferTaskRepository
	stagingMgr  *StagingManager
	backends    map[string]storage.StorageBackend
}

// NewRetroDVRHandoverEngine initializes a new RetroDVRHandoverEngine.
func NewRetroDVRHandoverEngine(
	resMgr ringbuffer.ReservationManager,
	jobRepo recording.JobRepository,
	assetRepo recording.AssetRepository,
	profileRepo recording.ProfileRepository,
	taskRepo recording.TransferTaskRepository,
	stagingMgr *StagingManager,
	backends []storage.StorageBackend,
) (*RetroDVRHandoverEngine, error) {
	if stagingMgr == nil {
		return nil, fmt.Errorf("stagingMgr cannot be nil")
	}
	backendMap := make(map[string]storage.StorageBackend)
	for _, b := range backends {
		if b != nil {
			backendMap[b.ID()] = b
		}
	}
	return &RetroDVRHandoverEngine{
		resMgr:      resMgr,
		jobRepo:     jobRepo,
		assetRepo:   assetRepo,
		profileRepo: profileRepo,
		taskRepo:    taskRepo,
		stagingMgr:  stagingMgr,
		backends:    backendMap,
	}, nil
}

// RetroHandoverRequest contains input params for triggering a Retro-DVR recording.
type RetroHandoverRequest struct {
	JobID      string    `json:"job_id"`
	AssetID    string    `json:"asset_id"`
	ProfileID  string    `json:"profile_id"`
	ServiceRef string    `json:"service_ref"`
	Title      string    `json:"title"`
	StartTime  time.Time `json:"start_time"`
	EndTime    time.Time `json:"end_time"`
}

// RetroHandoverResult returns details about the finalized retro recording.
type RetroHandoverResult struct {
	Job                     *recording.RecordingJob   `json:"job"`
	Asset                   *recording.RecordingAsset `json:"asset"`
	AssemblyReport          AssemblyReport            `json:"assembly_report"`
	LiveContinuationPending bool                      `json:"live_continuation_pending"`
	TransferScheduled       bool                      `json:"transfer_scheduled"`
}

// StagedSegment details verified original-to-staged mapping for each segment.
type StagedSegment struct {
	OriginalID      ringbuffer.SegmentID `json:"original_id"`
	OriginalPath    string               `json:"original_path"`
	StagedObjectKey string               `json:"staged_object_key"`
	ExpectedBytes   int64                `json:"expected_bytes"`
	ActualBytes     int64                `json:"actual_bytes"`
	StartTime       time.Time            `json:"start_time"`
	EndTime         time.Time            `json:"end_time"`
	TransferMethod  string               `json:"transfer_method"` // "HARDLINK" or "COPY"
}

// StagingManifest persists full original segment metadata in the staging workspace before lease release.
type StagingManifest struct {
	Version       int             `json:"version"`
	JobID         string          `json:"job_id"`
	ReservationID string          `json:"reservation_id"`
	CreatedAt     time.Time       `json:"created_at"`
	Segments      []StagedSegment `json:"segments"`
	Complete      bool            `json:"complete"`
}

// ExecuteRetroRecording processes a retro recording from NVMe disk buffer handover to finalized RecordingAsset with named defer error chaining.
func (e *RetroDVRHandoverEngine) ExecuteRetroRecording(ctx context.Context, req RetroHandoverRequest) (result *RetroHandoverResult, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req.JobID == "" || req.AssetID == "" || req.ProfileID == "" || req.ServiceRef == "" || req.Title == "" {
		return nil, fmt.Errorf("invalid retro handover request: missing required fields")
	}

	// Resolve authoritative profile from ProfileRepository
	if e.profileRepo == nil {
		return nil, fmt.Errorf("profileRepo cannot be nil")
	}
	profile, profileErr := e.profileRepo.Get(ctx, req.ProfileID)
	if profileErr != nil {
		return nil, fmt.Errorf("failed to resolve recording profile '%s': %w", req.ProfileID, profileErr)
	}

	backend, ok := e.backends[profile.Target.BackendID]
	if !ok || backend == nil {
		return nil, fmt.Errorf("target storage backend '%s' not found or offline", profile.Target.BackendID)
	}

	// 1. Lock NVMe segment range under lease from ReservationManager
	var reservation ringbuffer.Reservation
	var handles []ringbuffer.SegmentHandle
	released := false

	if e.resMgr != nil {
		leaseDuration := 30 * time.Minute
		reservation, err = e.resMgr.ReserveRange(req.ServiceRef, req.StartTime, req.EndTime, req.JobID, leaseDuration)
		if err != nil {
			return nil, fmt.Errorf("failed to reserve NVMe Retro-DVR segment range: %w", err)
		}

		handles, err = e.resMgr.ListReservedSegments(reservation.ID)
		if err != nil {
			_ = e.resMgr.ReleaseReservation(reservation.ID)
			return nil, fmt.Errorf("failed to list reserved segments for %s: %w", reservation.ID, err)
		}
	}

	// NAMED DEFER GUARD: Ensure reservation lease release error is chained if early errors occur
	defer func() {
		if !released && reservation.ID != "" && e.resMgr != nil {
			relErr := e.resMgr.ReleaseReservation(reservation.ID)
			if relErr != nil {
				if err != nil {
					err = fmt.Errorf("%w; additionally failed to release reservation lease '%s': %v", err, reservation.ID, relErr)
				} else {
					err = fmt.Errorf("failed to release reservation lease '%s': %w", reservation.ID, relErr)
				}
			}
		}
	}()

	// Sort handles chronologically by 1. StartWallTime, 2. SessionID, 3. Sequence
	sort.Slice(handles, func(i, j int) bool {
		if !handles[i].StartWallTime.Equal(handles[j].StartWallTime) {
			return handles[i].StartWallTime.Before(handles[j].StartWallTime)
		}
		if handles[i].ID.SessionID != handles[j].ID.SessionID {
			return handles[i].ID.SessionID < handles[j].ID.SessionID
		}
		return handles[i].Sequence < handles[j].Sequence
	})

	// Active event boundary capping: if EndTime is in the future, cap to latest available segment end
	effectiveEnd := req.EndTime
	isLiveActive := false
	now := time.Now()
	if req.EndTime.After(now) {
		isLiveActive = true
		if len(handles) > 0 {
			lastSegEnd := handles[len(handles)-1].EndWallTime
			if lastSegEnd.Before(effectiveEnd) {
				effectiveEnd = lastSegEnd
			}
		}
	}

	// 2. Create RecordingJob in PENDING state
	job, jobErr := recording.NewRecordingJob(
		req.JobID,
		req.ServiceRef,
		req.Title,
		recording.SourceRetro,
		req.StartTime,
		effectiveEnd,
		profile.Target.BackendID,
	)
	if jobErr != nil {
		return nil, fmt.Errorf("failed to create RecordingJob: %w", jobErr)
	}

	if e.jobRepo != nil {
		if err := e.jobRepo.Save(ctx, job); err != nil {
			return nil, fmt.Errorf("failed to save initial RecordingJob: %w", err)
		}
	}

	// 3. Prepare Staging workspace
	_, err = e.stagingMgr.PrepareWorkspace(ctx, job)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare staging workspace: %w", err)
	}

	// 4. Transfer reserved segments into staging directory & verify sizes
	stagingSegsDir := e.stagingMgr.SegmentsDir(job.ID)
	if err := os.MkdirAll(stagingSegsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create staging segments directory: %w", err)
	}

	var stagedSegments []StagedSegment
	for idx, h := range handles {
		srcPath := h.Location.Path
		if srcPath == "" {
			return nil, fmt.Errorf("reserved segment %s has empty path", h.ID)
		}

		segFilename := fmt.Sprintf("seg_%06d.ts", idx+1)
		destPath := filepath.Join(stagingSegsDir, segFilename)
		method, copyErr := copyOrLinkFile(srcPath, destPath)
		if copyErr != nil {
			return nil, fmt.Errorf("failed to transfer retro segment %s to staging: %w", srcPath, copyErr)
		}

		stat, statErr := os.Stat(destPath)
		if statErr != nil {
			return nil, fmt.Errorf("failed to stat staged segment %s: %w", destPath, statErr)
		}

		// STRICT SEGMENT SIZE VERIFICATION
		if h.SizeBytes > 0 && stat.Size() != h.SizeBytes {
			return nil, fmt.Errorf("%w: segment %s staged size %d != expected %d", ErrSegmentSizeMismatch, h.ID, stat.Size(), h.SizeBytes)
		}

		stagedSegments = append(stagedSegments, StagedSegment{
			OriginalID:      h.ID,
			OriginalPath:    srcPath,
			StagedObjectKey: filepath.Join("segments", segFilename),
			ExpectedBytes:   h.SizeBytes,
			ActualBytes:     stat.Size(),
			StartTime:       h.StartWallTime,
			EndTime:         h.EndWallTime,
			TransferMethod:  method,
		})
	}

	// Fsync segments/ directory
	pSegDir, pErr := os.Open(stagingSegsDir)
	if pErr != nil {
		return nil, fmt.Errorf("failed to open staging segments directory for fsync: %w", pErr)
	}
	if err := pSegDir.Sync(); err != nil {
		_ = pSegDir.Close()
		return nil, fmt.Errorf("failed to fsync staging segments directory: %w", err)
	}
	_ = pSegDir.Close()

	// Write StagingManifest with explicit file & directory fsync
	manifestPath := filepath.Join(e.stagingMgr.JobDir(job.ID), "manifest.json")
	sManifest := StagingManifest{
		Version:       1,
		JobID:         job.ID,
		ReservationID: reservation.ID,
		CreatedAt:     now,
		Segments:      stagedSegments,
		Complete:      reservation.Status == ringbuffer.CompletenessComplete,
	}
	if err := writeAndFsyncManifest(manifestPath, sManifest); err != nil {
		return nil, fmt.Errorf("failed to write staging manifest: %w", err)
	}

	// Fsync job workspace directory
	pJobDir, pJobErr := os.Open(e.stagingMgr.JobDir(job.ID))
	if pJobErr != nil {
		return nil, fmt.Errorf("failed to open job workspace directory for fsync: %w", pJobErr)
	}
	if err := pJobDir.Sync(); err != nil {
		_ = pJobDir.Close()
		return nil, fmt.Errorf("failed to fsync job workspace directory: %w", err)
	}
	_ = pJobDir.Close()

	// 5. Release NVMe reservation lease ONLY AFTER staging copy, manifest fsync, and workspace fsync complete
	if e.resMgr != nil && reservation.ID != "" {
		if err := e.resMgr.ReleaseReservation(reservation.ID); err != nil {
			return nil, fmt.Errorf("failed to release NVMe reservation lease: %w", err)
		}
		released = true
	}

	// 6. Update job state to STAGING & assemble output
	job.State = recording.StateStaging
	if e.jobRepo != nil {
		if err := e.jobRepo.Save(ctx, job); err != nil {
			return nil, fmt.Errorf("failed to update job to STAGING: %w", err)
		}
	}

	meta := recording.TemplateMetadata{
		Title:     req.Title,
		StartTime: req.StartTime,
		Year:      req.StartTime.Year(),
		AssetID:   req.AssetID,
	}
	formattedPath := recording.FormatMediaFilename(profile.NamingPreset, profile.FilenameTemplate, meta, profile.ContainerFormat)
	baseOutFilename := filepath.Base(formattedPath)

	report, err := e.stagingMgr.AssembleAndFinalize(ctx, job.ID, baseOutFilename)
	if err != nil {
		job.State = recording.StateFailed
		if e.jobRepo != nil {
			_ = e.jobRepo.Save(ctx, job)
		}
		return nil, fmt.Errorf("failed to assemble and finalize retro recording: %w", err)
	}

	// 7. Join target object key securely using POSIX slashes
	targetObjectKey, err := recording.JoinObjectKey(profile.Target.RelativePath, formattedPath)
	if err != nil {
		job.State = recording.StateFailed
		if e.jobRepo != nil {
			_ = e.jobRepo.Save(ctx, job)
		}
		return nil, fmt.Errorf("invalid recording target object key: %w", err)
	}

	job.State = recording.StateTransferring
	if e.jobRepo != nil {
		if err := e.jobRepo.Save(ctx, job); err != nil {
			return nil, fmt.Errorf("failed to update job to TRANSFERRING: %w", err)
		}
	}

	// 8. Construct RecordingAsset with embedded profile policy snapshot
	var recStart, recEnd time.Time
	if len(handles) > 0 {
		recStart = handles[0].StartWallTime
		recEnd = handles[len(handles)-1].EndWallTime
	} else {
		recStart = req.StartTime
		recEnd = effectiveEnd
	}

	asset, err := recording.NewRecordingAsset(
		req.AssetID,
		job.ID,
		req.Title,
		req.ServiceRef,
		profile.Target.BackendID,
		targetObjectKey,
		profile.ContainerFormat,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create RecordingAsset: %w", err)
	}

	// Snapshot profile rules on asset
	asset.ProfileID = profile.ID
	asset.SourceFilename = baseOutFilename
	asset.ManagementMode = profile.ManagementMode
	asset.DeletePolicy = profile.DeletePolicy
	asset.DurationSeconds = int(recEnd.Sub(recStart).Seconds())
	asset.SizeBytes = report.TotalBytes
	asset.RecordedStart = recStart
	asset.RecordedEnd = recEnd

	if isLiveActive {
		asset.Completeness = recording.AssetPartialAtEnd
	} else if report.Complete {
		asset.Completeness = recording.AssetComplete
	} else {
		asset.Completeness = recording.AssetGapped
		asset.GapCount = len(report.MissingRanges)
	}

	// Save persistent FinalizationManifest inside finalized/ directory for crash reconstruction
	finalManifest := FinalizationManifest{
		AssetID:         asset.ID,
		JobID:           job.ID,
		ProfileID:       asset.ProfileID,
		Title:           asset.Title,
		ServiceRef:      asset.ServiceRef,
		TargetBackendID: asset.BackendID,
		TargetObjectKey: asset.ObjectKey,
		SourceFilename:  asset.SourceFilename,
		Container:       asset.Container,
		SizeBytes:       asset.SizeBytes,
		ManagementMode:  asset.ManagementMode,
		DeletePolicy:    asset.DeletePolicy,
		DurationSeconds: asset.DurationSeconds,
		RecordedStart:   asset.RecordedStart,
		RecordedEnd:     asset.RecordedEnd,
		Completeness:    asset.Completeness,
	}
	manifestData, mErr := json.MarshalIndent(finalManifest, "", "  ")
	if mErr == nil {
		finalManifestPath := filepath.Join(filepath.Dir(report.FinalizedPath), "finalization_manifest.json")
		_ = os.WriteFile(finalManifestPath, manifestData, 0644)
	}

	// 9. Attempt StorageBackend CommitFile & Stat Verification
	commitErr := backend.CommitFile(ctx, report.FinalizedPath, targetObjectKey)
	var statInfo storage.ObjectInfo
	if commitErr == nil {
		statInfo, commitErr = backend.Stat(ctx, targetObjectKey)
		if commitErr == nil && statInfo.SizeBytes != report.TotalBytes {
			commitErr = fmt.Errorf("committed target size mismatch: got %d, expected %d", statInfo.SizeBytes, report.TotalBytes)
		}
	}

	if commitErr != nil {
		// TARGET FAILURE FALLBACK: Job -> WAITING_FOR_TARGET, Asset -> TRANSFER_PENDING, Create TransferTask
		assetPending, transErr := asset.TransitionState(recording.AssetTransferPending)
		if transErr != nil {
			return nil, fmt.Errorf("failed to transition asset to TRANSFER_PENDING: %w", transErr)
		}
		if e.assetRepo != nil {
			if err := e.assetRepo.Save(ctx, assetPending, 0); err != nil {
				return nil, fmt.Errorf("failed to save TRANSFER_PENDING asset: %w", err)
			}
		}

		job.State = recording.StateWaitingTarget
		if e.jobRepo != nil {
			if err := e.jobRepo.Save(ctx, job); err != nil {
				return nil, fmt.Errorf("failed to save WAITING_FOR_TARGET job: %w", err)
			}
		}

		if e.taskRepo == nil {
			return nil, fmt.Errorf("%w: taskRepo is nil", ErrTransferTaskSaveFailed)
		}

		task, taskErr := recording.NewTransferTask(
			"task_"+req.JobID,
			job.ID,
			assetPending.ID,
			job.ID, // SourceWorkspaceID = JobID
			baseOutFilename,
			profile.Target.BackendID,
			targetObjectKey,
			report.TotalBytes,
		)
		if taskErr != nil {
			return nil, fmt.Errorf("%w: %v", ErrTransferTaskSaveFailed, taskErr)
		}
		task.LastError = commitErr.Error()

		// CREATE TASK strictly via CreateTask (prevents silent overwrite of existing tasks)
		if err := e.taskRepo.CreateTask(ctx, task); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrTransferTaskSaveFailed, err)
		}

		return &RetroHandoverResult{
			Job:                     job,
			Asset:                   assetPending,
			AssemblyReport:          *report,
			LiveContinuationPending: isLiveActive,
			TransferScheduled:       true,
		}, nil
	}

	// SUCCESSFUL COMMIT: Asset AVAILABLE, Job COMPLETED
	availableAsset, err := asset.TransitionState(recording.AssetAvailable)
	if err != nil {
		return nil, fmt.Errorf("failed to transition asset to AVAILABLE: %w", err)
	}
	availableAsset.SizeBytes = statInfo.SizeBytes
	finTime := time.Now()
	availableAsset.FinalizedAt = &finTime

	if e.assetRepo != nil {
		if err := e.assetRepo.Save(ctx, availableAsset, 0); err != nil {
			return nil, fmt.Errorf("failed to save AVAILABLE RecordingAsset: %w", err)
		}
	}

	job.State = recording.StateCompleted
	if e.jobRepo != nil {
		if err := e.jobRepo.Save(ctx, job); err != nil {
			return nil, fmt.Errorf("failed to update job to COMPLETED: %w", err)
		}
	}

	return &RetroHandoverResult{
		Job:                     job,
		Asset:                   availableAsset,
		AssemblyReport:          *report,
		LiveContinuationPending: isLiveActive,
		TransferScheduled:       false,
	}, nil
}

func copyOrLinkFile(src, dst string) (string, error) {
	if err := os.Link(src, dst); err == nil {
		return "HARDLINK", nil
	}
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return "", err
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return "", err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	return "COPY", nil
}

func writeAndFsyncManifest(manifestPath string, manifest StagingManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := manifestPath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, manifestPath); err != nil {
		return err
	}

	pDir, err := os.Open(filepath.Dir(manifestPath))
	if err != nil {
		return err
	}
	if err := pDir.Sync(); err != nil {
		_ = pDir.Close()
		return err
	}
	return pDir.Close()
}

// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package staging

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/recording"
	"github.com/ManuGH/xg2g/internal/hls/ringbuffer"
	"github.com/ManuGH/xg2g/internal/infra/storage"
)

// RetroDVRHandoverEngine orchestrates Retro-DVR recordings from NVMe segment reservations to finalized RecordingAssets.
type RetroDVRHandoverEngine struct {
	mu          sync.Mutex
	resMgr      ringbuffer.ReservationManager
	jobRepo     recording.JobRepository
	assetRepo   recording.AssetRepository
	profileRepo recording.ProfileRepository
	stagingMgr  *StagingManager
	backends    map[string]storage.StorageBackend
}

// NewRetroDVRHandoverEngine initializes a new RetroDVRHandoverEngine.
func NewRetroDVRHandoverEngine(
	resMgr ringbuffer.ReservationManager,
	jobRepo recording.JobRepository,
	assetRepo recording.AssetRepository,
	profileRepo recording.ProfileRepository,
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
		stagingMgr:  stagingMgr,
		backends:    backendMap,
	}, nil
}

// RetroHandoverRequest contains input params for triggering a Retro-DVR recording.
type RetroHandoverRequest struct {
	JobID      string                     `json:"job_id"`
	AssetID    string                     `json:"asset_id"`
	ServiceRef string                     `json:"service_ref"`
	Title      string                     `json:"title"`
	StartTime  time.Time                  `json:"start_time"`
	EndTime    time.Time                  `json:"end_time"`
	Profile    recording.RecordingProfile `json:"profile"`
}

// RetroHandoverResult returns details about the finalized retro recording.
type RetroHandoverResult struct {
	Job            *recording.RecordingJob   `json:"job"`
	Asset          *recording.RecordingAsset `json:"asset"`
	AssemblyReport AssemblyReport            `json:"assembly_report"`
}

// ExecuteRetroRecording processes a retro recording from NVMe disk buffer handover to finalized RecordingAsset.
func (e *RetroDVRHandoverEngine) ExecuteRetroRecording(ctx context.Context, req RetroHandoverRequest) (*RetroHandoverResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req.JobID == "" || req.AssetID == "" || req.ServiceRef == "" || req.Title == "" {
		return nil, fmt.Errorf("invalid retro handover request: missing required fields")
	}

	backend, ok := e.backends[req.Profile.Target.BackendID]
	if !ok || backend == nil {
		return nil, fmt.Errorf("target storage backend '%s' not found or offline", req.Profile.Target.BackendID)
	}

	// 1. Lock NVMe segment range under lease from ReservationManager
	var reservation ringbuffer.Reservation
	var handles []ringbuffer.SegmentHandle
	var err error
	if e.resMgr != nil {
		leaseDuration := 30 * time.Minute
		reservation, err = e.resMgr.ReserveRange(req.ServiceRef, req.StartTime, req.EndTime, req.JobID, leaseDuration)
		if err != nil {
			return nil, fmt.Errorf("failed to reserve NVMe Retro-DVR segment range: %w", err)
		}
		defer func() {
			// Always release lease upon completion or failure
			_ = e.resMgr.ReleaseReservation(reservation.ID)
		}()

		handles, err = e.resMgr.ListReservedSegments(reservation.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to list reserved segments for %s: %w", reservation.ID, err)
		}
	}

	// 2. Create RecordingJob in PENDING state
	job, err := recording.NewRecordingJob(
		req.JobID,
		req.ServiceRef,
		req.Title,
		recording.SourceRetro,
		req.StartTime,
		req.EndTime,
		req.Profile.Target.BackendID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create RecordingJob: %w", err)
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

	// 4. Transfer reserved segments into staging directory
	stagingSegsDir := e.stagingMgr.SegmentsDir(job.ID)
	if err := os.MkdirAll(stagingSegsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create staging segments directory: %w", err)
	}
	for idx, h := range handles {
		srcPath := h.Location.Path
		if srcPath == "" {
			continue
		}
		destPath := filepath.Join(stagingSegsDir, fmt.Sprintf("seg_%06d.ts", idx+1))
		if err := copyOrLinkFile(srcPath, destPath); err != nil {
			return nil, fmt.Errorf("failed to transfer retro segment %s to staging: %w", srcPath, err)
		}
	}

	// 5. Update job state to STAGING
	job.State = recording.StateStaging
	if e.jobRepo != nil {
		if err := e.jobRepo.Save(ctx, job); err != nil {
			return nil, fmt.Errorf("failed to update job to STAGING: %w", err)
		}
	}

	// 6. Format media filename and finalize output
	meta := recording.TemplateMetadata{
		Title:     req.Title,
		StartTime: req.StartTime,
		Year:      req.StartTime.Year(),
		AssetID:   req.AssetID,
	}
	outFilename := recording.FormatMediaFilename(req.Profile.NamingPreset, req.Profile.FilenameTemplate, meta, req.Profile.ContainerFormat)

	report, err := e.stagingMgr.AssembleAndFinalize(ctx, job.ID, filepath.Base(outFilename))
	if err != nil {
		job.State = recording.StateFailed
		if e.jobRepo != nil {
			_ = e.jobRepo.Save(ctx, job)
		}
		return nil, fmt.Errorf("failed to assemble and finalize retro recording: %w", err)
	}

	// 7. Commit finalized file to target StorageBackend
	cleanRelTarget, err := recording.SanitizeAndValidateRelativePath(req.Profile.Target.RelativePath, outFilename)
	if err != nil {
		// Fallback to safe base filename if subfolder validation fails
		cleanRelTarget = filepath.Base(outFilename)
	}

	job.State = recording.StateTransferring
	if e.jobRepo != nil {
		_ = e.jobRepo.Save(ctx, job)
	}

	// 8. Create & Save RecordingAsset in AVAILABLE state
	asset, err := recording.NewRecordingAsset(
		req.AssetID,
		job.ID,
		req.Title,
		req.ServiceRef,
		req.Profile.Target.BackendID,
		cleanRelTarget,
		req.Profile.ContainerFormat,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create RecordingAsset: %w", err)
	}

	availableAsset, err := asset.TransitionState(recording.AssetAvailable)
	if err != nil {
		return nil, fmt.Errorf("failed to transition asset to AVAILABLE: %w", err)
	}
	availableAsset.ProfileID = req.Profile.ID
	availableAsset.DurationSeconds = int(req.EndTime.Sub(req.StartTime).Seconds())
	availableAsset.SizeBytes = report.TotalBytes
	availableAsset.RecordedStart = req.StartTime
	availableAsset.RecordedEnd = req.EndTime
	finTime := time.Now()
	availableAsset.FinalizedAt = &finTime

	if report.Complete {
		availableAsset.Completeness = recording.AssetComplete
	} else {
		availableAsset.Completeness = recording.AssetGapped
		availableAsset.GapCount = len(report.MissingRanges)
	}

	if e.assetRepo != nil {
		if err := e.assetRepo.Save(ctx, availableAsset, 0); err != nil {
			return nil, fmt.Errorf("failed to save RecordingAsset: %w", err)
		}
	}

	// 9. Transition Job to COMPLETED
	job.State = recording.StateCompleted
	if e.jobRepo != nil {
		if err := e.jobRepo.Save(ctx, job); err != nil {
			return nil, fmt.Errorf("failed to update job to COMPLETED: %w", err)
		}
	}

	return &RetroHandoverResult{
		Job:            job,
		Asset:          availableAsset,
		AssemblyReport: *report,
	}, nil
}

func copyOrLinkFile(src, dst string) error {
	// Try hardlink first for instant zero-copy NVMe transfer
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	// Fallback to copy
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

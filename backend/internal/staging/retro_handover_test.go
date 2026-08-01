// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package staging

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	appRecording "github.com/ManuGH/xg2g/internal/application/recording"
	"github.com/ManuGH/xg2g/internal/domain/recording"
	"github.com/ManuGH/xg2g/internal/hls/ringbuffer"
	"github.com/ManuGH/xg2g/internal/infra/storage"
)

type MockReservationManager struct {
	handles map[string][]ringbuffer.SegmentHandle
}

func (m *MockReservationManager) ProbeRange(serviceRef string, start, end time.Time) (ringbuffer.RangeProbe, error) {
	return ringbuffer.RangeProbe{Completeness: ringbuffer.CompletenessComplete}, nil
}

func (m *MockReservationManager) ReserveRange(serviceRef string, start, end time.Time, ownerID string, leaseDuration time.Duration) (ringbuffer.Reservation, error) {
	return ringbuffer.Reservation{
		ID:         "res_mock_123",
		OwnerID:    ownerID,
		ServiceRef: serviceRef,
		Start:      start,
		End:        end,
		Status:     ringbuffer.CompletenessComplete,
		ExpiresAt:  time.Now().Add(leaseDuration),
	}, nil
}

func (m *MockReservationManager) GetReservation(reservationID string) (ringbuffer.Reservation, error) {
	return ringbuffer.Reservation{ID: reservationID}, nil
}

func (m *MockReservationManager) ListReservedSegments(reservationID string) ([]ringbuffer.SegmentHandle, error) {
	return m.handles[reservationID], nil
}

func (m *MockReservationManager) RenewReservation(reservationID string, leaseDuration time.Duration) error {
	return nil
}

func (m *MockReservationManager) ReleaseReservation(reservationID string) error {
	return nil
}

func TestRetroDVRHandoverEngine_EndToEndFlow(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "retro_handover_test")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	serviceRef := "1:0:19:283D:3FB:1:C00000:0:0:0:"
	now := time.Now()

	// Prepare 2 physical TS segments
	nvmeRoot := filepath.Join(tmpDir, "nvme_store")
	if err := os.MkdirAll(nvmeRoot, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	seg1Path := filepath.Join(nvmeRoot, "seg_000001.ts")
	seg2Path := filepath.Join(nvmeRoot, "seg_000002.ts")
	payload1 := []byte("RETRO_HEADER_FRAME_DATA_")
	payload2 := []byte("RETRO_BODY_FRAME_DATA")

	if err := os.WriteFile(seg1Path, payload1, 0644); err != nil {
		t.Fatalf("WriteFile seg1 failed: %v", err)
	}
	if err := os.WriteFile(seg2Path, payload2, 0644); err != nil {
		t.Fatalf("WriteFile seg2 failed: %v", err)
	}

	mockResMgr := &MockReservationManager{
		handles: map[string][]ringbuffer.SegmentHandle{
			"res_mock_123": {
				{
					ID:          ringbuffer.SegmentID{SessionID: "sess_1", Sequence: 1},
					Location:    ringbuffer.SegmentLocation{Kind: ringbuffer.StorageKindDisk, Path: seg1Path},
					Sequence:    1,
					SizeBytes:   int64(len(payload1)),
					DurationSec: 900.0,
					StartWallTime: now.Add(-30 * time.Minute),
					EndWallTime:   now.Add(-15 * time.Minute),
				},
				{
					ID:          ringbuffer.SegmentID{SessionID: "sess_1", Sequence: 2},
					Location:    ringbuffer.SegmentLocation{Kind: ringbuffer.StorageKindDisk, Path: seg2Path},
					Sequence:    2,
					SizeBytes:   int64(len(payload2)),
					DurationSec: 900.0,
					StartWallTime: now.Add(-15 * time.Minute),
					EndWallTime:   now,
				},
			},
		},
	}

	// Setup Repositories and Handover Engine
	jobRepoPath := filepath.Join(tmpDir, "staging")
	jobRepo, err := recording.NewDiskJobRepository(jobRepoPath)
	if err != nil {
		t.Fatalf("NewDiskJobRepository failed: %v", err)
	}

	assetRepoPath := filepath.Join(tmpDir, "library", "assets.json")
	assetRepo, err := recording.NewDiskAssetRepository(assetRepoPath)
	if err != nil {
		t.Fatalf("NewDiskAssetRepository failed: %v", err)
	}

	profileRepoPath := filepath.Join(tmpDir, "library", "profiles.json")
	profileRepo, err := recording.NewDiskProfileRepository(profileRepoPath)
	if err != nil {
		t.Fatalf("NewDiskProfileRepository failed: %v", err)
	}

	taskRepoPath := filepath.Join(tmpDir, "library", "transfers.json")
	taskRepo, err := recording.NewDiskTransferTaskRepository(taskRepoPath)
	if err != nil {
		t.Fatalf("NewDiskTransferTaskRepository failed: %v", err)
	}

	backend, err := NewLocalNVMeStorageBackend("local-nvme-1", nvmeRoot)
	if err != nil {
		t.Fatalf("NewLocalNVMeStorageBackend failed: %v", err)
	}

	sm, err := NewStagingManager(jobRepoPath, jobRepo)
	if err != nil {
		t.Fatalf("NewStagingManager failed: %v", err)
	}

	profile, err := recording.NewRecordingProfile("prof_retro", "Retro Profile", backend.ID(), "Recordings", recording.ContainerTS, recording.NamingPresetMovies)
	if err != nil {
		t.Fatalf("NewRecordingProfile failed: %v", err)
	}
	if err := profileRepo.Save(ctx, profile); err != nil {
		t.Fatalf("profileRepo.Save failed: %v", err)
	}

	engine, err := NewRetroDVRHandoverEngine(mockResMgr, jobRepo, assetRepo, profileRepo, taskRepo, sm, []storage.StorageBackend{backend})
	if err != nil {
		t.Fatalf("NewRetroDVRHandoverEngine failed: %v", err)
	}

	// 1. Execute Retro Recording Handover (Happy Path)
	req := RetroHandoverRequest{
		JobID:      "job_retro_999",
		AssetID:    "asset_retro_999",
		ProfileID:  profile.ID,
		ServiceRef: serviceRef,
		Title:      "Tagesschau Retro",
		StartTime:  now.Add(-30 * time.Minute),
		EndTime:    now,
	}

	res, err := engine.ExecuteRetroRecording(ctx, req)
	if err != nil {
		t.Fatalf("ExecuteRetroRecording failed: %v", err)
	}

	if res.Job.State != recording.StateCompleted {
		t.Errorf("Expected job state COMPLETED, got %s", res.Job.State)
	}
	if res.Asset.State != recording.AssetAvailable {
		t.Errorf("Expected asset state AVAILABLE, got %s", res.Asset.State)
	}
	if res.Asset.SizeBytes != int64(len(payload1)+len(payload2)) {
		t.Errorf("Expected asset size %d, got %d", len(payload1)+len(payload2), res.Asset.SizeBytes)
	}

	// Verify asset in AssetRepository
	recoveredAsset, err := assetRepo.Get(ctx, req.AssetID)
	if err != nil {
		t.Fatalf("assetRepo.Get failed: %v", err)
	}
	if recoveredAsset.Title != "Tagesschau Retro" {
		t.Errorf("Expected title 'Tagesschau Retro', got '%s'", recoveredAsset.Title)
	}

	// 2. Test AssetDeletionService (Policy Snapshot & ErrObjectNotFound verification)
	delService := appRecording.NewAssetDeletionService(assetRepo, []storage.StorageBackend{backend})
	if err := delService.DeleteAsset(ctx, req.AssetID, false); err != nil {
		t.Fatalf("delService.DeleteAsset failed: %v", err)
	}

	_, err = assetRepo.Get(ctx, req.AssetID)
	if err != recording.ErrAssetNotFound {
		t.Errorf("Expected ErrAssetNotFound after deletion, got %v", err)
	}
}

type FailingStorageBackend struct {
	id string
}

func (f *FailingStorageBackend) ID() string { return f.id }
func (f *FailingStorageBackend) Type() storage.StorageType { return storage.StorageTypeLocal }
func (f *FailingStorageBackend) Roles() []storage.StorageRole { return []storage.StorageRole{storage.RoleRecordingTarget} }
func (f *FailingStorageBackend) Capabilities() storage.StorageCapabilities { return storage.StorageCapabilities{} }
func (f *FailingStorageBackend) Health(ctx context.Context) storage.HealthStatus { return storage.HealthStatus{} }
func (f *FailingStorageBackend) Capacity(ctx context.Context) (storage.CapacityInfo, error) { return storage.CapacityInfo{}, nil }
func (f *FailingStorageBackend) Open(ctx context.Context, objectKey string) (storage.ObjectReader, error) { return nil, storage.ErrObjectNotFound }
func (f *FailingStorageBackend) OpenRange(ctx context.Context, objectKey string, offset, length int64) (io.ReadCloser, error) { return nil, storage.ErrObjectNotFound }
func (f *FailingStorageBackend) CommitFile(ctx context.Context, srcLocalPath string, targetObjectKey string) error { return os.ErrPermission }
func (f *FailingStorageBackend) Stat(ctx context.Context, objectKey string) (storage.ObjectInfo, error) { return storage.ObjectInfo{}, storage.ErrObjectNotFound }
func (f *FailingStorageBackend) DeleteFile(ctx context.Context, targetObjectKey string) error { return storage.ErrObjectNotFound }

func TestRetroDVRHandoverEngine_TargetFailureFallbackAndWorkerRetry(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "retro_failure_test")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	serviceRef := "1:0:19:283D:3FB:1:C00000:0:0:0:"
	now := time.Now()

	nvmeRoot := filepath.Join(tmpDir, "nvme_store")
	seg1Path := filepath.Join(nvmeRoot, "seg_000001.ts")
	if err := os.MkdirAll(nvmeRoot, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	payload := []byte("SEGMENT_DATA_PAYLOAD")
	if err := os.WriteFile(seg1Path, payload, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	mockResMgr := &MockReservationManager{
		handles: map[string][]ringbuffer.SegmentHandle{
			"res_mock_123": {
				{
					ID:          ringbuffer.SegmentID{SessionID: "sess_1", Sequence: 1},
					Location:    ringbuffer.SegmentLocation{Kind: ringbuffer.StorageKindDisk, Path: seg1Path},
					Sequence:    1,
					SizeBytes:   int64(len(payload)),
					DurationSec: 900.0,
					StartWallTime: now.Add(-30 * time.Minute),
					EndWallTime:   now.Add(-15 * time.Minute),
				},
			},
		},
	}

	jobRepoPath := filepath.Join(tmpDir, "staging")
	jobRepo, _ := recording.NewDiskJobRepository(jobRepoPath)
	assetRepoPath := filepath.Join(tmpDir, "library", "assets.json")
	assetRepo, _ := recording.NewDiskAssetRepository(assetRepoPath)
	profileRepoPath := filepath.Join(tmpDir, "library", "profiles.json")
	profileRepo, _ := recording.NewDiskProfileRepository(profileRepoPath)
	taskRepoPath := filepath.Join(tmpDir, "library", "transfers.json")
	taskRepo, _ := recording.NewDiskTransferTaskRepository(taskRepoPath)

	failingBackend := &FailingStorageBackend{id: "failing-backend-1"}
	healthyBackend, _ := NewLocalNVMeStorageBackend("healthy-backend-1", filepath.Join(tmpDir, "healthy_storage"))

	sm, _ := NewStagingManager(jobRepoPath, jobRepo)
	profile, _ := recording.NewRecordingProfile("prof_fail", "Fail Profile", failingBackend.ID(), "Recordings", recording.ContainerTS, recording.NamingPresetMovies)
	_ = profileRepo.Save(ctx, profile)

	engine, _ := NewRetroDVRHandoverEngine(mockResMgr, jobRepo, assetRepo, profileRepo, taskRepo, sm, []storage.StorageBackend{failingBackend, healthyBackend})

	req := RetroHandoverRequest{
		JobID:      "job_fail_101",
		AssetID:    "asset_fail_101",
		ProfileID:  profile.ID,
		ServiceRef: serviceRef,
		Title:      "Failed Transfer Show",
		StartTime:  now.Add(-30 * time.Minute),
		EndTime:    now,
	}

	res, err := engine.ExecuteRetroRecording(ctx, req)
	if err != nil {
		t.Fatalf("ExecuteRetroRecording failed: %v", err)
	}

	if res.Job.State != recording.StateWaitingTarget {
		t.Errorf("Expected job state WAITING_FOR_TARGET, got %s", res.Job.State)
	}
	if res.Asset.State != recording.AssetTransferPending {
		t.Errorf("Expected asset state TRANSFER_PENDING, got %s", res.Asset.State)
	}
	if !res.TransferScheduled {
		t.Errorf("Expected TransferScheduled to be true!")
	}

	// Verify TransferTask in repository
	tasks, err := taskRepo.List(ctx)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("Expected 1 TransferTask, got %d (err: %v)", len(tasks), err)
	}
	if tasks[0].State != recording.TransferPending {
		t.Errorf("Expected TransferTask PENDING state, got %s", tasks[0].State)
	}

	// Fix target backend to healthy and run TransferWorker retry loop
	worker, err := appRecording.NewTransferWorker("worker-1", jobRepoPath, taskRepo, jobRepo, assetRepo, []storage.StorageBackend{healthyBackend})
	if err != nil {
		t.Fatalf("NewTransferWorker failed: %v", err)
	}

	// Update task target backend to healthy-backend-1
	taskToUpdate := tasks[0]
	taskToUpdate.TargetBackendID = healthyBackend.ID()
	if err := taskRepo.Save(ctx, taskToUpdate); err != nil {
		t.Fatalf("taskRepo.Save failed: %v", err)
	}

	processed, err := worker.ProcessNextTask(ctx)
	if err != nil || !processed {
		t.Fatalf("ProcessNextTask failed: processed=%v, err=%v", processed, err)
	}

	// Verify recovered Asset AVAILABLE and Job COMPLETED
	recAsset, err := assetRepo.Get(ctx, req.AssetID)
	if err != nil {
		t.Fatalf("assetRepo.Get failed: %v", err)
	}
	if recAsset.State != recording.AssetAvailable {
		t.Errorf("Expected recovered asset AVAILABLE, got state: %s", recAsset.State)
	}

	recJob, err := jobRepo.Get(ctx, req.JobID)
	if err != nil {
		t.Fatalf("jobRepo.Get failed: %v", err)
	}
	if recJob.State != recording.StateCompleted {
		t.Errorf("Expected recovered job COMPLETED, got state: %s", recJob.State)
	}
}

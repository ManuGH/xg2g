// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package staging

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
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
					ID:            ringbuffer.SegmentID{SessionID: "sess_1", Sequence: 1},
					Location:      ringbuffer.SegmentLocation{Kind: ringbuffer.StorageKindDisk, Path: seg1Path},
					Sequence:      1,
					SizeBytes:     int64(len(payload1)),
					DurationSec:   900.0,
					StartWallTime: now.Add(-30 * time.Minute),
					EndWallTime:   now.Add(-15 * time.Minute),
				},
				{
					ID:            ringbuffer.SegmentID{SessionID: "sess_1", Sequence: 2},
					Location:      ringbuffer.SegmentLocation{Kind: ringbuffer.StorageKindDisk, Path: seg2Path},
					Sequence:      2,
					SizeBytes:     int64(len(payload2)),
					DurationSec:   900.0,
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

func (f *FailingStorageBackend) ID() string                { return f.id }
func (f *FailingStorageBackend) Type() storage.StorageType { return storage.StorageTypeLocal }
func (f *FailingStorageBackend) Roles() []storage.StorageRole {
	return []storage.StorageRole{storage.RoleRecordingTarget}
}
func (f *FailingStorageBackend) Capabilities() storage.StorageCapabilities {
	return storage.StorageCapabilities{SupportsAtomicReplace: true}
}
func (f *FailingStorageBackend) Health(ctx context.Context) storage.HealthStatus {
	return storage.HealthStatus{}
}
func (f *FailingStorageBackend) Capacity(ctx context.Context) (storage.CapacityInfo, error) {
	return storage.CapacityInfo{}, nil
}
func (f *FailingStorageBackend) Open(ctx context.Context, objectKey string) (storage.ObjectReader, error) {
	return nil, storage.ErrObjectNotFound
}
func (f *FailingStorageBackend) OpenRange(ctx context.Context, objectKey string, offset, length int64) (io.ReadCloser, error) {
	return nil, storage.ErrObjectNotFound
}
func (f *FailingStorageBackend) CommitFile(ctx context.Context, srcLocalPath string, targetObjectKey string) error {
	return os.ErrPermission
}
func (f *FailingStorageBackend) Stat(ctx context.Context, objectKey string) (storage.ObjectInfo, error) {
	return storage.ObjectInfo{}, storage.ErrObjectNotFound
}
func (f *FailingStorageBackend) DeleteFile(ctx context.Context, targetObjectKey string) error {
	return storage.ErrObjectNotFound
}

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
					ID:            ringbuffer.SegmentID{SessionID: "sess_1", Sequence: 1},
					Location:      ringbuffer.SegmentLocation{Kind: ringbuffer.StorageKindDisk, Path: seg1Path},
					Sequence:      1,
					SizeBytes:     int64(len(payload)),
					DurationSec:   900.0,
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

	// Update task target backend to healthy-backend-1 directly in repo
	tasks[0].TargetBackendID = healthyBackend.ID()
	if err := taskRepo.CreateTask(ctx, tasks[0]); err != nil && !errors.Is(err, recording.ErrTransferTaskAlreadyExists) {
		t.Fatalf("taskRepo.CreateTask failed: %v", err)
	}
	taskToSave := tasks[0]
	taskToSave.TargetBackendID = healthyBackend.ID()
	_ = taskRepo.Delete(ctx, taskToSave.ID)
	if err := taskRepo.CreateTask(ctx, taskToSave); err != nil {
		t.Fatalf("CreateTask healthy backend failed: %v", err)
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

func TestStartupReconciler_5CaseRecoveryMatrix(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "reconciler_test")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	jobRepoPath := filepath.Join(tmpDir, "staging")
	jobRepo, _ := recording.NewDiskJobRepository(jobRepoPath)
	assetRepoPath := filepath.Join(tmpDir, "library", "assets.json")
	assetRepo, _ := recording.NewDiskAssetRepository(assetRepoPath)
	taskRepoPath := filepath.Join(tmpDir, "library", "transfers.json")
	taskRepo, _ := recording.NewDiskTransferTaskRepository(taskRepoPath)

	targetRoot := filepath.Join(tmpDir, "target_storage")
	backend, _ := NewLocalNVMeStorageBackend("target-1", targetRoot)

	// Case 1: Target file exists + Asset TRANSFER_PENDING -> AVAILABLE & COMPLETED
	targetFilePath := filepath.Join(targetRoot, "movies", "show1.ts")
	_ = os.MkdirAll(filepath.Dir(targetFilePath), 0755)
	payload := []byte("SHOW1_PAYLOAD")
	_ = os.WriteFile(targetFilePath, payload, 0644)

	now := time.Now()
	job1, _ := recording.NewRecordingJob("job_rec_1", "ref_1", "Show 1", recording.SourceRetro, now.Add(-1*time.Hour), now, backend.ID())
	job1Pending, _ := job1.TransitionState(recording.StateWaitingTarget, "")
	_ = jobRepo.Save(ctx, job1Pending, 0)

	asset1, _ := recording.NewRecordingAsset("asset_rec_1", job1.ID, "Show 1", "ref_1", backend.ID(), "movies/show1.ts", recording.ContainerTS)
	asset1.SizeBytes = int64(len(payload))
	asset1Pending, _ := asset1.TransitionState(recording.AssetTransferPending)
	_ = assetRepo.Save(ctx, asset1Pending, 0)

	reconciler, err := NewStartupReconciler(jobRepo, assetRepo, taskRepo, jobRepoPath, []storage.StorageBackend{backend})
	if err != nil {
		t.Fatalf("NewStartupReconciler failed: %v", err)
	}

	if err := reconciler.ReconcileAll(ctx); err != nil {
		t.Fatalf("ReconcileAll failed: %v", err)
	}

	recJob1, _ := jobRepo.Get(ctx, job1.ID)
	if recJob1.State != recording.StateCompleted {
		t.Errorf("Case 1: Expected job state COMPLETED, got %s", recJob1.State)
	}
	recAsset1, _ := assetRepo.Get(ctx, asset1.ID)
	if recAsset1.State != recording.AssetAvailable {
		t.Errorf("Case 1: Expected asset state AVAILABLE, got %s", recAsset1.State)
	}
}

func TestTransferWorker_LeaseOwnershipCASRejection(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cas_lease_test")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	taskRepoPath := filepath.Join(tmpDir, "transfers.json")
	taskRepo, _ := recording.NewDiskTransferTaskRepository(taskRepoPath)

	task, _ := recording.NewTransferTask("task_cas_1", "job_1", "asset_1", "job_1", "show.ts", "backend_1", "movies/show.ts", 100)
	_ = taskRepo.CreateTask(ctx, task)

	// Worker A claims task
	claimedA, err := taskRepo.ClaimTask(ctx, "worker-A", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("ClaimTask worker-A failed: %v", err)
	}

	// Wait for lease to expire
	time.Sleep(150 * time.Millisecond)

	// Worker B claims task
	claimedB, err := taskRepo.ClaimTask(ctx, "worker-B", 10*time.Minute)
	if err != nil {
		t.Fatalf("ClaimTask worker-B failed: %v", err)
	}
	_ = claimedB

	// Worker A wakes up and tries to save task using its expired lease token
	claimedA.State = recording.TransferFailed
	err = taskRepo.SaveTaskLeased(ctx, claimedA, "worker-A", claimedA.LeaseToken)
	if !errors.Is(err, recording.ErrWorkerLeaseLost) {
		t.Fatalf("Expected ErrWorkerLeaseLost when worker-A attempts stale save, got %v", err)
	}

	// Verify Worker B's active lock remains intact
	freshTask, _ := taskRepo.Get(ctx, "task_cas_1")
	if freshTask.LockedBy != "worker-B" {
		t.Fatalf("Worker B's lease was corrupted by Worker A! Active lock: %s", freshTask.LockedBy)
	}
}

func TestListAllInventory_CorruptedManifestResilience(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "inventory_test")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	jobRepo, err := recording.NewDiskJobRepository(tmpDir)
	if err != nil {
		t.Fatalf("NewDiskJobRepository failed: %v", err)
	}

	// 1. Create a valid job
	job1, _ := recording.NewRecordingJob("job_valid_1", "ref_1", "Valid Show", recording.SourceRetro, time.Now(), time.Now().Add(1*time.Hour), "backend-1")
	if err := jobRepo.Save(ctx, job1, 0); err != nil {
		t.Fatalf("jobRepo.Save failed: %v", err)
	}

	// 2. Create a corrupted manifest file
	corruptDir := filepath.Join(tmpDir, "jobs", "job_corrupt_2")
	_ = os.MkdirAll(corruptDir, 0755)
	_ = os.WriteFile(filepath.Join(corruptDir, "manifest.json"), []byte("{invalid json payload..."), 0644)

	// 3. Scan inventory
	inventory, err := jobRepo.ListAllInventory(ctx)
	if err != nil {
		t.Fatalf("ListAllInventory failed: %v", err)
	}

	if len(inventory.Jobs) != 1 {
		t.Errorf("Expected 1 valid job, got %d", len(inventory.Jobs))
	}
	if len(inventory.Issues) != 1 {
		t.Errorf("Expected 1 issue reported, got %d", len(inventory.Issues))
	}
}

func TestCrashMatrixMultiSaveRecovery(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "crash_matrix_test")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	jobRepoPath := filepath.Join(tmpDir, "staging")
	jobRepo, _ := recording.NewDiskJobRepository(jobRepoPath)
	assetRepoPath := filepath.Join(tmpDir, "library", "assets.json")
	assetRepo, _ := recording.NewDiskAssetRepository(assetRepoPath)
	taskRepoPath := filepath.Join(tmpDir, "library", "transfers.json")
	taskRepo, _ := recording.NewDiskTransferTaskRepository(taskRepoPath)

	targetRoot := filepath.Join(tmpDir, "target_storage")
	backend, _ := NewLocalNVMeStorageBackend("target-1", targetRoot)

	targetFilePath := filepath.Join(targetRoot, "movies", "crash_show.ts")
	_ = os.MkdirAll(filepath.Dir(targetFilePath), 0755)
	payload := []byte("CRASH_TEST_PAYLOAD")
	_ = os.WriteFile(targetFilePath, payload, 0644)

	now := time.Now()
	// Scenario: Crash 1 - Asset save succeeded (AVAILABLE), Job save crashed (TRANSFERRING), Task crashed (PENDING)
	job, _ := recording.NewRecordingJob("job_crash_1", "ref_crash", "Crash Show", recording.SourceRetro, now.Add(-1*time.Hour), now, backend.ID())
	jobWaiting, _ := job.TransitionState(recording.StateWaitingTarget, "")
	jobTransferring, _ := jobWaiting.TransitionState(recording.StateTransferring, "")
	if err := jobRepo.Save(ctx, jobTransferring, 0); err != nil {
		t.Fatalf("jobRepo.Save failed: %v", err)
	}

	asset, _ := recording.NewRecordingAsset("asset_crash_1", job.ID, "Crash Show", "ref_crash", backend.ID(), "movies/crash_show.ts", recording.ContainerTS)
	asset.SizeBytes = int64(len(payload))
	availAsset, _ := asset.TransitionState(recording.AssetAvailable)
	_ = assetRepo.Save(ctx, availAsset, 0)

	task, _ := recording.NewTransferTask("task_crash_1", job.ID, asset.ID, job.ID, "crash_show.ts", backend.ID(), "movies/crash_show.ts", int64(len(payload)))
	_ = taskRepo.CreateTask(ctx, task)

	reconciler, err := NewStartupReconciler(jobRepo, assetRepo, taskRepo, jobRepoPath, []storage.StorageBackend{backend})
	if err != nil {
		t.Fatalf("NewStartupReconciler failed: %v", err)
	}

	if err := reconciler.ReconcileAll(ctx); err != nil {
		t.Fatalf("ReconcileAll failed: %v", err)
	}

	recJob, _ := jobRepo.Get(ctx, job.ID)
	if recJob.State != recording.StateCompleted {
		t.Errorf("Crash Matrix 1: Expected job state COMPLETED after recovery, got %s", recJob.State)
	}
	recTask, _ := taskRepo.Get(ctx, task.ID)
	if recTask.State != recording.TransferCompleted {
		t.Errorf("Crash Matrix 1: Expected task state COMPLETED after recovery, got %s", recTask.State)
	}
}

func TestManifestFileIsolation_NoOverwrite(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "manifest_isolation_test")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	jobRepo, err := recording.NewDiskJobRepository(tmpDir)
	if err != nil {
		t.Fatalf("NewDiskJobRepository failed: %v", err)
	}

	job, _ := recording.NewRecordingJob("job_iso_100", "ref_100", "Isolated Show", recording.SourceRetro, time.Now(), time.Now().Add(1*time.Hour), "backend-1")
	if err := jobRepo.Save(ctx, job, 0); err != nil {
		t.Fatalf("jobRepo.Save failed: %v", err)
	}

	// Write StagingManifest to staging/staging_manifest.json
	stagingDir := filepath.Join(tmpDir, "jobs", job.ID, "staging")
	_ = os.MkdirAll(stagingDir, 0755)
	_ = os.WriteFile(filepath.Join(stagingDir, "staging_manifest.json"), []byte(`{"job_id":"job_iso_100","type":"staging"}`), 0644)

	// Write FinalizationManifest to finalized/finalization_manifest.json
	finalizedDir := filepath.Join(tmpDir, "jobs", job.ID, "finalized")
	_ = os.MkdirAll(finalizedDir, 0755)
	_ = os.WriteFile(filepath.Join(finalizedDir, "finalization_manifest.json"), []byte(`{"job_id":"job_iso_100","type":"finalization"}`), 0644)

	// Verify JobRepository.Get(job.ID) still loads RecordingJob cleanly without corruption
	loadedJob, err := jobRepo.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("JobRepository.Get failed after staging & finalization manifest saves: %v", err)
	}

	if loadedJob.Title != "Isolated Show" {
		t.Errorf("Expected title 'Isolated Show', got '%s'", loadedJob.Title)
	}
	if loadedJob.State != recording.StatePreparing {
		t.Errorf("Expected state PREPARING, got %s", loadedJob.State)
	}
}

func TestLegacyJobManifestSchema_MigrationAndTransition(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "legacy_schema_test")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	jobRepo, err := recording.NewDiskJobRepository(tmpDir)
	if err != nil {
		t.Fatalf("NewDiskJobRepository failed: %v", err)
	}

	// Write legacy JSON manifest containing legacy fields: source_type, PENDING, local_fallback_id
	legacyDir := filepath.Join(tmpDir, "jobs", "job_legacy_1")
	_ = os.MkdirAll(legacyDir, 0755)
	legacyJSON := `{
		"id": "job_legacy_1",
		"service_ref": "ref_legacy",
		"title": "Legacy Show",
		"source_type": "MANUAL",
		"state": "PENDING",
		"local_fallback_id": "local-nvme-legacy",
		"start_time": "2026-08-01T12:00:00Z",
		"end_time": "2026-08-01T13:00:00Z"
	}`
	_ = os.WriteFile(filepath.Join(legacyDir, "manifest.json"), []byte(legacyJSON), 0644)

	// Read via JobRepository.Get (triggers legacy fallback & custom unmarshaler)
	job, err := jobRepo.Get(ctx, "job_legacy_1")
	if err != nil {
		t.Fatalf("jobRepo.Get failed to unmarshal legacy manifest: %v", err)
	}

	if job.Source != recording.SourceManual {
		t.Errorf("Expected Source MANUAL, got '%s'", job.Source)
	}
	if job.LocalFallbackID != "local-nvme-legacy" {
		t.Errorf("Expected LocalFallbackID 'local-nvme-legacy', got '%s'", job.LocalFallbackID)
	}
	if job.State != recording.StatePending {
		t.Errorf("Expected State PENDING, got '%s'", job.State)
	}

	// Verify transition from PENDING is legal and works cleanly
	preparingJob, err := job.TransitionState(recording.StatePreparing, "")
	if err != nil {
		t.Fatalf("Failed to transition legacy job from PENDING to PREPARING: %v", err)
	}

	if err := jobRepo.Save(ctx, preparingJob, job.Version); err != nil {
		t.Fatalf("Failed to save migrated job: %v", err)
	}
}

func TestAtomicReplaceWithExistingTargetFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "atomic_replace_test")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	targetRoot := filepath.Join(tmpDir, "target")
	backend, err := NewLocalNVMeStorageBackend("b1", targetRoot)
	if err != nil {
		t.Fatalf("NewLocalNVMeStorageBackend failed: %v", err)
	}

	// Create pre-existing truncated target file
	targetKey := "movies/target_show.ts"
	targetPath := filepath.Join(targetRoot, targetKey)
	_ = os.MkdirAll(filepath.Dir(targetPath), 0755)
	_ = os.WriteFile(targetPath, []byte("TRUNCATED"), 0644)

	// Prepare new complete source file
	srcPath := filepath.Join(tmpDir, "complete_source.ts")
	newPayload := []byte("COMPLETE_REPLACEMENT_PAYLOAD_DATA_12345")
	_ = os.WriteFile(srcPath, newPayload, 0644)

	// Execute CommitFile
	if err := backend.CommitFile(ctx, srcPath, targetKey); err != nil {
		t.Fatalf("CommitFile atomic replace failed: %v", err)
	}

	// Stat verification
	info, err := backend.Stat(ctx, targetKey)
	if err != nil {
		t.Fatalf("backend.Stat failed: %v", err)
	}

	if info.SizeBytes != int64(len(newPayload)) {
		t.Errorf("Expected atomic replace target size %d, got %d", len(newPayload), info.SizeBytes)
	}
}

type memoryJobRepo struct {
	mu   sync.Mutex
	jobs map[string]*recording.RecordingJob
}

func newMemoryJobRepo() *memoryJobRepo {
	return &memoryJobRepo{jobs: make(map[string]*recording.RecordingJob)}
}

func (m *memoryJobRepo) Save(ctx context.Context, job *recording.RecordingJob, expectedVersion uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.jobs[job.ID]; ok && expectedVersion > 0 && existing.Version != expectedVersion {
		return recording.ErrOptimisticLockConflict
	}
	saveJob := *job
	saveJob.Version++
	m.jobs[job.ID] = &saveJob
	job.Version = saveJob.Version
	return nil
}

func (m *memoryJobRepo) Get(ctx context.Context, id string) (*recording.RecordingJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if job, ok := m.jobs[id]; ok {
		cp := *job
		return &cp, nil
	}
	return nil, recording.ErrJobNotFound
}

func (m *memoryJobRepo) ListAllInventory(ctx context.Context) (recording.JobInventory, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var inv recording.JobInventory
	for _, j := range m.jobs {
		cp := *j
		inv.Jobs = append(inv.Jobs, &cp)
	}
	return inv, nil
}

func (m *memoryJobRepo) ListRecoverable(ctx context.Context) ([]*recording.RecordingJob, error) {
	inv, _ := m.ListAllInventory(ctx)
	var rec []*recording.RecordingJob
	for _, j := range inv.Jobs {
		if !j.State.IsTerminal() {
			rec = append(rec, j)
		}
	}
	return rec, nil
}

func (m *memoryJobRepo) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.jobs, id)
	return nil
}

func TestStagingManager_MockRepository_NoPanic(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mock_repo_test")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	repo := newMemoryJobRepo()
	sm, err := NewStagingManager(tmpDir, repo)
	if err != nil {
		t.Fatalf("NewStagingManager failed: %v", err)
	}

	job, _ := recording.NewRecordingJob("job_mock_1", "ref_m1", "Mock Show", recording.SourceLive, time.Now(), time.Now().Add(1*time.Hour), "nas-1")
	if err := repo.Save(ctx, job, 0); err != nil {
		t.Fatalf("repo.Save failed: %v", err)
	}

	// PrepareWorkspace must succeed with non-disk JobRepository without panic!
	jobDir, err := sm.PrepareWorkspace(ctx, job)
	if err != nil {
		t.Fatalf("PrepareWorkspace failed with memory repo: %v", err)
	}
	if jobDir == "" {
		t.Errorf("Expected non-empty jobDir")
	}
}

func TestJobRepository_OptimisticLocking(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cas_lock_test")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	repo, _ := recording.NewDiskJobRepository(tmpDir)

	job, _ := recording.NewRecordingJob("job_cas_1", "ref_cas", "CAS Show", recording.SourceRetro, time.Now(), time.Now().Add(1*time.Hour), "b-1")
	if err := repo.Save(ctx, job, 0); err != nil {
		t.Fatalf("Initial Save failed: %v", err)
	}
	initialVersion := job.Version

	// Worker 1 fetches job (Version 1)
	snapshot1, _ := repo.Get(ctx, job.ID)
	// Worker 2 fetches job (Version 1)
	snapshot2, _ := repo.Get(ctx, job.ID)

	// Worker 1 mutates & saves state (Version becomes 2)
	st1, _ := snapshot1.TransitionState(recording.StateStaging, "")
	if err := repo.Save(ctx, st1, snapshot1.Version); err != nil {
		t.Fatalf("Worker 1 Save failed: %v", err)
	}

	// Worker 2 attempts save with stale expectedVersion 1 -> MUST return ErrOptimisticLockConflict!
	st2, _ := snapshot2.TransitionState(recording.StateWaitingTarget, "")
	err = repo.Save(ctx, st2, initialVersion)
	if err == nil || !errors.Is(err, recording.ErrOptimisticLockConflict) {
		t.Errorf("Expected ErrOptimisticLockConflict for stale save attempt, got: %v", err)
	}
}

func TestJobStateTransition_InterruptedRecovery(t *testing.T) {
	job, _ := recording.NewRecordingJob("job_int_1", "ref_int", "Interrupted Show", recording.SourceLive, time.Now(), time.Now().Add(1*time.Hour), "b-1")
	intJob, err := job.TransitionState(recording.StateInterrupted, "power failure")
	if err != nil {
		t.Fatalf("TransitionState StateInterrupted failed: %v", err)
	}

	if intJob.State.IsTerminal() {
		t.Errorf("StateInterrupted MUST NOT be terminal")
	}

	// Verify recovery transition to StateStaging works
	stJob, err := intJob.TransitionState(recording.StateStaging, "")
	if err != nil {
		t.Fatalf("Transition from INTERRUPTED to STAGING failed: %v", err)
	}
	if stJob.State != recording.StateStaging {
		t.Errorf("Expected state STAGING, got %s", stJob.State)
	}
}

func TestLegacyManifest_FallbackIsolation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "fallback_iso_test")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	jobRepo, _ := recording.NewDiskJobRepository(tmpDir)

	legacyDir := filepath.Join(tmpDir, "jobs", "job_fb_1")
	_ = os.MkdirAll(legacyDir, 0755)
	legacyJSON := `{
		"id": "job_fb_1",
		"service_ref": "ref_fb",
		"title": "Fallback Show",
		"target_backend_id": "nas-primary",
		"local_fallback_id": "local-nvme-backup",
		"state": "STAGING"
	}`
	_ = os.WriteFile(filepath.Join(legacyDir, "manifest.json"), []byte(legacyJSON), 0644)

	job, err := jobRepo.Get(ctx, "job_fb_1")
	if err != nil {
		t.Fatalf("jobRepo.Get failed: %v", err)
	}

	if job.TargetBackendID != "nas-primary" {
		t.Errorf("Expected TargetBackendID 'nas-primary', got '%s'", job.TargetBackendID)
	}
	if job.LocalFallbackID != "local-nvme-backup" {
		t.Errorf("Expected LocalFallbackID 'local-nvme-backup', got '%s'", job.LocalFallbackID)
	}
}

func TestCrashAndWorkerRecovery_CASVersionValidation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "crash_cas_val_test")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	jobRepo, _ := recording.NewDiskJobRepository(tmpDir)

	// Step 1: Initial Job creation
	job, _ := recording.NewRecordingJob("job_crash_cas", "ref_cas", "CAS Crash Test", recording.SourceRetro, time.Now(), time.Now().Add(1*time.Hour), "b1")
	if err := jobRepo.Save(ctx, job, 0); err != nil {
		t.Fatalf("Initial save failed: %v", err)
	}

	// Advance version through legal state changes to simulate prior pipeline steps
	states := []recording.RecordingState{
		recording.StateRecording,
		recording.StateStaging,
		recording.StateFinalizing,
		recording.StateWaitingTarget,
		recording.StateTransferring,
	}
	for _, targetState := range states {
		cur, _ := jobRepo.Get(ctx, job.ID)
		st, err := cur.TransitionState(targetState, "")
		if err != nil {
			t.Fatalf("TransitionState to %s failed: %v", targetState, err)
		}
		if err := jobRepo.Save(ctx, st, cur.Version); err != nil {
			t.Fatalf("Save %s failed: %v", targetState, err)
		}
	}

	// At this point version is 7
	snapV7, err := jobRepo.Get(ctx, job.ID)
	if err != nil || snapV7.Version != 7 {
		t.Fatalf("Expected version 7 before crash, got %d (err: %v)", snapV7.Version, err)
	}

	// Step 2: Simulating Reconciler / Recovery process advancing job state to WAITING_FOR_TARGET
	recJob, _ := snapV7.TransitionState(recording.StateWaitingTarget, "")
	if err := jobRepo.Save(ctx, recJob, snapV7.Version); err != nil {
		t.Fatalf("Reconciler save failed: %v", err)
	}
	// Manifest on disk is now Version 8

	// Step 3: Worker fetches fresh job instance (Version 8)
	workerJob, err := jobRepo.Get(ctx, job.ID)
	if err != nil || workerJob.Version != 8 {
		t.Fatalf("Expected worker to fetch Version 8, got %d (err: %v)", workerJob.Version, err)
	}

	// Step 4: Worker successfully advances job state to COMPLETED with expectedVersion 8
	compJob, _ := workerJob.TransitionState(recording.StateCompleted, "")
	if err := jobRepo.Save(ctx, compJob, workerJob.Version); err != nil {
		t.Fatalf("Worker COMPLETED save failed: %v", err)
	}

	// Manifest on disk is now Version 9
	finalJob, _ := jobRepo.Get(ctx, job.ID)
	if finalJob.Version != 9 {
		t.Errorf("Expected final job Version 9, got %d", finalJob.Version)
	}

	// Step 5: Stale pre-crash snapshot (snapV7 with Version 7) attempts save -> MUST be rejected via ErrOptimisticLockConflict
	staleMutation, _ := snapV7.TransitionState(recording.StateFailed, "stale update")
	staleErr := jobRepo.Save(ctx, staleMutation, snapV7.Version)
	if staleErr == nil || !errors.Is(staleErr, recording.ErrOptimisticLockConflict) {
		t.Errorf("Expected ErrOptimisticLockConflict for stale Version 7 save attempt, got %v", staleErr)
	}
}

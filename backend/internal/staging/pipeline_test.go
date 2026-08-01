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

	"github.com/ManuGH/xg2g/internal/domain/recording"
	"github.com/ManuGH/xg2g/internal/infra/storage"
)

// LocalNVMeStorageBackend implements storage.StorageBackend for local NVMe testing.
type LocalNVMeStorageBackend struct {
	id       string
	baseRoot string
}

func NewLocalNVMeStorageBackend(id, baseRoot string) (*LocalNVMeStorageBackend, error) {
	if err := os.MkdirAll(baseRoot, 0755); err != nil {
		return nil, err
	}
	return &LocalNVMeStorageBackend{
		id:       id,
		baseRoot: baseRoot,
	}, nil
}

func (b *LocalNVMeStorageBackend) ID() string { return b.id }
func (b *LocalNVMeStorageBackend) Type() storage.StorageType {
	return storage.StorageTypeLocal
}
func (b *LocalNVMeStorageBackend) Roles() []storage.StorageRole {
	return []storage.StorageRole{storage.RoleRetroDVR, storage.RoleStaging, storage.RoleRecordingTarget}
}
func (b *LocalNVMeStorageBackend) Capabilities() storage.StorageCapabilities {
	return storage.StorageCapabilities{
		SupportsHardlink:         true,
		SupportsReflink:          true,
		SupportsAtomicRename:     true,
		SupportsAtomicReplace:    true,
		RecommendedForRingbuffer: true,
	}
}
func (b *LocalNVMeStorageBackend) Health(ctx context.Context) storage.HealthStatus {
	return storage.HealthStatus{
		State:    storage.HealthStateHealthy,
		Readable: true,
		Writable: true,
	}
}
func (b *LocalNVMeStorageBackend) Capacity(ctx context.Context) (storage.CapacityInfo, error) {
	return storage.CapacityInfo{
		TotalBytes:     500 * 1024 * 1024 * 1024,
		AvailableBytes: 200 * 1024 * 1024 * 1024,
	}, nil
}

func (b *LocalNVMeStorageBackend) Open(ctx context.Context, objectKey string) (storage.ObjectReader, error) {
	fullPath, err := recording.SanitizeAndValidateRelativePath(b.baseRoot, objectKey)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(filepath.Join(b.baseRoot, fullPath))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, storage.ErrObjectNotFound
		}
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return &LocalObjectReader{file: f, size: info.Size()}, nil
}

func (b *LocalNVMeStorageBackend) OpenRange(ctx context.Context, objectKey string, offset, length int64) (io.ReadCloser, error) {
	reader, err := b.Open(ctx, objectKey)
	if err != nil {
		return nil, err
	}
	if _, err := reader.Seek(offset, io.SeekStart); err != nil {
		_ = reader.Close()
		return nil, err
	}
	return struct {
		io.Reader
		io.Closer
	}{
		Reader: io.LimitReader(reader, length),
		Closer: reader,
	}, nil
}

func (b *LocalNVMeStorageBackend) CommitFile(ctx context.Context, srcLocalPath string, targetObjectKey string) error {
	fullPath, err := recording.SanitizeAndValidateRelativePath(b.baseRoot, targetObjectKey)
	if err != nil {
		return err
	}
	targetAbsPath := filepath.Join(b.baseRoot, fullPath)
	if err := os.MkdirAll(filepath.Dir(targetAbsPath), 0755); err != nil {
		return err
	}

	data, err := os.ReadFile(srcLocalPath)
	if err != nil {
		return err
	}
	tmpPath := targetAbsPath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
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
	return os.Rename(tmpPath, targetAbsPath)
}

func (b *LocalNVMeStorageBackend) Stat(ctx context.Context, objectKey string) (storage.ObjectInfo, error) {
	fullPath, err := recording.SanitizeAndValidateRelativePath(b.baseRoot, objectKey)
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	absPath := filepath.Join(b.baseRoot, fullPath)
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return storage.ObjectInfo{}, storage.ErrObjectNotFound
		}
		return storage.ObjectInfo{}, err
	}
	return storage.ObjectInfo{
		ObjectKey: fullPath,
		SizeBytes: info.Size(),
		UpdatedAt: info.ModTime(),
	}, nil
}

func (b *LocalNVMeStorageBackend) DeleteFile(ctx context.Context, targetObjectKey string) error {
	fullPath, err := recording.SanitizeAndValidateRelativePath(b.baseRoot, targetObjectKey)
	if err != nil {
		return err
	}
	absPath := filepath.Join(b.baseRoot, fullPath)
	err = os.Remove(absPath)
	if err != nil && os.IsNotExist(err) {
		return storage.ErrObjectNotFound
	}
	return err
}

type LocalObjectReader struct {
	file *os.File
	size int64
}

func (r *LocalObjectReader) Read(p []byte) (int, error)       { return r.file.Read(p) }
func (r *LocalObjectReader) ReadAt(p []byte, off int64) (int, error) { return r.file.ReadAt(p, off) }
func (r *LocalObjectReader) Seek(offset int64, whence int) (int64, error) {
	return r.file.Seek(offset, whence)
}
func (r *LocalObjectReader) Close() error { return r.file.Close() }
func (r *LocalObjectReader) Size() int64  { return r.size }

func TestRecordingPipeline_EndToEndIntegration(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "e2e_recording_test")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()

	// Initialize repositories and staging manager
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

	storageRoot := filepath.Join(tmpDir, "recordings_storage")
	backend, err := NewLocalNVMeStorageBackend("local-nvme-1", storageRoot)
	if err != nil {
		t.Fatalf("NewLocalNVMeStorageBackend failed: %v", err)
	}

	sm, err := NewStagingManager(jobRepoPath, jobRepo)
	if err != nil {
		t.Fatalf("NewStagingManager failed: %v", err)
	}

	// 1. Create RecordingJob
	now := time.Now()
	job, err := recording.NewRecordingJob("job_e2e_888", "1:0:19:283D:3FB:1:C00000:0:0:0:", "Tatort Wien", recording.SourceRetro, now.Add(-1*time.Hour), now, backend.ID())
	if err != nil {
		t.Fatalf("NewRecordingJob failed: %v", err)
	}

	// 2. Prepare Workspace and ingest segments
	_, err = sm.PrepareWorkspace(ctx, job)
	if err != nil {
		t.Fatalf("PrepareWorkspace failed: %v", err)
	}

	segsDir := sm.SegmentsDir(job.ID)
	seg1 := filepath.Join(segsDir, "seg_000001.ts")
	seg2 := filepath.Join(segsDir, "seg_000002.ts")
	payload1 := []byte("HEADER_FRAME_DATA_PART_1_")
	payload2 := []byte("BODY_FRAME_DATA_PART_2")

	if err := os.WriteFile(seg1, payload1, 0644); err != nil {
		t.Fatalf("WriteFile seg1 failed: %v", err)
	}
	if err := os.WriteFile(seg2, payload2, 0644); err != nil {
		t.Fatalf("WriteFile seg2 failed: %v", err)
	}

	// Transition job state to STAGING
	stagingJob, err := job.TransitionState(recording.StateStaging, "")
	if err != nil {
		t.Fatalf("TransitionState StateStaging failed: %v", err)
	}
	if err := jobRepo.Save(ctx, stagingJob, job.Version); err != nil {
		t.Fatalf("jobRepo.Save StateStaging failed: %v", err)
	}
	job = stagingJob

	// 3. Finalize segments into staged output
	report, err := sm.AssembleAndFinalize(ctx, job.ID, "tatort_wien.ts")
	if err != nil {
		t.Fatalf("AssembleAndFinalize failed: %v", err)
	}

	if !report.Complete {
		t.Errorf("Expected report.Complete to be true!")
	}

	// 4. Commit finalized file to target StorageBackend
	targetRelPath := "TV-Recordings/Tatort Wien.ts"
	if err := backend.CommitFile(ctx, report.FinalizedPath, targetRelPath); err != nil {
		t.Fatalf("backend.CommitFile failed: %v", err)
	}

	// Stat verification
	info, err := backend.Stat(ctx, targetRelPath)
	if err != nil || info.SizeBytes != report.TotalBytes {
		t.Fatalf("backend.Stat failed or size mismatch: %v (size: %d, expected: %d)", err, info.SizeBytes, report.TotalBytes)
	}

	// 5. Create RecordingAsset atomically in AssetRepository
	asset, err := recording.NewRecordingAsset("asset_e2e_888", job.ID, job.Title, job.ServiceRef, backend.ID(), targetRelPath, recording.ContainerTS)
	if err != nil {
		t.Fatalf("NewRecordingAsset failed: %v", err)
	}

	updatedAsset, err := asset.TransitionState(recording.AssetAvailable)
	if err != nil {
		t.Fatalf("TransitionState AssetAvailable failed: %v", err)
	}
	updatedAsset.DurationSeconds = 3600
	updatedAsset.SizeBytes = report.TotalBytes
	updatedAsset.RecordedStart = job.RequestedStart
	updatedAsset.RecordedEnd = job.RequestedEnd

	if err := assetRepo.Save(ctx, updatedAsset, 0); err != nil {
		t.Fatalf("assetRepo.Save initial expectedVersion 0 failed: %v", err)
	}

	// 6. Transition RecordingJob to COMPLETED atomically
	job, err = jobRepo.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("jobRepo.Get latest job failed: %v", err)
	}
	completedJob, err := job.TransitionState(recording.StateCompleted, "")
	if err != nil {
		t.Fatalf("TransitionState StateCompleted failed: %v", err)
	}
	if err := jobRepo.Save(ctx, completedJob, job.Version); err != nil {
		t.Fatalf("jobRepo.Save StateCompleted failed: %v", err)
	}

	// 7. Simulate Process Restart: Instantiate new repositories
	newJobRepo, err := recording.NewDiskJobRepository(jobRepoPath)
	if err != nil {
		t.Fatalf("NewDiskJobRepository restart failed: %v", err)
	}
	newAssetRepo, err := recording.NewDiskAssetRepository(assetRepoPath)
	if err != nil {
		t.Fatalf("NewDiskAssetRepository restart failed: %v", err)
	}

	// 8. Recover Job & Asset from disk
	recoveredJob, err := newJobRepo.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("newJobRepo.Get recovered job failed: %v", err)
	}
	if recoveredJob.State != recording.StateCompleted {
		t.Errorf("Expected recovered job state StateCompleted, got %s", recoveredJob.State)
	}

	recoveredAsset, err := newAssetRepo.Get(ctx, asset.ID)
	if err != nil {
		t.Fatalf("newAssetRepo.Get recovered asset failed: %v", err)
	}
	if recoveredAsset.State != recording.AssetAvailable {
		t.Errorf("Expected recovered asset state AssetAvailable, got %s", recoveredAsset.State)
	}

	// 9. Read media file via StorageBackend.Open() (ObjectReader) and HTTP Range Read
	reader, err := backend.Open(ctx, recoveredAsset.ObjectKey)
	if err != nil {
		t.Fatalf("backend.Open failed: %v", err)
	}
	defer reader.Close()

	if reader.Size() != int64(len(payload1)+len(payload2)) {
		t.Errorf("Expected reader size %d, got %d", len(payload1)+len(payload2), reader.Size())
	}

	// Range read: Offset 0, length len(payload1)
	rangeStream, err := backend.OpenRange(ctx, recoveredAsset.ObjectKey, 0, int64(len(payload1)))
	if err != nil {
		t.Fatalf("backend.OpenRange failed: %v", err)
	}
	defer rangeStream.Close()

	rangeData, err := io.ReadAll(rangeStream)
	if err != nil {
		t.Fatalf("ReadAll rangeStream failed: %v", err)
	}

	if string(rangeData) != string(payload1) {
		t.Errorf("Expected range data '%s', got '%s'", string(payload1), string(rangeData))
	}
}

// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package staging

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

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
					Location:    ringbuffer.SegmentLocation{Kind: ringbuffer.StorageKindDisk, Path: seg1Path},
					Sequence:    1,
					SizeBytes:   int64(len(payload1)),
					DurationSec: 900.0,
				},
				{
					Location:    ringbuffer.SegmentLocation{Kind: ringbuffer.StorageKindDisk, Path: seg2Path},
					Sequence:    2,
					SizeBytes:   int64(len(payload2)),
					DurationSec: 900.0,
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

	backend, err := NewLocalNVMeStorageBackend("local-nvme-1", nvmeRoot)
	if err != nil {
		t.Fatalf("NewLocalNVMeStorageBackend failed: %v", err)
	}

	sm, err := NewStagingManager(jobRepoPath, jobRepo)
	if err != nil {
		t.Fatalf("NewStagingManager failed: %v", err)
	}

	engine, err := NewRetroDVRHandoverEngine(mockResMgr, jobRepo, assetRepo, profileRepo, sm, []storage.StorageBackend{backend})
	if err != nil {
		t.Fatalf("NewRetroDVRHandoverEngine failed: %v", err)
	}

	profile, err := recording.NewRecordingProfile("prof_retro", "Retro Profile", backend.ID(), "Recordings", recording.ContainerTS, recording.NamingPresetMovies)
	if err != nil {
		t.Fatalf("NewRecordingProfile failed: %v", err)
	}

	// Execute Retro Recording Handover
	req := RetroHandoverRequest{
		JobID:      "job_retro_999",
		AssetID:    "asset_retro_999",
		ServiceRef: serviceRef,
		Title:      "Tagesschau Retro",
		StartTime:  now.Add(-30 * time.Minute),
		EndTime:    now,
		Profile:    *profile,
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
}

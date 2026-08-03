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
)

func TestHardenedRecordingPipeline_ManifestAndAssembly(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "staging_hardened_test")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	repo, err := recording.NewDiskJobRepository(tmpDir)
	if err != nil {
		t.Fatalf("NewDiskJobRepository failed: %v", err)
	}

	sm, err := NewStagingManager(tmpDir, repo)
	if err != nil {
		t.Fatalf("NewStagingManager failed: %v", err)
	}

	job, err := recording.NewRecordingJob("job_hardened_101", "1:0:19:283D:3FB:1:C00000:0:0:0:", "Hardened Movie", recording.SourceRetro, time.Now(), time.Now().Add(1*time.Hour), "local-nvme")
	if err != nil {
		t.Fatalf("NewRecordingJob failed: %v", err)
	}

	wsDir, err := sm.PrepareWorkspace(ctx, job)
	if err != nil {
		t.Fatalf("PrepareWorkspace failed: %v", err)
	}
	t.Logf("Prepared workspace at %s", wsDir)

	// Verify manifest.json exists on disk
	manifestFile := repo.ManifestPath(job.ID)
	if _, err := os.Stat(manifestFile); os.IsNotExist(err) {
		t.Fatalf("Manifest file %s was not persisted!", manifestFile)
	}

	// Verify safe workspace recovery: re-calling PrepareWorkspace reuses directory without deleting files!
	segsDir := sm.SegmentsDir(job.ID)
	seg1 := filepath.Join(segsDir, "seg_000001.ts")
	seg3 := filepath.Join(segsDir, "seg_000003.ts") // Gap: seg_000002.ts missing!

	if err := os.WriteFile(seg1, []byte("PART1_"), 0644); err != nil {
		t.Fatalf("WriteFile seg1 failed: %v", err)
	}
	if err := os.WriteFile(seg3, []byte("PART3"), 0644); err != nil {
		t.Fatalf("WriteFile seg3 failed: %v", err)
	}

	// Re-run PrepareWorkspace to verify safe recovery
	_, err = sm.PrepareWorkspace(ctx, job)
	if err != nil {
		t.Fatalf("PrepareWorkspace recovery failed: %v", err)
	}

	if _, err := os.Stat(seg1); os.IsNotExist(err) {
		t.Fatalf("Workspace recovery deleted existing segment file seg1!")
	}

	// Transition job to StateStaging
	stagingJob, err := job.TransitionState(recording.StateStaging, "")
	if err != nil {
		t.Fatalf("TransitionState StateStaging failed: %v", err)
	}
	if err := repo.Save(ctx, stagingJob, job.Version); err != nil {
		t.Fatalf("repo.Save failed: %v", err)
	}

	// Execute Assembly & Finalization
	report, err := sm.AssembleAndFinalize(ctx, job.ID, "final_movie.ts")
	if err != nil {
		t.Fatalf("AssembleAndFinalize failed: %v", err)
	}

	if report.Complete {
		t.Errorf("Expected report.Complete to be false due to missing seg_000002.ts!")
	}
	if len(report.MissingRanges) != 1 {
		t.Errorf("Expected 1 missing range gap, got %d", len(report.MissingRanges))
	} else if report.MissingRanges[0].StartSeq != 2 || report.MissingRanges[0].EndSeq != 2 {
		t.Errorf("Expected missing range [2, 2], got %v", report.MissingRanges[0])
	}

	// Verify job state transitioned to StatePartial in persistent repository
	savedJob, err := repo.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("repo.Get failed: %v", err)
	}
	if savedJob.State != recording.StatePartial {
		t.Errorf("Expected persistent job state StatePartial, got %s", savedJob.State)
	}

	// Verify crash recovery listing
	recoverable, err := repo.ListRecoverable(ctx)
	if err != nil {
		t.Fatalf("ListRecoverable failed: %v", err)
	}
	if len(recoverable) != 0 {
		t.Errorf("Expected 0 recoverable jobs (StatePartial is terminal), got %d", len(recoverable))
	}
}

func TestPathTraversalRejection(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "staging_traversal_test")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	repo, _ := recording.NewDiskJobRepository(tmpDir)
	sm, _ := NewStagingManager(tmpDir, repo)

	// Reject invalid job ID with path traversal
	_, err = recording.NewRecordingJob("../illegal_job", "ref_1", "Title", recording.SourceScheduled, time.Now(), time.Now().Add(1*time.Hour), "local-nvme")
	if err == nil {
		t.Errorf("Expected error for illegal job ID with '../', got nil")
	}

	// Reject path traversal in outputFilename
	job, _ := recording.NewRecordingJob("job_valid_102", "ref_1", "Title", recording.SourceScheduled, time.Now(), time.Now().Add(1*time.Hour), "local-nvme")
	_, _ = sm.PrepareWorkspace(ctx, job)

	_, err = sm.AssembleAndFinalize(ctx, job.ID, "../../../etc/passwd")
	if err == nil {
		t.Errorf("Expected error for path traversal in outputFilename, got nil")
	}
}

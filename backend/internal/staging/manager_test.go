// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package staging

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/recording"
)

func TestStagingManager_WorkspaceAndAssembly(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "staging_test")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	sm, err := NewStagingManager(tmpDir)
	if err != nil {
		t.Fatalf("NewStagingManager failed: %v", err)
	}

	job := recording.NewRecordingJob("job_101", "1:0:19:283D:3FB:1:C00000:0:0:0:", "Test Movie", recording.SourceRetro, time.Now(), time.Now().Add(1*time.Hour), "local-nvme")
	
	wsDir, err := sm.PrepareWorkspace(job)
	if err != nil {
		t.Fatalf("PrepareWorkspace failed: %v", err)
	}

	if _, err := os.Stat(wsDir); os.IsNotExist(err) {
		t.Fatalf("Workspace dir %s was not created!", wsDir)
	}

	// Create 3 segment files in workspace out of order
	seg2 := filepath.Join(wsDir, "seg_000002.ts")
	seg1 := filepath.Join(wsDir, "seg_000001.ts")
	seg3 := filepath.Join(wsDir, "seg_000003.ts")

	_ = os.WriteFile(seg2, []byte("PART2_"), 0644)
	_ = os.WriteFile(seg1, []byte("PART1_"), 0644)
	_ = os.WriteFile(seg3, []byte("PART3"), 0644)

	// Transition job to STAGING
	if err := job.TransitionTo(recording.StatePreparing, ""); err != nil {
		t.Fatalf("TransitionTo StatePreparing failed: %v", err)
	}
	if err := job.TransitionTo(recording.StateStaging, ""); err != nil {
		t.Fatalf("TransitionTo StateStaging failed: %v", err)
	}

	// Assemble segments
	finalPath, err := sm.AssembleSegments(job.ID, "final_movie.ts")
	if err != nil {
		t.Fatalf("AssembleSegments failed: %v", err)
	}

	data, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatalf("ReadFile finalPath failed: %v", err)
	}

	if string(data) != "PART1_PART2_PART3" {
		t.Errorf("Expected assembled data 'PART1_PART2_PART3', got '%s'", string(data))
	}

	// Cleanup workspace
	if err := sm.CleanupWorkspace(job.ID); err != nil {
		t.Fatalf("CleanupWorkspace failed: %v", err)
	}

	if _, err := os.Stat(wsDir); !os.IsNotExist(err) {
		t.Errorf("Workspace dir %s still exists after CleanupWorkspace!", wsDir)
	}
}

func TestRecordingJob_StateTransitions(t *testing.T) {
	job := recording.NewRecordingJob("job_202", "ref_1", "Test Show", recording.SourceScheduled, time.Now(), time.Now().Add(1*time.Hour), "local-nvme")

	if job.CurrentState() != recording.StatePending {
		t.Errorf("Expected initial state PENDING, got %s", job.CurrentState())
	}

	if err := job.TransitionTo(recording.StatePreparing, ""); err != nil {
		t.Errorf("Transition to StatePreparing failed: %v", err)
	}

	if err := job.TransitionTo(recording.StateRecording, ""); err != nil {
		t.Errorf("Transition to StateRecording failed: %v", err)
	}

	// Invalid transition: Recording -> Completed directly (must go through Staging/Finalizing)
	if err := job.TransitionTo(recording.StateCompleted, ""); err == nil {
		t.Errorf("Expected error on invalid transition Recording -> Completed, got nil")
	}

	// Valid transition: Recording -> Staging -> Finalizing -> Completed
	_ = job.TransitionTo(recording.StateStaging, "")
	_ = job.TransitionTo(recording.StateFinalizing, "")
	if err := job.TransitionTo(recording.StateCompleted, ""); err != nil {
		t.Errorf("Transition to StateCompleted failed: %v", err)
	}

	// Terminal state cannot be changed
	if err := job.TransitionTo(recording.StateFailed, "already completed"); err == nil {
		t.Errorf("Expected error when modifying terminal state, got nil")
	}
}

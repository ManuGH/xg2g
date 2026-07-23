// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package fsm

import (
	"errors"
	"testing"
	"time"
)

func TestNewArtifact_Valid(t *testing.T) {
	now := time.Now()
	a, err := NewArtifact("art-123", "rec-456", "var-789", "/tmp/m.m3u8", "/tmp/seg-*.ts", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if a.ID != "art-123" {
		t.Errorf("ID = %q, want art-123", a.ID)
	}
	if a.State != StatePreparing {
		t.Errorf("State = %s, want PREPARING", a.State)
	}
}

func TestNewArtifact_Validation(t *testing.T) {
	now := time.Now()
	if _, err := NewArtifact("", "rec-456", "var-789", "", "", now); err == nil {
		t.Error("expected error for empty ID")
	}
	if _, err := NewArtifact("art-123", "", "var-789", "", "", now); err == nil {
		t.Error("expected error for empty recordingRef")
	}
	if _, err := NewArtifact("art-123", "rec-456", "", "", "", now); err == nil {
		t.Error("expected error for empty variantHash")
	}
}

func TestStateTransitions_Valid(t *testing.T) {
	now := time.Now()
	a, _ := NewArtifact("art-1", "rec-1", "var-1", "/m.m3u8", "/s-*.ts", now)

	// PREPARING -> READY
	if err := CompleteBuild(a, now); err != nil {
		t.Fatalf("CompleteBuild failed: %v", err)
	}
	if a.State != StateReady {
		t.Errorf("State = %s, want READY", a.State)
	}

	// READY -> DELETED
	if err := EvictArtifact(a, now); err != nil {
		t.Fatalf("EvictArtifact failed: %v", err)
	}
	if a.State != StateDeleted {
		t.Errorf("State = %s, want DELETED", a.State)
	}
}

func TestStateTransitions_FailureAndRetry(t *testing.T) {
	now := time.Now()
	a, _ := NewArtifact("art-2", "rec-2", "var-2", "/m.m3u8", "/s-*.ts", now)

	// PREPARING -> FAILED
	if err := FailBuild(a, "transcode crashed", now); err != nil {
		t.Fatalf("FailBuild failed: %v", err)
	}
	if a.State != StateFailed {
		t.Errorf("State = %s, want FAILED", a.State)
	}
	if a.FailureReason != "transcode crashed" {
		t.Errorf("FailureReason = %q, want 'transcode crashed'", a.FailureReason)
	}

	// FAILED -> PREPARING
	if err := RetryBuild(a, now); err != nil {
		t.Fatalf("RetryBuild failed: %v", err)
	}
	if a.State != StatePreparing {
		t.Errorf("State = %s, want PREPARING", a.State)
	}
	if a.FailureReason != "" {
		t.Errorf("FailureReason = %q, want empty after retry", a.FailureReason)
	}
}

func TestStateTransitions_Invalid(t *testing.T) {
	now := time.Now()
	a, _ := NewArtifact("art-3", "rec-3", "var-3", "/m.m3u8", "/s-*.ts", now)

	// Cannot retry PREPARING artifact
	err := RetryBuild(a, now)
	if err == nil {
		t.Error("expected error for RetryBuild on PREPARING artifact")
	}
	if !errors.Is(err, ErrInvalidStateTransition) {
		t.Errorf("expected ErrInvalidStateTransition, got %v", err)
	}

	// Transition to READY
	_ = CompleteBuild(a, now)

	// Cannot CompleteBuild on READY artifact
	err = CompleteBuild(a, now)
	if err == nil {
		t.Error("expected error for CompleteBuild on READY artifact")
	}
	if !errors.Is(err, ErrInvalidStateTransition) {
		t.Errorf("expected ErrInvalidStateTransition, got %v", err)
	}
}

func TestStateTransitions_NilArtifact(t *testing.T) {
	now := time.Now()
	if err := CompleteBuild(nil, now); !errors.Is(err, ErrArtifactNotFound) {
		t.Errorf("expected ErrArtifactNotFound, got %v", err)
	}
}

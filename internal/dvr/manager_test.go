// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package dvr

import (
	"testing"
	"time"
)

func TestUpdateRule_AllowsLastRunUpdate(t *testing.T) {
	manager := NewManager(t.TempDir())

	initialSummary := RunSummary{
		EpgItemsScanned: 2,
		TimersCreated:   1,
	}
	initialTime := time.Unix(1700000000, 0)

	ruleID, err := manager.AddRule(SeriesRule{
		Enabled:        true,
		Keyword:        "News",
		Priority:       5,
		LastRunAt:      initialTime,
		LastRunStatus:  "completed",
		LastRunSummary: initialSummary,
	})
	if err != nil {
		t.Fatalf("failed to create rule: %v", err)
	}

	newTime := time.Unix(1700003600, 0)
	updateSummary := RunSummary{
		EpgItemsScanned: 10,
		TimersCreated:   3,
	}

	err = manager.UpdateRule(ruleID, SeriesRule{
		Enabled:        true,
		Keyword:        "News",
		Priority:       5,
		LastRunAt:      newTime,
		LastRunStatus:  "failed",
		LastRunSummary: updateSummary,
	})
	if err != nil {
		t.Fatalf("failed to update rule: %v", err)
	}

	updated, ok := manager.GetRule(ruleID)
	if !ok {
		t.Fatalf("rule not found after update")
	}

	if !updated.LastRunAt.Equal(newTime) {
		t.Errorf("expected LastRunAt %v, got %v", newTime, updated.LastRunAt)
	}
	if updated.LastRunStatus != "failed" {
		t.Errorf("expected LastRunStatus=failed, got %q", updated.LastRunStatus)
	}
	if updated.LastRunSummary.EpgItemsScanned != 10 {
		t.Errorf("expected EpgItemsScanned=10, got %d", updated.LastRunSummary.EpgItemsScanned)
	}
	if updated.LastRunSummary.TimersCreated != 3 {
		t.Errorf("expected TimersCreated=3, got %d", updated.LastRunSummary.TimersCreated)
	}
}

func TestUpdateRule_PreservesPartialLastRun(t *testing.T) {
	manager := NewManager(t.TempDir())

	seedSummary := RunSummary{
		EpgItemsScanned: 8,
		TimersCreated:   2,
	}
	seedTime := time.Unix(1700007200, 0)

	ruleID, err := manager.AddRule(SeriesRule{
		Enabled:        true,
		Keyword:        "News",
		Priority:       5,
		LastRunAt:      seedTime,
		LastRunStatus:  "completed",
		LastRunSummary: seedSummary,
	})
	if err != nil {
		t.Fatalf("failed to create rule: %v", err)
	}

	newTime := time.Unix(1700010800, 0)
	err = manager.UpdateRule(ruleID, SeriesRule{
		Enabled:   true,
		Keyword:   "News",
		Priority:  5,
		LastRunAt: newTime,
	})
	if err != nil {
		t.Fatalf("failed to update rule: %v", err)
	}

	updated, ok := manager.GetRule(ruleID)
	if !ok {
		t.Fatalf("rule not found after update")
	}

	if !updated.LastRunAt.Equal(newTime) {
		t.Errorf("expected LastRunAt %v, got %v", newTime, updated.LastRunAt)
	}
	if updated.LastRunStatus != "completed" {
		t.Errorf("expected LastRunStatus=completed, got %q", updated.LastRunStatus)
	}
	if updated.LastRunSummary.EpgItemsScanned != 8 {
		t.Errorf("expected EpgItemsScanned=8, got %d", updated.LastRunSummary.EpgItemsScanned)
	}
	if updated.LastRunSummary.TimersCreated != 2 {
		t.Errorf("expected TimersCreated=2, got %d", updated.LastRunSummary.TimersCreated)
	}
	if updated.LastRunSummary != seedSummary {
		t.Errorf("expected LastRunSummary preserved, got %+v", updated.LastRunSummary)
	}
}

func TestUpdateRule_AcceptsZeroSummaryWhenStatusSet(t *testing.T) {
	manager := NewManager(t.TempDir())

	seedSummary := RunSummary{
		EpgItemsScanned: 5,
		TimersCreated:   1,
	}
	seedTime := time.Unix(1700020000, 0)

	ruleID, err := manager.AddRule(SeriesRule{
		Enabled:        true,
		Keyword:        "News",
		Priority:       5,
		LastRunAt:      seedTime,
		LastRunStatus:  "completed",
		LastRunSummary: seedSummary,
	})
	if err != nil {
		t.Fatalf("failed to create rule: %v", err)
	}

	newTime := time.Unix(1700023600, 0)
	err = manager.UpdateRule(ruleID, SeriesRule{
		Enabled:       true,
		Keyword:       "News",
		Priority:      5,
		LastRunAt:     newTime,
		LastRunStatus: "completed",
	})
	if err != nil {
		t.Fatalf("failed to update rule: %v", err)
	}

	updated, ok := manager.GetRule(ruleID)
	if !ok {
		t.Fatalf("rule not found after update")
	}

	if !updated.LastRunAt.Equal(newTime) {
		t.Errorf("expected LastRunAt %v, got %v", newTime, updated.LastRunAt)
	}
	if updated.LastRunSummary != (RunSummary{}) {
		t.Errorf("expected zero LastRunSummary, got %+v", updated.LastRunSummary)
	}
}

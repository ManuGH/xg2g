// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/dvr"
)

func TestUpdateSeriesRule_Success(t *testing.T) {
	// Setup
	tmpDir := t.TempDir()
	manager := dvr.NewManager(tmpDir)

	// Create initial rule
	ruleID, err := manager.AddRule(dvr.SeriesRule{
		Enabled:  true,
		Keyword:  "News",
		Priority: 5,
	})
	if err != nil {
		t.Fatalf("Failed to create initial rule: %v", err)
	}

	// Simulate server-managed fields being set (with meaningful data)
	rule, _ := manager.GetRule(ruleID)
	now := time.Now()
	rule.LastRunAt = now
	rule.LastRunStatus = "completed"
	rule.LastRunSummary = dvr.RunSummary{
		EpgItemsScanned: 10,
		TimersCreated:   2,
	}
	_ = manager.UpdateRule(ruleID, rule)

	// Create server
	srv := &Server{
		cfg:           config.AppConfig{},
		seriesManager: manager,
	}

	// Prepare update request (only editable fields)
	updatePayload := map[string]interface{}{
		"enabled":  false,
		"keyword":  "Updated News",
		"priority": 10,
	}
	body, _ := json.Marshal(updatePayload)

	// Make request
	req := httptest.NewRequest("PUT", "/series-rules/"+ruleID, bytes.NewReader(body))
	w := httptest.NewRecorder()

	srv.UpdateSeriesRule(w, req, ruleID)

	// Assert
	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify response
	var response SeriesRule
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Check updated fields
	if response.Keyword == nil || *response.Keyword != "Updated News" {
		t.Errorf("Expected keyword 'Updated News', got %v", response.Keyword)
	}
	if response.Enabled == nil || *response.Enabled != false {
		t.Errorf("Expected enabled=false, got %v", response.Enabled)
	}
	if response.Priority == nil || *response.Priority != 10 {
		t.Errorf("Expected priority=10, got %v", response.Priority)
	}

	// CRITICAL: Check server-managed fields are preserved
	if response.LastRunAt == nil {
		t.Error("LastRunAt should be preserved (not nil)")
	} else if response.LastRunAt.Before(now.Add(-time.Second)) {
		t.Error("LastRunAt should be recent")
	}

	if response.LastRunStatus == nil || *response.LastRunStatus != "completed" {
		t.Errorf("Expected LastRunStatus='completed', got %v", response.LastRunStatus)
	}

	if response.LastRunSummary == nil {
		t.Error("LastRunSummary should be preserved (not nil)")
	} else {
		if response.LastRunSummary.EpgItemsScanned == nil || *response.LastRunSummary.EpgItemsScanned != 10 {
			t.Errorf("Expected EpgItemsScanned=10, got %v", response.LastRunSummary.EpgItemsScanned)
		}
		if response.LastRunSummary.TimersCreated == nil || *response.LastRunSummary.TimersCreated != 2 {
			t.Errorf("Expected TimersCreated=2, got %v", response.LastRunSummary.TimersCreated)
		}
	}
}

func TestUpdateSeriesRule_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	manager := dvr.NewManager(tmpDir)
	srv := &Server{
		cfg:           config.AppConfig{},
		seriesManager: manager,
	}

	updatePayload := map[string]interface{}{
		"enabled":  true,
		"keyword":  "Test",
		"priority": 1,
	}
	body, _ := json.Marshal(updatePayload)

	req := httptest.NewRequest("PUT", "/series-rules/nonexistent", bytes.NewReader(body))
	w := httptest.NewRecorder()

	srv.UpdateSeriesRule(w, req, "nonexistent")

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404, got %d", w.Code)
	}
}

func TestUpdateSeriesRule_EmptyKeyword(t *testing.T) {
	tmpDir := t.TempDir()
	manager := dvr.NewManager(tmpDir)
	ruleID, _ := manager.AddRule(dvr.SeriesRule{
		Enabled:  true,
		Keyword:  "News",
		Priority: 5,
	})

	srv := &Server{
		cfg:           config.AppConfig{},
		seriesManager: manager,
	}

	updatePayload := map[string]interface{}{
		"enabled":  true,
		"keyword":  "   ", // whitespace only
		"priority": 1,
	}
	body, _ := json.Marshal(updatePayload)

	req := httptest.NewRequest("PUT", "/series-rules/"+ruleID, bytes.NewReader(body))
	w := httptest.NewRecorder()

	srv.UpdateSeriesRule(w, req, ruleID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}
}

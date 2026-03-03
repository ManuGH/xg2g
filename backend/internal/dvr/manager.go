// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package dvr

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
)

var ErrRuleNotFound = errors.New("rule not found")

type Manager struct {
	mu       sync.RWMutex
	rules    map[string]SeriesRule
	dataPath string
}

func NewManager(dataDir string) *Manager {
	return &Manager{
		rules:    make(map[string]SeriesRule),
		dataPath: filepath.Join(dataDir, "series_rules.json"),
	}
}

func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.dataPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	var stored []SeriesRule
	if err := json.Unmarshal(data, &stored); err != nil {
		return err
	}

	m.rules = make(map[string]SeriesRule)
	for _, r := range stored {
		m.rules[r.ID] = r
	}
	return nil
}

func (m *Manager) Save() error {
	m.mu.RLock()
	rules := m.getRulesSlice()
	m.mu.RUnlock()

	return m.saveRulesToFile(rules)
}

// Internal helper to get rules as slice (requires lock)
func (m *Manager) getRulesSlice() []SeriesRule {
	var sorted []SeriesRule
	for _, r := range m.rules {
		sorted = append(sorted, r)
	}
	return sorted
}

// Internal helper to write to disk (no lock)
func (m *Manager) saveRulesToFile(rules []SeriesRule) error {
	data, err := json.MarshalIndent(rules, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.dataPath, data, 0600)
}

// SaveRules is an alias for Save, used by the engine.
func (m *Manager) SaveRules() error {
	return m.Save()
}

func (m *Manager) AddRule(r SeriesRule) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if r.ID == "" {
		r.ID = uuid.New().String()
	}
	m.rules[r.ID] = r

	// Prepare slice while holding lock, then save
	rules := m.getRulesSlice()

	if err := m.saveRulesToFile(rules); err != nil {
		// Rollback on save failure
		delete(m.rules, r.ID)
		return "", fmt.Errorf("failed to save rule: %w", err)
	}

	return r.ID, nil
}

func (m *Manager) GetRule(id string) (SeriesRule, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.rules[id]
	return r, ok
}

func (m *Manager) GetRules() []SeriesRule {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.getRulesSlice()
}

func (m *Manager) UpdateRule(id string, upd SeriesRule) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	existing, ok := m.rules[id]
	if !ok {
		return fmt.Errorf("%w: %s", ErrRuleNotFound, id)
	}

	// Preserve server-managed fields when caller did not supply them.
	upd.ID = id
	hasLastRunStatus := upd.LastRunStatus != ""
	hasLastRunSummary := !runSummaryEmpty(upd.LastRunSummary)
	if upd.LastRunAt.IsZero() {
		upd.LastRunAt = existing.LastRunAt
	}
	if !hasLastRunStatus {
		upd.LastRunStatus = existing.LastRunStatus
	}
	if !hasLastRunSummary && !hasLastRunStatus {
		upd.LastRunSummary = existing.LastRunSummary
	}

	m.rules[id] = upd
	rules := m.getRulesSlice()

	if err := m.saveRulesToFile(rules); err != nil {
		// Rollback on save failure
		m.rules[id] = existing
		return fmt.Errorf("failed to update rule: %w", err)
	}

	return nil
}

func runSummaryEmpty(summary RunSummary) bool {
	return summary.EpgItemsScanned == 0 &&
		summary.EpgItemsMatched == 0 &&
		summary.TimersAttempted == 0 &&
		summary.TimersCreated == 0 &&
		summary.TimersSkipped == 0 &&
		summary.TimersConflicted == 0 &&
		summary.TimersErrored == 0 &&
		!summary.MaxTimersGlobalPerRunHit &&
		!summary.MaxMatchesScannedPerRuleHit &&
		!summary.ReceiverUnreachable
}

func (m *Manager) DeleteRule(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	existing, ok := m.rules[id]
	if !ok {
		return fmt.Errorf("%w: %s", ErrRuleNotFound, id)
	}

	delete(m.rules, id)
	rules := m.getRulesSlice()

	if err := m.saveRulesToFile(rules); err != nil {
		// Rollback on save failure
		m.rules[id] = existing
		return fmt.Errorf("failed to delete rule: %w", err)
	}

	return nil
}

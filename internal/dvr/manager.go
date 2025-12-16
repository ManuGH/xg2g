package dvr

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
)

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
	return os.WriteFile(m.dataPath, data, 0644)
}

// SaveRules is an alias for Save, used by the engine.
func (m *Manager) SaveRules() error {
	return m.Save()
}

func (m *Manager) AddRule(r SeriesRule) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	if r.ID == "" {
		r.ID = uuid.New().String()
	}
	m.rules[r.ID] = r

	// Prepare slice while holding lock, then save
	rules := m.getRulesSlice()

	// We can release lock before I/O if we want, but keeping it simple for now to avoid race conditions on save order
	// Actually, saveRulesToFile does I/O.
	// We can do I/O under lock or outside.
	// If we do inside, we block updates during I/O.
	// If we do outside, we risk overwriting newer save?
	// Given low throughput, inside is safer and simpler.
	_ = m.saveRulesToFile(rules)

	return r.ID
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

func (m *Manager) DeleteRule(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.rules[id]; ok {
		delete(m.rules, id)
		rules := m.getRulesSlice()
		_ = m.saveRulesToFile(rules)
		return true
	}
	return false
}

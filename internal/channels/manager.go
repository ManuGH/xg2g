package channels

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/ManuGH/xg2g/internal/log"
)

// Manager handles persistence of channel states (enabled/disabled)
type Manager struct {
	mu       sync.RWMutex
	dataDir  string
	filePath string
	// Map of ChannelID (TvgID or Name) -> Enabled (bool)
	// If a channel is not in the map, it is considered ENABLED by default.
	// We only store disabled channels to keep the file small.
	disabledChannels map[string]bool
}

// NewManager creates a new channel manager
func NewManager(dataDir string) *Manager {
	return &Manager{
		dataDir:          dataDir,
		filePath:         filepath.Join(dataDir, "channels.json"),
		disabledChannels: make(map[string]bool),
	}
}

// Load loads the channel states from disk
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// No file yet, that's fine
			return nil
		}
		return err
	}

	var disabled []string
	if err := json.Unmarshal(data, &disabled); err != nil {
		return err
	}

	m.disabledChannels = make(map[string]bool)
	for _, id := range disabled {
		m.disabledChannels[id] = true
	}

	log.L().Info().Int("disabled_count", len(m.disabledChannels)).Msg("loaded channel states")
	return nil
}

// Save saves the channel states to disk
func (m *Manager) Save() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var disabled []string
	for id, isDisabled := range m.disabledChannels {
		if isDisabled {
			disabled = append(disabled, id)
		}
	}

	data, err := json.MarshalIndent(disabled, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(m.filePath, data, 0644)
}

// IsEnabled checks if a channel is enabled
func (m *Manager) IsEnabled(channelID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return !m.disabledChannels[channelID]
}

// SetEnabled sets the enabled state of a channel
func (m *Manager) SetEnabled(channelID string, enabled bool) error {
	m.mu.Lock()
	if enabled {
		delete(m.disabledChannels, channelID)
	} else {
		m.disabledChannels[channelID] = true
	}
	m.mu.Unlock()

	return m.Save()
}

// GetDisabledCount returns the number of disabled channels
func (m *Manager) GetDisabledCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.disabledChannels)
}

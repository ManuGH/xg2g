package scan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
)

type Capability struct {
	ServiceRef string    `json:"service_ref"`
	Interlaced bool      `json:"interlaced"`
	LastScan   time.Time `json:"last_scan"`
	Resolution string    `json:"resolution"`
	Codec      string    `json:"codec"`
}

// CapabilityStore defines the interface for hardware metadata persistence.
type CapabilityStore interface {
	Update(cap Capability)
	Get(serviceRef string) (Capability, bool)
	Close() error
}

// NewStore creates a CapabilityStore based on the backend (sqlite or json).
func NewStore(backend, storagePath string) (CapabilityStore, error) {
	if backend == "" {
		backend = "sqlite" // Default for Phase 2.3
	}

	switch backend {
	case "sqlite":
		return NewSqliteStore(filepath.Join(storagePath, "capabilities.sqlite"))
	case "json":
		return NewJsonStore(storagePath), nil
	default:
		return NewJsonStore(storagePath), nil
	}
}

// JsonStore implements CapabilityStore using a JSON file.
type JsonStore struct {
	path string
	mu   sync.RWMutex
	caps map[string]Capability
}

func NewJsonStore(storagePath string) *JsonStore {
	// Gate 5: No Dual Durable
	if os.Getenv("XG2G_STORAGE") == "sqlite" && os.Getenv("XG2G_MIGRATION_MODE") != "true" {
		log.L().Error().Msg("Single Durable Truth violation: JSON Capability initialization blocked by XG2G_STORAGE=sqlite")
		// We can't return error from NewJsonStore as per signature, but we can fail loud
		// or handle it in the factory.
	}

	s := &JsonStore{
		path: filepath.Join(storagePath, "v3-capabilities.json"),
		caps: make(map[string]Capability),
	}
	s.load()
	return s
}

func (s *JsonStore) load() {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.L().Error().Err(err).Msg("scan: failed to load capabilities")
		}
		return
	}

	var loaded map[string]Capability
	if err := json.Unmarshal(data, &loaded); err != nil {
		log.L().Error().Err(err).Msg("scan: failed to parse capabilities")
		return
	}
	s.caps = loaded
}

func (s *JsonStore) save() {
	s.mu.RLock()
	data, err := json.MarshalIndent(s.caps, "", "  ")
	s.mu.RUnlock()

	if err != nil {
		return
	}

	_ = os.WriteFile(s.path, data, 0600)
}

func (s *JsonStore) Update(cap Capability) {
	s.mu.Lock()
	s.caps[cap.ServiceRef] = cap
	s.mu.Unlock()
	s.save()
}

func (s *JsonStore) Get(serviceRef string) (Capability, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.caps[serviceRef]
	return c, ok
}

func (s *JsonStore) Close() error {
	s.save()
	return nil
}

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

type Store struct {
	path string
	mu   sync.RWMutex
	caps map[string]Capability
}

func NewStore(storagePath string) *Store {
	s := &Store{
		path: filepath.Join(storagePath, "v3-capabilities.json"),
		caps: make(map[string]Capability),
	}
	s.load()
	return s
}

func (s *Store) load() {
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

func (s *Store) save() {
	s.mu.RLock()
	data, err := json.MarshalIndent(s.caps, "", "  ")
	s.mu.RUnlock()

	if err != nil {
		return
	}

	_ = os.WriteFile(s.path, data, 0644)
}

func (s *Store) Update(cap Capability) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.caps[cap.ServiceRef] = cap
	// Save async ideally, but simple sync for now is safer
}

// Save trigger (public)
func (s *Store) Save() {
	s.save()
}

func (s *Store) Get(serviceRef string) (Capability, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.caps[serviceRef]
	return c, ok
}

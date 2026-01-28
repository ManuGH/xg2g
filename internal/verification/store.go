package verification

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// FileStore implements Store with memory caching and atomic file persistence.
type FileStore struct {
	mu    sync.RWMutex
	state DriftState
	path  string
}

// NewFileStore creates a new store backed by the given file path.
// It attempts to load existing state from disk on initialization.
func NewFileStore(path string) (*FileStore, error) {
	s := &FileStore{path: path}
	if err := s.load(); err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to load drift state: %w", err)
		}
	}
	return s, nil
}

func (s *FileStore) Get(ctx context.Context) (DriftState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state, !s.state.LastCheck.IsZero()
}

func (s *FileStore) Set(ctx context.Context, st DriftState) error {
	// 1. Sanitize & Validate (Fail-Closed)
	st.Version = 1
	for i := range st.Mismatches {
		if !st.Mismatches[i].Kind.Valid() {
			return fmt.Errorf("invalid mismatch kind: %s", st.Mismatches[i].Kind)
		}
		st.Mismatches[i].Expected = truncateForReport(st.Mismatches[i].Expected)
		st.Mismatches[i].Actual = truncateForReport(st.Mismatches[i].Actual)
	}

	// 2. Update Memory
	s.mu.Lock()
	s.state = st
	s.mu.Unlock()

	// 3. Persist to Disk (Atomic Write + Dir Sync)
	return s.persist(st)
}

func (s *FileStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(filepath.Clean(s.path))
	if err != nil {
		return err
	}

	var st DriftState
	if err := json.Unmarshal(data, &st); err != nil {
		return err
	}
	s.state = st
	return nil
}

func (s *FileStore) persist(st DriftState) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(s.path)
	tmpFile, err := os.CreateTemp(dir, "drift_state_*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	defer func() {
		_ = tmpFile.Close()
		if _, err := os.Stat(tmpPath); err == nil {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("sync temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	// Atomic Rename
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("atomic rename: %w", err)
	}

	// Directory Sync (POSIX Durability)
	if f, err := os.Open(filepath.Clean(dir)); err == nil {
		_ = f.Sync()
		_ = f.Close()
	}

	return nil
}

func truncateForReport(val string) string {
	const maxLen = 256
	if len(val) > maxLen {
		return val[:maxLen] + "...(truncated)"
	}
	return val
}

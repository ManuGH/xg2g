// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lease

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	ErrIntentStoreAlreadyOpen    = errors.New("intent store for file path is already open")
	ErrIntentStoreCorrupt        = errors.New("intent store file is corrupt or invalid")
	ErrIntentDurabilityUncertain = errors.New("intent store persistence durability is uncertain")
)

var (
	openStorePathsMu sync.Mutex
	openStorePaths   = make(map[string]bool)
)

// FileIntentStore is a thread-safe persistent implementation of IntentStore that persists LeaseIntents to a JSON file.
// Enforces single-writer process ownership per file path with atomic file replacement and fsync durability.
type FileIntentStore struct {
	mu        sync.RWMutex
	filePath  string
	cleanPath string
	intents   map[ID]LeaseIntent
	nowFunc   func() time.Time
	closed    bool
}

// FileIntentStoreConfig configures FileIntentStore options.
type FileIntentStoreConfig struct {
	FilePath string
	Clock    func() time.Time
}

// NewFileIntentStore creates or opens a FileIntentStore at the given filePath.
func NewFileIntentStore(filePath string) (*FileIntentStore, error) {
	return NewFileIntentStoreWithConfig(FileIntentStoreConfig{FilePath: filePath})
}

// NewFileIntentStoreWithConfig creates or opens a FileIntentStore using configuration options.
func NewFileIntentStoreWithConfig(cfg FileIntentStoreConfig) (*FileIntentStore, error) {
	if cfg.FilePath == "" {
		return nil, fmt.Errorf("%w: file path cannot be empty", ErrInvalidIntent)
	}

	clean := filepath.Clean(cfg.FilePath)

	openStorePathsMu.Lock()
	if openStorePaths[clean] {
		openStorePathsMu.Unlock()
		return nil, fmt.Errorf("%w: %s", ErrIntentStoreAlreadyOpen, clean)
	}
	openStorePaths[clean] = true
	openStorePathsMu.Unlock()

	dir := filepath.Dir(clean)
	if err := os.MkdirAll(dir, 0755); err != nil {
		openStorePathsMu.Lock()
		delete(openStorePaths, clean)
		openStorePathsMu.Unlock()
		return nil, fmt.Errorf("failed to create intent store directory: %w", err)
	}

	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}

	store := &FileIntentStore{
		filePath:  cfg.FilePath,
		cleanPath: clean,
		intents:   make(map[ID]LeaseIntent),
		nowFunc:   clock,
	}

	if err := store.load(); err != nil {
		_ = store.Close()
		return nil, err
	}

	return store, nil
}

func (s *FileIntentStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	openStorePathsMu.Lock()
	delete(openStorePaths, s.cleanPath)
	openStorePathsMu.Unlock()

	return nil
}

func (s *FileIntentStore) load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read intent store file: %w", err)
	}

	if len(data) == 0 {
		return nil
	}

	var rawMap map[ID]LeaseIntent
	if err := json.Unmarshal(data, &rawMap); err != nil {
		return fmt.Errorf("%w: failed to parse intent store JSON: %v", ErrIntentStoreCorrupt, err)
	}

	// Validate loaded entries for data integrity
	validated := make(map[ID]LeaseIntent, len(rawMap))
	for key, intent := range rawMap {
		if key != intent.IntentID {
			return fmt.Errorf("%w: map key %q does not match intent ID %q", ErrIntentStoreCorrupt, key, intent.IntentID)
		}
		if intent.Revision < 1 {
			return fmt.Errorf("%w: intent %s has invalid revision %d", ErrIntentStoreCorrupt, intent.IntentID, intent.Revision)
		}
		if err := validateIntent(intent); err != nil {
			return fmt.Errorf("%w: intent %s validation failed: %v", ErrIntentStoreCorrupt, intent.IntentID, err)
		}
		validated[key] = intent
	}

	s.intents = validated
	return nil
}

func (s *FileIntentStore) persistLocked(intentsMap map[ID]LeaseIntent) error {
	data, err := json.MarshalIndent(intentsMap, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal intents: %w", err)
	}

	dir := filepath.Dir(s.filePath)
	tmpFile := s.filePath + ".tmp"

	f, err := os.OpenFile(tmpFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to open temp intent file: %w", err)
	}

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpFile)
		return fmt.Errorf("failed to write temp intent file: %w", err)
	}

	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpFile)
		return fmt.Errorf("failed to fsync temp intent file: %w", err)
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("failed to close temp intent file: %w", err)
	}

	if err := os.Rename(tmpFile, s.filePath); err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("failed to rename intent store file: %w", err)
	}

	// fsync parent directory to guarantee directory entry durability
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("%w: open parent directory for fsync: %v", ErrIntentDurabilityUncertain, err)
	}
	if err := d.Sync(); err != nil {
		_ = d.Close()
		return fmt.Errorf("%w: fsync parent directory: %v", ErrIntentDurabilityUncertain, err)
	}
	if err := d.Close(); err != nil {
		return fmt.Errorf("%w: close parent directory: %v", ErrIntentDurabilityUncertain, err)
	}

	return nil
}

func validateIntent(intent LeaseIntent) error {
	if intent.IntentID == "" {
		return fmt.Errorf("%w: intent ID cannot be empty", ErrInvalidIntent)
	}
	if intent.Owner == "" {
		return fmt.Errorf("%w: owner cannot be empty", ErrInvalidOwner)
	}
	if intent.Scope == "" {
		return fmt.Errorf("%w: scope cannot be empty", ErrInvalidScope)
	}

	switch intent.State {
	case IntentStatePending, IntentStateActive, IntentStateReleasing, IntentStateTerminal:
	default:
		return fmt.Errorf("%w: invalid intent state %q", ErrInvalidIntent, intent.State)
	}

	if intent.State == IntentStateActive && intent.LeaseID == "" {
		return fmt.Errorf("%w: ACTIVE intent requires non-empty LeaseID", ErrInvalidIntent)
	}
	if intent.State == IntentStatePending && intent.LeaseID != "" {
		return fmt.Errorf("%w: PENDING intent cannot have LeaseID", ErrInvalidIntent)
	}
	if !intent.CreatedAt.IsZero() && !intent.UpdatedAt.IsZero() && intent.UpdatedAt.Before(intent.CreatedAt) {
		return fmt.Errorf("%w: UpdatedAt (%v) cannot be before CreatedAt (%v)", ErrInvalidIntent, intent.UpdatedAt, intent.CreatedAt)
	}

	return nil
}

func cloneIntentMap(m map[ID]LeaseIntent) map[ID]LeaseIntent {
	cp := make(map[ID]LeaseIntent, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}

// SaveIntent persists an intent enforcing CAS revision increments.
// Memory map is updated ONLY after disk persistence succeeds.
func (s *FileIntentStore) SaveIntent(ctx context.Context, intent LeaseIntent) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if err := validateIntent(intent); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrManagerClosed
	}

	now := s.nowFunc()
	existing, exists := s.intents[intent.IntentID]

	if !exists {
		if intent.Revision == 0 {
			intent.Revision = 1
		} else if intent.Revision != 1 {
			return fmt.Errorf("%w: new intent must start at revision 1, got %d", ErrIntentConflict, intent.Revision)
		}
		if intent.CreatedAt.IsZero() {
			intent.CreatedAt = now
		}
	} else {
		if intent.Revision != existing.Revision+1 {
			return fmt.Errorf("%w: intent update revision must be exactly existing revision+1 (%d), got %d",
				ErrIntentConflict, existing.Revision+1, intent.Revision)
		}
		if intent.CreatedAt.IsZero() {
			intent.CreatedAt = existing.CreatedAt
		}
	}

	if intent.UpdatedAt.IsZero() || intent.UpdatedAt.Before(intent.CreatedAt) {
		intent.UpdatedAt = now
	}

	// Copy-on-write memory safety
	nextMap := cloneIntentMap(s.intents)
	nextMap[intent.IntentID] = intent

	if err := s.persistLocked(nextMap); err != nil {
		return err
	}

	s.intents = nextMap
	return nil
}

// GetIntent retrieves an intent by IntentID.
func (s *FileIntentStore) GetIntent(ctx context.Context, intentID ID) (*LeaseIntent, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrManagerClosed
	}

	intent, exists := s.intents[intentID]
	if !exists {
		return nil, fmt.Errorf("%w: intent %s", ErrIntentNotFound, intentID)
	}
	return &intent, nil
}

// ListIntents returns all persisted intents.
func (s *FileIntentStore) ListIntents(ctx context.Context) ([]LeaseIntent, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrManagerClosed
	}

	res := make([]LeaseIntent, 0, len(s.intents))
	for _, intent := range s.intents {
		res = append(res, intent)
	}
	return res, nil
}

// DeleteIntent removes an intent by IntentID.
// Memory map is updated ONLY after disk persistence succeeds.
func (s *FileIntentStore) DeleteIntent(ctx context.Context, intentID ID) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrManagerClosed
	}

	if _, exists := s.intents[intentID]; !exists {
		return nil // Idempotent delete
	}

	nextMap := cloneIntentMap(s.intents)
	delete(nextMap, intentID)

	if err := s.persistLocked(nextMap); err != nil {
		return err
	}

	s.intents = nextMap
	return nil
}

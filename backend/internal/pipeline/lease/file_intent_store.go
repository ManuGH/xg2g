// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lease

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// FileIntentStore is a thread-safe persistent implementation of IntentStore that persists LeaseIntents to a JSON file.
// Intents survive process restarts and crashes.
type FileIntentStore struct {
	mu       sync.RWMutex
	filePath string
	intents  map[ID]LeaseIntent
}

// NewFileIntentStore creates or opens a FileIntentStore at the given filePath.
func NewFileIntentStore(filePath string) (*FileIntentStore, error) {
	if filePath == "" {
		return nil, fmt.Errorf("%w: file path cannot be empty", ErrInvalidIntent)
	}

	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create intent store directory: %w", err)
	}

	store := &FileIntentStore{
		filePath: filePath,
		intents:  make(map[ID]LeaseIntent),
	}

	if err := store.load(); err != nil {
		return nil, err
	}

	return store, nil
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
		return fmt.Errorf("failed to parse intent store JSON: %w", err)
	}

	s.intents = rawMap
	return nil
}

func (s *FileIntentStore) saveLocked() error {
	data, err := json.MarshalIndent(s.intents, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal intents: %w", err)
	}

	tmpFile := s.filePath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp intent file: %w", err)
	}

	if err := os.Rename(tmpFile, s.filePath); err != nil {
		return fmt.Errorf("failed to rename intent store file: %w", err)
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
	if intent.State == "" {
		return fmt.Errorf("%w: state cannot be empty", ErrInvalidIntent)
	}
	if intent.State == IntentStateActive && intent.LeaseID == "" {
		return fmt.Errorf("%w: ACTIVE intent requires non-empty LeaseID", ErrInvalidIntent)
	}
	return nil
}

// SaveIntent persists an intent. Rejects updates with a lower revision than the existing intent.
func (s *FileIntentStore) SaveIntent(ctx context.Context, intent LeaseIntent) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if err := validateIntent(intent); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	existing, exists := s.intents[intent.IntentID]
	if exists {
		if intent.Revision < existing.Revision {
			return fmt.Errorf("%w: intent revision %d is lower than existing revision %d", ErrIntentConflict, intent.Revision, existing.Revision)
		}
	}

	if intent.CreatedAt.IsZero() {
		if exists {
			intent.CreatedAt = existing.CreatedAt
		} else {
			intent.CreatedAt = time.Now()
		}
	}
	if intent.UpdatedAt.IsZero() {
		intent.UpdatedAt = time.Now()
	}

	s.intents[intent.IntentID] = intent
	return s.saveLocked()
}

// GetIntent retrieves an intent by IntentID.
func (s *FileIntentStore) GetIntent(ctx context.Context, intentID ID) (*LeaseIntent, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

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

	res := make([]LeaseIntent, 0, len(s.intents))
	for _, intent := range s.intents {
		res = append(res, intent)
	}
	return res, nil
}

// DeleteIntent removes an intent by IntentID.
func (s *FileIntentStore) DeleteIntent(ctx context.Context, intentID ID) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.intents[intentID]; !exists {
		return nil // Idempotent delete
	}

	delete(s.intents, intentID)
	return s.saveLocked()
}

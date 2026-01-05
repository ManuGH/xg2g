package resume

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
)

const (
	bucketName = "resume_v1"
	dbName     = "resume.db"
)

// NewStore creates a resume store based on the backend (bolt or memory).
// If path is empty and backend is bolt, it defaults to memory.
func NewStore(backend, dir string) (Store, error) {
	switch backend {
	case "bolt":
		if dir == "" {
			return NewMemoryStore(), nil
		}
		return NewBoltStore(filepath.Join(dir, dbName))
	case "memory":
		return NewMemoryStore(), nil
	default:
		// Default to memory for safety
		return NewMemoryStore(), nil
	}
}

// BoltStore implements Store using BoltDB.
type BoltStore struct {
	db *bolt.DB
}

// NewBoltStore opens a BoltDB resume store.
func NewBoltStore(path string) (*BoltStore, error) {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create resume store dir: %w", err)
	}

	opts := *bolt.DefaultOptions
	opts.Timeout = 2 * time.Second

	db, err := bolt.Open(path, 0600, &opts)
	if err != nil {
		return nil, fmt.Errorf("open resume db: %w", err)
	}

	// Init bucket
	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(bucketName))
		return err
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("init resume bucket: %w", err)
	}

	return &BoltStore{db: db}, nil
}

func (s *BoltStore) Put(ctx context.Context, principalID, recordingID string, state *State) error {
	key := []byte(compositeKey(principalID, recordingID))
	val, err := json.Marshal(state)
	if err != nil {
		return err
	}

	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketName))
		return b.Put(key, val)
	})
}

func (s *BoltStore) Get(ctx context.Context, principalID, recordingID string) (*State, error) {
	key := []byte(compositeKey(principalID, recordingID))
	var state State

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketName))
		val := b.Get(key)
		if val == nil {
			return nil // Not found
		}
		return json.Unmarshal(val, &state)
	})

	if err != nil {
		return nil, err
	}
	if state.UpdatedAt.IsZero() && state.PosSeconds == 0 {
		return nil, nil // Treat empty as not found
	}

	return &state, nil
}

func (s *BoltStore) Delete(ctx context.Context, principalID, recordingID string) error {
	key := []byte(compositeKey(principalID, recordingID))
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketName))
		return b.Delete(key)
	})
}

func (s *BoltStore) Close() error {
	return s.db.Close()
}

// MemoryStore implements Store using a map (thread-safe).
type MemoryStore struct {
	mu   sync.RWMutex
	data map[string]*State
}

// NewMemoryStore creates an in-memory resume store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		data: make(map[string]*State),
	}
}

func (s *MemoryStore) Put(ctx context.Context, principalID, recordingID string, state *State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := compositeKey(principalID, recordingID)
	// Copy to avoid race if caller modifies state later
	clone := *state
	s.data[key] = &clone
	return nil
}

func (s *MemoryStore) Get(ctx context.Context, principalID, recordingID string) (*State, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := compositeKey(principalID, recordingID)
	if val, ok := s.data[key]; ok {
		clone := *val
		return &clone, nil
	}
	return nil, nil
}

func (s *MemoryStore) Delete(ctx context.Context, principalID, recordingID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, compositeKey(principalID, recordingID))
	return nil
}

func (s *MemoryStore) Close() error {
	s.mu.Lock()
	s.data = nil
	s.mu.Unlock()
	return nil
}

func compositeKey(principal, recording string) string {
	return principal + "\x00" + recording
}

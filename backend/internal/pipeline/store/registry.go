package store

import (
	"context"
	"errors"
	"sync"
)

// ObjectReader provides a read-only interface to retrieve immutable media segments.
// This is used by the HTTP layer to read segments from the RAM Shadow Store.
type ObjectReader interface {
	Get(ctx context.Context, streamID StreamID, name string) (Object, error)
}

// StoreRegistry tracks active ObjectReader instances (e.g. Shadow Stores) per session.
// It allows the HTTP layer to discover and read from the RAM store of an active FFmpeg session.
type StoreRegistry interface {
	// Register assigns an ObjectReader to a sessionID. It returns ErrRegistryConflict
	// if a reader is already registered for this session.
	Register(sessionID string, reader ObjectReader) error
	// Lookup retrieves the ObjectReader for a sessionID, if it exists.
	Lookup(sessionID string) (ObjectReader, bool)
	// Unregister removes the ObjectReader for a sessionID.
	Unregister(sessionID string)
}

// MemoryStoreRegistry is a thread-safe in-memory implementation of StoreRegistry.
type MemoryStoreRegistry struct {
	mu      sync.RWMutex
	readers map[string]ObjectReader
}

// NewMemoryStoreRegistry creates a new empty MemoryStoreRegistry.
func NewMemoryStoreRegistry() *MemoryStoreRegistry {
	return &MemoryStoreRegistry{
		readers: make(map[string]ObjectReader),
	}
}

// Register implements StoreRegistry.Register.
func (r *MemoryStoreRegistry) Register(sessionID string, reader ObjectReader) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.readers[sessionID]; exists {
		return ErrRegistryConflict
	}
	r.readers[sessionID] = reader
	return nil
}

// Lookup implements StoreRegistry.Lookup.
func (r *MemoryStoreRegistry) Lookup(sessionID string) (ObjectReader, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	reader, exists := r.readers[sessionID]
	return reader, exists
}

// Unregister implements StoreRegistry.Unregister.
func (r *MemoryStoreRegistry) Unregister(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.readers, sessionID)
}

// ErrRegistryConflict is returned when attempting to register a session ID that is already registered.
var ErrRegistryConflict = errors.New("store registry conflict: session ID already registered")

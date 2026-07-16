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

// RegistrationHandle provides a race-safe mechanism to unregister an ObjectReader from the registry.
type RegistrationHandle interface {
	Close() error
}

// StoreRegistry tracks active ObjectReader instances (e.g. Shadow Stores) per session.
// It allows the HTTP layer to discover and read from the RAM store of an active FFmpeg session.
type StoreRegistry interface {
	// Register assigns an ObjectReader to a sessionID. It returns ErrRegistryConflict
	// if a reader is already registered for this session.
	Register(sessionID string, reader ObjectReader) (RegistrationHandle, error)
	// Lookup retrieves the ObjectReader for a sessionID, if it exists.
	Lookup(sessionID string) (ObjectReader, bool)
	// Unregister removes the ObjectReader for a sessionID unconditionally.
	Unregister(sessionID string)
}

type memRegistrationHandle struct {
	registry  *MemoryStoreRegistry
	sessionID string
	token     uint64
	closed    bool
	mu        sync.Mutex
}

func (h *memRegistrationHandle) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	h.closed = true
	h.registry.unregisterToken(h.sessionID, h.token)
	return nil
}

type readerEntry struct {
	reader ObjectReader
	token  uint64
}

// MemoryStoreRegistry is a thread-safe in-memory implementation of StoreRegistry.
type MemoryStoreRegistry struct {
	mu      sync.RWMutex
	readers map[string]readerEntry
	nextID  uint64
}

// NewMemoryStoreRegistry creates a new empty MemoryStoreRegistry.
func NewMemoryStoreRegistry() *MemoryStoreRegistry {
	return &MemoryStoreRegistry{
		readers: make(map[string]readerEntry),
	}
}

// Register implements StoreRegistry.Register.
func (r *MemoryStoreRegistry) Register(sessionID string, reader ObjectReader) (RegistrationHandle, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.readers[sessionID]; exists {
		return nil, ErrRegistryConflict
	}
	r.nextID++
	token := r.nextID
	r.readers[sessionID] = readerEntry{reader: reader, token: token}

	return &memRegistrationHandle{
		registry:  r,
		sessionID: sessionID,
		token:     token,
	}, nil
}

// Lookup implements StoreRegistry.Lookup.
func (r *MemoryStoreRegistry) Lookup(sessionID string) (ObjectReader, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, exists := r.readers[sessionID]
	if !exists {
		return nil, false
	}
	return entry.reader, true
}

// Unregister implements StoreRegistry.Unregister.
func (r *MemoryStoreRegistry) Unregister(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.readers, sessionID)
}

func (r *MemoryStoreRegistry) unregisterToken(sessionID string, token uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if entry, exists := r.readers[sessionID]; exists && entry.token == token {
		delete(r.readers, sessionID)
	}
}

// ErrRegistryConflict is returned when attempting to register a session ID that is already registered.
var ErrRegistryConflict = errors.New("store registry conflict: session ID already registered")

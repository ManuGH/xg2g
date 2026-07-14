package store

import (
	"bytes"
	"container/list"
	"context"
	"errors"
	"fmt"
	"sync"
)

// ErrInvalidCapacity is returned if the store is configured with <= 0 bytes.
var ErrInvalidCapacity = errors.New("invalid capacity")

// ErrCapacityInvariant is returned if the store cannot evict enough bytes to meet capacity.
var ErrCapacityInvariant = errors.New("capacity invariant violated")

// StoreStats provides observability metrics for the store.
type StoreStats struct {
	CurrentBytes   int64
	CurrentObjects int
	EvictionsTotal uint64
	PublishesTotal uint64
	PublishErrors  uint64
}

type storedItem struct {
	Object
	element *list.Element
}

type evictEntry struct {
	streamID StreamID
	name     string
}

// RAMShadowStore implements ShadowStore using an in-memory map constrained by MaxStoredBytes.
// It tracks objects by stream and enforces memory limits by evicting older items.
type RAMShadowStore struct {
	mu               sync.RWMutex
	maxStoredBytes   int64
	maxStoredObjects int
	currentBytes     int64

	streams  map[StreamID]map[string]*storedItem
	eviction *list.List

	evictionsTotal uint64
	publishesTotal uint64
	publishErrors  uint64
}

// NewRAMShadowStore creates a new in-memory shadow store with the given byte and object limits.
func NewRAMShadowStore(maxStoredBytes int64, maxStoredObjects int) (*RAMShadowStore, error) {
	if maxStoredBytes <= 0 {
		return nil, fmt.Errorf("maxStoredBytes must be > 0")
	}
	if maxStoredObjects <= 0 {
		return nil, fmt.Errorf("maxStoredObjects must be > 0")
	}

	return &RAMShadowStore{
		maxStoredBytes:   maxStoredBytes,
		maxStoredObjects: maxStoredObjects,
		streams:          make(map[StreamID]map[string]*storedItem),
		eviction:         list.New(),
	}, nil
}

// Stats returns a snapshot of current store statistics.
func (s *RAMShadowStore) Stats() StoreStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return StoreStats{
		CurrentBytes:   s.currentBytes,
		CurrentObjects: s.eviction.Len(),
		EvictionsTotal: s.evictionsTotal,
		PublishesTotal: s.publishesTotal,
		PublishErrors:  s.publishErrors,
	}
}

// Publish stores the object. If the object exceeds the total store limit, ErrObjectTooLarge is returned.
// If the object causes the store to exceed maxStoredBytes, older objects are evicted.
func (s *RAMShadowStore) Publish(ctx context.Context, streamID StreamID, object Object) error {
	objSize := int64(len(object.Data))

	if objSize > s.maxStoredBytes {
		s.mu.Lock()
		s.publishErrors++
		s.mu.Unlock()
		return ErrObjectTooLarge
	}

	// Defensive copy to guarantee ownership, done only after passing the size check
	object.Data = bytes.Clone(object.Data)

	s.mu.Lock()
	defer s.mu.Unlock()

	sd, ok := s.streams[streamID]
	if !ok {
		sd = make(map[string]*storedItem)
		s.streams[streamID] = sd
	}

	if existing, exists := sd[object.Name]; exists {
		s.currentBytes -= int64(len(existing.Data))
		s.eviction.Remove(existing.element)
	}

	// Evict until we have enough space or enough object headroom
	for s.currentBytes+objSize > s.maxStoredBytes || s.eviction.Len() >= s.maxStoredObjects {
		if !s.evictOldest() {
			s.publishErrors++
			return ErrCapacityInvariant
		}
	}

	element := s.eviction.PushBack(evictEntry{
		streamID: streamID,
		name:     object.Name,
	})

	sd[object.Name] = &storedItem{
		Object:  object,
		element: element,
	}
	s.currentBytes += objSize
	s.publishesTotal++

	return nil
}

// Delete removes a specific object from the store.
func (s *RAMShadowStore) Delete(ctx context.Context, streamID StreamID, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sd, ok := s.streams[streamID]
	if !ok {
		return nil
	}

	if existing, exists := sd[name]; exists {
		s.currentBytes -= int64(len(existing.Data))
		s.eviction.Remove(existing.element)
		delete(sd, name)
	}
	return nil
}

// DeleteStream removes an entire stream and all its objects from the store.
func (s *RAMShadowStore) DeleteStream(ctx context.Context, streamID StreamID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sd, ok := s.streams[streamID]
	if !ok {
		return nil
	}

	for _, item := range sd {
		s.currentBytes -= int64(len(item.Data))
		s.eviction.Remove(item.element)
	}
	delete(s.streams, streamID)
	return nil
}

// Get retrieves an object by streamID and name.
func (s *RAMShadowStore) Get(ctx context.Context, streamID StreamID, name string) (Object, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sd, ok := s.streams[streamID]
	if !ok {
		return Object{}, ErrNotFound
	}
	item, exists := sd[name]
	if !exists {
		return Object{}, ErrNotFound
	}

	// Return an immutable copy of the data
	objCopy := item.Object
	objCopy.Data = bytes.Clone(item.Data)

	return objCopy, nil
}

// evictOldest removes the oldest object across all streams.
// Must be called with lock held.
func (s *RAMShadowStore) evictOldest() bool {
	front := s.eviction.Front()
	if front == nil {
		return false
	}

	entry := front.Value.(evictEntry)
	sd, ok := s.streams[entry.streamID]
	if ok {
		if item, exists := sd[entry.name]; exists {
			s.currentBytes -= int64(len(item.Data))
			delete(sd, entry.name)
		}
	}
	s.eviction.Remove(front)
	s.evictionsTotal++
	return true
}

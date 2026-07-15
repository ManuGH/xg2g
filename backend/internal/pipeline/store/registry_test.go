package store

import (
	"context"
	"testing"
)

type dummyReader struct{}

func (d *dummyReader) Get(ctx context.Context, streamID StreamID, name string) (Object, error) {
	return Object{}, ErrNotFound
}

func TestMemoryStoreRegistry(t *testing.T) {
	registry := NewMemoryStoreRegistry()

	// Test Lookup on empty registry
	_, ok := registry.Lookup("session-1")
	if ok {
		t.Error("Expected false for non-existent session")
	}

	// Test Register
	r1 := &dummyReader{}
	err := registry.Register("session-1", r1)
	if err != nil {
		t.Errorf("Expected nil error, got %v", err)
	}

	// Test Duplicate Registration
	err = registry.Register("session-1", &dummyReader{})
	if err != ErrRegistryConflict {
		t.Errorf("Expected ErrRegistryConflict, got %v", err)
	}

	// Test Lookup existing
	r, ok := registry.Lookup("session-1")
	if !ok {
		t.Error("Expected true for existing session")
	}
	if r != r1 {
		t.Error("Expected returned reader to match registered reader")
	}

	// Test Unregister
	registry.Unregister("session-1")
	_, ok = registry.Lookup("session-1")
	if ok {
		t.Error("Expected false after Unregister")
	}

	// Test Unregister non-existent doesn't panic
	registry.Unregister("session-2")
}

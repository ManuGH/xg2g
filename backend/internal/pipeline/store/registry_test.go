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
	handle1, err := registry.Register("session-1", r1)
	if err != nil {
		t.Errorf("Expected nil error, got %v", err)
	}
	if handle1 == nil {
		t.Error("Expected non-nil registration handle")
	}

	// Test Duplicate Registration
	_, err = registry.Register("session-1", &dummyReader{})
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

	// Test Handle Close
	if err := handle1.Close(); err != nil {
		t.Errorf("Expected nil error from handle.Close(), got %v", err)
	}
	_, ok = registry.Lookup("session-1")
	if ok {
		t.Error("Expected false after handle.Close()")
	}

	// Test Unregister non-existent doesn't panic
	registry.Unregister("session-2")
}

func TestMemoryStoreRegistry_RaceSafeHandleClose(t *testing.T) {
	registry := NewMemoryStoreRegistry()
	r1 := &dummyReader{}
	r2 := &dummyReader{}

	handle1, err := registry.Register("session-race", r1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Force unregister (simulating hard cleanup or session recycle) and register r2
	registry.Unregister("session-race")
	handle2, err := registry.Register("session-race", r2)
	if err != nil {
		t.Fatalf("unexpected error registering r2: %v", err)
	}

	// Late defer of old generation attempts to close handle1
	if err := handle1.Close(); err != nil {
		t.Errorf("handle1.Close() unexpected error: %v", err)
	}

	// r2 must still be present in the registry!
	r, ok := registry.Lookup("session-race")
	if !ok || r != r2 {
		t.Errorf("handle1.Close() unexpectedly evicted new reader entry r2")
	}

	_ = handle2.Close()
}


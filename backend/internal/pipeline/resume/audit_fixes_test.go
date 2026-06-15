package resume

import (
	"context"
	"errors"
	"testing"
)

// L15: Put after Close must return ErrStoreClosed instead of panicking. Close() nils the
// backing map, so the previous `s.data[key] = ...` panicked with "assignment to entry in
// nil map".
func TestMemoryStorePutAfterCloseReturnsErr(t *testing.T) {
	s := NewMemoryStore()
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	err := s.Put(context.Background(), "principal", "recording", &State{})
	if !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("expected ErrStoreClosed, got %v", err)
	}
}

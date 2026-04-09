package resume

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func openResumeStore(t *testing.T, backend string) Store {
	t.Helper()

	store, err := NewStore(backend, t.TempDir())
	if err != nil {
		t.Fatalf("new %s resume store: %v", backend, err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close %s resume store: %v", backend, err)
		}
	})
	return store
}

func TestStoreContract_PutGetRoundTripAndLastWriteWins(t *testing.T) {
	backends := []string{"memory", "sqlite"}
	for _, backend := range backends {
		t.Run(backend, func(t *testing.T) {
			store := openResumeStore(t, backend)
			ctx := context.Background()

			first := &State{
				PosSeconds:      120,
				DurationSeconds: 3600,
				UpdatedAt:       time.Date(2026, time.April, 9, 12, 0, 0, 0, time.UTC),
				Fingerprint:     "id:recording-1",
				Finished:        false,
			}
			if err := store.Put(ctx, "viewer", "recording-1", first); err != nil {
				t.Fatalf("put first state: %v", err)
			}

			got, err := store.Get(ctx, "viewer", "recording-1")
			if err != nil {
				t.Fatalf("get first state: %v", err)
			}
			if !reflect.DeepEqual(got, first) {
				t.Fatalf("expected first roundtrip %#v, got %#v", first, got)
			}

			second := &State{
				PosSeconds:      3599,
				DurationSeconds: 3600,
				UpdatedAt:       time.Date(2026, time.April, 9, 13, 30, 0, 0, time.UTC),
				Fingerprint:     "id:recording-1:v2",
				Finished:        true,
			}
			if err := store.Put(ctx, "viewer", "recording-1", second); err != nil {
				t.Fatalf("put second state: %v", err)
			}

			got, err = store.Get(ctx, "viewer", "recording-1")
			if err != nil {
				t.Fatalf("get second state: %v", err)
			}
			if !reflect.DeepEqual(got, second) {
				t.Fatalf("expected last write %#v, got %#v", second, got)
			}
		})
	}
}

func TestStoreContract_DeleteMissingIsNoopAndDeleteRemovesState(t *testing.T) {
	backends := []string{"memory", "sqlite"}
	for _, backend := range backends {
		t.Run(backend, func(t *testing.T) {
			store := openResumeStore(t, backend)
			ctx := context.Background()

			if err := store.Delete(ctx, "viewer", "missing-recording"); err != nil {
				t.Fatalf("delete missing state: %v", err)
			}
			if err := store.Put(ctx, "viewer", "recording-1", &State{
				PosSeconds:      42,
				DurationSeconds: 3600,
				UpdatedAt:       time.Date(2026, time.April, 9, 12, 0, 0, 0, time.UTC),
				Fingerprint:     "id:recording-1",
			}); err != nil {
				t.Fatalf("put state: %v", err)
			}
			if err := store.Delete(ctx, "viewer", "recording-1"); err != nil {
				t.Fatalf("delete stored state: %v", err)
			}

			got, err := store.Get(ctx, "viewer", "recording-1")
			if err != nil {
				t.Fatalf("get deleted state: %v", err)
			}
			if got != nil {
				t.Fatalf("expected deleted state to be absent, got %#v", got)
			}
		})
	}
}

func TestStoreContract_GetMissingReturnsNil(t *testing.T) {
	backends := []string{"memory", "sqlite"}
	for _, backend := range backends {
		t.Run(backend, func(t *testing.T) {
			store := openResumeStore(t, backend)
			got, err := store.Get(context.Background(), "viewer", "missing-recording")
			if err != nil {
				t.Fatalf("get missing state: %v", err)
			}
			if got != nil {
				t.Fatalf("expected missing state to be nil, got %#v", got)
			}
		})
	}
}

func TestStoreContract_PutRejectsNilState(t *testing.T) {
	backends := []string{"memory", "sqlite"}
	for _, backend := range backends {
		t.Run(backend, func(t *testing.T) {
			store := openResumeStore(t, backend)
			err := store.Put(context.Background(), "viewer", "recording-1", nil)
			if !errors.Is(err, ErrNilState) {
				t.Fatalf("expected ErrNilState, got %v", err)
			}
		})
	}
}

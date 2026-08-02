// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestLeaseStore_Contract(t *testing.T) {
	ctx := context.Background()

	stores := map[string]func(t *testing.T) LeaseStore{
		"MemoryStore": func(t *testing.T) LeaseStore {
			return NewMemoryStore()
		},
		"SqliteStore": func(t *testing.T) LeaseStore {
			dbPath := filepath.Join(t.TempDir(), "store.sqlite")
			st, err := NewSqliteStore(dbPath)
			if err != nil {
				t.Fatalf("failed to create SqliteStore: %v", err)
			}
			t.Cleanup(func() { _ = st.Close() })
			return st
		},
		"InstrumentedStore": func(t *testing.T) LeaseStore {
			return NewInstrumentedStore(NewMemoryStore(), "memory_test")
		},
	}

	for name, factory := range stores {
		t.Run(name, func(t *testing.T) {
			s := factory(t)

			// 1. Acquire lease
			lease, ok, err := s.TryAcquireLease(ctx, "tuner:0", "owner-1", 10*time.Minute)
			if err != nil || !ok || lease == nil {
				t.Fatalf("failed to acquire lease: ok=%v, err=%v", ok, err)
			}

			// 2. ReleaseLease returns nil
			err = s.ReleaseLease(ctx, "tuner:0", "owner-1")
			if err != nil {
				t.Fatalf("expected nil from ReleaseLease, got %v", err)
			}

			// 3. GetLease reports absent
			_, ok, err = s.GetLease(ctx, "tuner:0")
			if err != nil {
				t.Fatalf("GetLease returned error: %v", err)
			}
			if ok {
				t.Errorf("expected GetLease to report absent after release")
			}

			// 4. ListLeases does not contain lease
			leases, err := s.ListLeases(ctx)
			if err != nil {
				t.Fatalf("ListLeases returned error: %v", err)
			}
			for _, l := range leases {
				if l.Key() == "tuner:0" {
					t.Errorf("ListLeases still contains released lease key tuner:0")
				}
			}
		})
	}
}

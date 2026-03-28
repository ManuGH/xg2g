// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package library

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestServiceGetRootItems_RootNotFound(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	svc := NewService(nil, store)
	_, _, err = svc.GetRootItems(context.Background(), "missing-root", 10, 0)
	if err == nil {
		t.Fatal("expected error for missing root")
	}
	if !errors.Is(err, ErrRootNotFound) {
		t.Fatalf("expected ErrRootNotFound, got: %v", err)
	}
}

func TestNewService_DoesNotInitializeRootsDuringConstruction(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	svc := NewService([]RootConfig{{
		ID:   "movies",
		Type: "local",
	}}, store)
	if svc == nil {
		t.Fatal("expected service")
	}

	roots, err := store.GetRoots(context.Background())
	if err != nil {
		t.Fatalf("get roots: %v", err)
	}
	if len(roots) != 0 {
		t.Fatalf("expected constructor to avoid DB writes, got %#v", roots)
	}
}

func TestServiceInitializeRoots_UpsertsConfiguredRoots(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	svc := NewService([]RootConfig{
		{ID: "movies", Type: "local"},
		{ID: "shows", Type: "nfs"},
	}, store)

	if err := svc.InitializeRoots(context.Background()); err != nil {
		t.Fatalf("initialize roots: %v", err)
	}
	if err := svc.InitializeRoots(context.Background()); err != nil {
		t.Fatalf("initialize roots second pass: %v", err)
	}

	roots, err := store.GetRoots(context.Background())
	if err != nil {
		t.Fatalf("get roots: %v", err)
	}
	if len(roots) != 2 {
		t.Fatalf("expected 2 roots, got %#v", roots)
	}
	if roots[0].ID != "movies" || roots[0].Type != "local" {
		t.Fatalf("unexpected first root: %#v", roots[0])
	}
	if roots[1].ID != "shows" || roots[1].Type != "nfs" {
		t.Fatalf("unexpected second root: %#v", roots[1])
	}
}

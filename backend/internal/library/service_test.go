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

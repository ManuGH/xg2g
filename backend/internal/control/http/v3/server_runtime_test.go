package v3

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
)

func TestSetRuntimeContext_InitializesLibraryRoots(t *testing.T) {
	cfg := config.AppConfig{
		Library: config.LibraryConfig{
			Enabled: true,
			DBPath:  filepath.Join(t.TempDir(), "library.db"),
			Roots: []config.LibraryRootConfig{
				{
					ID:   "movies",
					Path: t.TempDir(),
					Type: "local",
				},
			},
		},
	}

	s := NewServer(cfg, nil, nil)
	t.Cleanup(func() {
		_ = s.Shutdown(context.Background())
	})

	if err := s.SetRuntimeContext(context.Background()); err != nil {
		t.Fatalf("set runtime context: %v", err)
	}

	librarySvc := s.LibraryService()
	if librarySvc == nil {
		t.Fatal("expected library service")
	}

	roots, err := librarySvc.GetRoots(context.Background())
	if err != nil {
		t.Fatalf("get roots: %v", err)
	}
	if len(roots) != 1 {
		t.Fatalf("expected 1 root, got %#v", roots)
	}
	if roots[0].ID != "movies" || roots[0].Type != "local" {
		t.Fatalf("unexpected root: %#v", roots[0])
	}
}

package recordings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
)

func TestPathMapper_ResolveLocalExisting(t *testing.T) {
	// Setup temporary directory structure
	tmpDir, err := os.MkdirTemp("", "pathmap_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create root directory
	recRoot := filepath.Join(tmpDir, "rec")
	if err := os.Mkdir(recRoot, 0750); err != nil {
		t.Fatalf("failed to create root dir: %v", err)
	}

	// Create a file inside root
	validFile := filepath.Join(recRoot, "movie.ts")
	if err := os.WriteFile(validFile, []byte("content"), 0600); err != nil {
		t.Fatalf("failed to create valid file: %v", err)
	}

	// Create a directory outside root
	secretDir := filepath.Join(tmpDir, "secret")
	if err := os.Mkdir(secretDir, 0750); err != nil {
		t.Fatalf("failed to create secret dir: %v", err)
	}
	secretFile := filepath.Join(secretDir, "passwd")
	if err := os.WriteFile(secretFile, []byte("secret"), 0600); err != nil {
		t.Fatalf("failed to create secret file: %v", err)
	}

	// Create symlink INSIDE root pointing OUTSIDE root (escape)
	linkEscape := filepath.Join(recRoot, "escape")
	if err := os.Symlink(secretDir, linkEscape); err != nil {
		t.Fatalf("failed to create escape symlink: %v", err)
	}

	// Create symlink INSIDE root pointing INSIDE root (valid)
	subdir := filepath.Join(recRoot, "subdir")
	if err := os.Mkdir(subdir, 0750); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}
	linkValid := filepath.Join(recRoot, "valid_link")
	if err := os.Symlink(subdir, linkValid); err != nil {
		t.Fatalf("failed to create valid symlink: %v", err)
	}

	// Create a "prefix confusion" directory
	// /tmp/pathmap_test/rec_evil
	recEvil := filepath.Join(tmpDir, "rec_evil")
	if err := os.Mkdir(recEvil, 0750); err != nil {
		t.Fatalf("failed to create evil root: %v", err)
	}
	evilFile := filepath.Join(recEvil, "evil.ts")
	if err := os.WriteFile(evilFile, []byte("evil"), 0600); err != nil {
		t.Fatalf("failed to create evil file: %v", err)
	}

	mappings := []config.RecordingPathMapping{
		{
			ReceiverRoot: "/media/hdd",
			LocalRoot:    recRoot,
		},
	}
	pm := NewPathMapper(mappings)

	tests := []struct {
		name           string
		receiverPath   string
		wantOK         bool
		wantPathSuffix string // Check suffix because tmp dir is random
	}{
		{
			name:           "Happy Path Existing File",
			receiverPath:   "/media/hdd/movie.ts",
			wantOK:         true,
			wantPathSuffix: "movie.ts",
		},
		{
			name:         "Non-Existent File",
			receiverPath: "/media/hdd/missing.ts",
			wantOK:       false,
		},
		{
			name:         "Traversal Attempt",
			receiverPath: "/media/hdd/../secret/passwd",
			wantOK:       false,
		},
		{
			name:         "Symlink Escape (Direct)",
			receiverPath: "/media/hdd/escape/passwd",
			wantOK:       false, // Should be blocked
		},
		{
			name:         "Valid Symlink",
			receiverPath: "/media/hdd/valid_link",
			wantOK:       true,
		},
		{
			name: "Prefix Confusion",
			// This effectively maps to validLocalRoot + _evil/evil.ts IF simply joined
			// But since we use ResolveLocal, it constructs path based on mapping.
			// Ideally we want to test if LocalRoot enforcement works.
			// Since ResolveLocal constructs path from LocalRoot + Rel, we can't easily injection ".." unless we bypass the cleaner.
			// But let's verify standard behavior.
			receiverPath: "/media/hdd_evil/evil.ts",
			wantOK:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPath, gotOK := pm.ResolveLocalExisting(tt.receiverPath)
			if gotOK != tt.wantOK {
				t.Errorf("ResolveLocalExisting() gotOK = %v, want %v", gotOK, tt.wantOK)
			}
			if gotOK && tt.wantPathSuffix != "" {
				if filepath.Base(gotPath) != filepath.Base(tt.wantPathSuffix) {
					t.Errorf("ResolveLocalExisting() gotPath = %v, want suffix %v", gotPath, tt.wantPathSuffix)
				}
			}
		})
	}
}

// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package fs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfineRelPath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "confine_test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Fatalf("cleanup tmp dir: %v", err)
		}
	})

	// Create a subdirectory "subdir"
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subDir, 0o750); err != nil {
		t.Fatal(err)
	}

	nestedDir := filepath.Join(subDir, "nested")
	if err := os.Mkdir(nestedDir, 0o750); err != nil {
		t.Fatal(err)
	}

	// Create a regular file "safe.txt"
	safeFile := filepath.Join(tmpDir, "safe.txt")
	if err := os.WriteFile(safeFile, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Create a symlink "link_outside" -> /etc/passwd (or just ../)
	// We'll link to parent of tmpDir (usually /tmp)
	linkOutside := filepath.Join(tmpDir, "link_outside")
	if err := os.Symlink("..", linkOutside); err != nil {
		t.Fatal(err)
	}

	linkChainOutside := filepath.Join(tmpDir, "link_chain_outside")
	if err := os.Symlink("link_outside", linkChainOutside); err != nil {
		t.Fatal(err)
	}

	linkInside := filepath.Join(tmpDir, "link_inside")
	if err := os.Symlink("subdir", linkInside); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		root     string
		target   string
		wantErr  bool
		wantPath string // if not empty, checks suffix
	}{
		{
			name:     "valid simple file",
			root:     tmpDir,
			target:   "safe.txt",
			wantErr:  false,
			wantPath: "safe.txt",
		},
		{
			name:   "valid subdir file",
			root:   tmpDir,
			target: "subdir/foo.txt", // doesn't need to exist for name check, but resolution might fail if we checked existence?
			// Wait, ResolveAndCheck checks Lstat. So target must exist or parent must exist.
			// "subdir" exists. "foo.txt" does not.
			// Logic: "File does not exist? Check parent." -> Parent "subdir" exists.
			// "EvalSymlinks(subdir)" ok.
			// "Join(realSubdir, foo.txt)"
			// "Rel(realRoot, ...)" ok.
			wantErr:  false,
			wantPath: "subdir/foo.txt",
		},
		{
			name:     "clean path variant inside root",
			root:     tmpDir,
			target:   "subdir/../safe.txt",
			wantErr:  false,
			wantPath: "safe.txt",
		},
		{
			name:    "traversal attempt ..",
			root:    tmpDir,
			target:  "../outside.txt",
			wantErr: true,
		},
		{
			name:    "cleaned traversal attempt",
			root:    tmpDir,
			target:  "subdir/nested/../../../outside.txt",
			wantErr: true,
		},
		{
			name:    "traversal attempt /",
			root:    tmpDir,
			target:  "/etc/passwd",
			wantErr: true,
		},
		{
			name:    "symlink escape",
			root:    tmpDir,
			target:  "link_outside/foo",
			wantErr: true, // "link_outside" resolves to parent, so it escapes
		},
		{
			name:    "symlink chain escape",
			root:    tmpDir,
			target:  "link_chain_outside/foo",
			wantErr: true,
		},
		{
			name:     "missing leaf under inside symlink",
			root:     tmpDir,
			target:   "link_inside/missing.txt",
			wantErr:  false,
			wantPath: "subdir/missing.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConfineRelPath(tt.root, tt.target)
			if (err != nil) != tt.wantErr {
				t.Errorf("ConfineRelPath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.wantPath != "" {
				if !strings.HasSuffix(got, tt.wantPath) {
					t.Errorf("ConfineRelPath() got = %v, want suffix %v", got, tt.wantPath)
				}
			}
		})
	}
}

func TestConfineAbsPath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "confine_abs_test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Fatalf("cleanup tmp dir: %v", err)
		}
	})

	// safe file
	safePath := filepath.Join(tmpDir, "safe.txt")
	if err := os.WriteFile(safePath, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}

	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subDir, 0o750); err != nil {
		t.Fatal(err)
	}

	linkInside := filepath.Join(tmpDir, "link_inside")
	if err := os.Symlink("subdir", linkInside); err != nil {
		t.Fatal(err)
	}

	// outside file
	outsideDir, err := os.MkdirTemp("", "outside")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(outsideDir); err != nil {
			t.Fatalf("cleanup outside dir: %v", err)
		}
	})
	outsidePath := filepath.Join(outsideDir, "secret.txt")

	linkOutside := filepath.Join(tmpDir, "link_outside")
	if err := os.Symlink(outsideDir, linkOutside); err != nil {
		t.Fatal(err)
	}

	linkChainOutside := filepath.Join(tmpDir, "link_chain_outside")
	if err := os.Symlink("link_outside", linkChainOutside); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		root    string
		target  string
		wantErr bool
	}{
		{
			name:    "valid absolute path",
			root:    tmpDir,
			target:  safePath,
			wantErr: false,
		},
		{
			name:    "cleaned absolute path inside root",
			root:    tmpDir,
			target:  filepath.Join(tmpDir, "subdir", "..", "safe.txt"),
			wantErr: false,
		},
		{
			name:    "outside absolute path",
			root:    tmpDir,
			target:  outsidePath,
			wantErr: true,
		},
		{
			name:    "missing nested path inside root",
			root:    tmpDir,
			target:  filepath.Join(tmpDir, "missing", "nested", "safe.txt"),
			wantErr: false,
		},
		{
			name:    "missing leaf under inside symlink",
			root:    tmpDir,
			target:  filepath.Join(linkInside, "missing.txt"),
			wantErr: false,
		},
		{
			name:    "missing leaf under outside symlink",
			root:    tmpDir,
			target:  filepath.Join(linkOutside, "missing.txt"),
			wantErr: true,
		},
		{
			name:    "symlink chain escape",
			root:    tmpDir,
			target:  filepath.Join(linkChainOutside, "missing.txt"),
			wantErr: true,
		},
		{
			name:    "relative path input (error)",
			root:    tmpDir,
			target:  "safe.txt",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ConfineAbsPath(tt.root, tt.target)
			if (err != nil) != tt.wantErr {
				t.Errorf("ConfineAbsPath() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

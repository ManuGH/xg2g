// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestValidatePlaylistPath(t *testing.T) {
	baseDir := t.TempDir()

	tests := []struct {
		name       string
		input      string
		wantErr    bool
		wantSuffix string
	}{
		{
			name:       "valid m3u",
			input:      "playlist.m3u",
			wantSuffix: "playlist.m3u",
		},
		{
			name:       "valid m3u8 in subdir",
			input:      "sub/playlist.m3u8",
			wantSuffix: filepath.Join("sub", "playlist.m3u8"),
		},
		{
			name:    "empty",
			input:   "",
			wantErr: true,
		},
		{
			name:    "absolute path",
			input:   "/etc/passwd",
			wantErr: true,
		},
		{
			name:    "traversal",
			input:   "../secret.m3u",
			wantErr: true,
		},
		{
			name:    "invalid extension",
			input:   "playlist.txt",
			wantErr: true,
		},
		{
			name:    "backslash path",
			input:   `subdir\\playlist.m3u`,
			wantErr: true,
		},
		{
			name:       "mixed case extension",
			input:      "PLAYLIST.M3U",
			wantSuffix: "PLAYLIST.M3U",
		},
		{
			name:    "dot path",
			input:   ".",
			wantErr: true,
		},
		{
			name:    "escaped traversal",
			input:   "sub/../../escape.m3u",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidatePlaylistPath(baseDir, tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.HasPrefix(got, baseDir) {
				t.Fatalf("path %q does not start with baseDir %q", got, baseDir)
			}
			if tt.wantSuffix != "" && !strings.HasSuffix(got, filepath.FromSlash(tt.wantSuffix)) {
				t.Fatalf("path %q does not end with %q", got, tt.wantSuffix)
			}
		})
	}
}

func TestValidatePlaylistPath_SymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may be restricted on windows")
	}

	baseDir := t.TempDir()
	outsideDir := t.TempDir()

	linkPath := filepath.Join(baseDir, "escape")
	if err := os.Symlink(outsideDir, linkPath); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	_, err := ValidatePlaylistPath(baseDir, "escape/playlist.m3u")
	if err == nil {
		t.Fatal("expected symlink escape to be rejected")
	}
}

// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package recordings

import (
	"path/filepath"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
)

func TestPathMapper_ResolveLocal_Success(t *testing.T) {
	mappings := []config.RecordingPathMapping{
		{ReceiverRoot: "/media/net/movie", LocalRoot: "/mnt/nfs-recordings"},
	}
	pm := NewPathMapper(mappings)

	tests := []struct {
		name         string
		receiverPath string
		wantLocal    string
		wantOK       bool
	}{
		{
			name:         "simple file mapping",
			receiverPath: "/media/net/movie/test.ts",
			wantLocal:    filepath.Join("/mnt/nfs-recordings", "test.ts"),
			wantOK:       true,
		},
		{
			name:         "nested file mapping",
			receiverPath: "/media/net/movie/subdir/recording.ts",
			wantLocal:    filepath.Join("/mnt/nfs-recordings", "subdir", "recording.ts"),
			wantOK:       true,
		},
		{
			name:         "exact root match",
			receiverPath: "/media/net/movie",
			wantLocal:    "/mnt/nfs-recordings",
			wantOK:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotLocal, gotOK := pm.ResolveLocal(tt.receiverPath)
			if gotOK != tt.wantOK {
				t.Errorf("ResolveLocal() gotOK = %v, want %v", gotOK, tt.wantOK)
			}
			if gotLocal != tt.wantLocal {
				t.Errorf("ResolveLocal() gotLocal = %q, want %q", gotLocal, tt.wantLocal)
			}
		})
	}
}

func TestPathMapper_LongestPrefixWins(t *testing.T) {
	// Test collision: /media/hdd/movie vs /media/hdd/movie2
	mappings := []config.RecordingPathMapping{
		{ReceiverRoot: "/media/hdd/movie", LocalRoot: "/mnt/movie"},
		{ReceiverRoot: "/media/hdd/movie2", LocalRoot: "/mnt/movie2"},
	}
	pm := NewPathMapper(mappings)

	tests := []struct {
		name         string
		receiverPath string
		wantLocal    string
		wantOK       bool
	}{
		{
			name:         "movie2 path should match movie2 mapping",
			receiverPath: "/media/hdd/movie2/recording.ts",
			wantLocal:    filepath.Join("/mnt/movie2", "recording.ts"),
			wantOK:       true,
		},
		{
			name:         "movie path should match movie mapping",
			receiverPath: "/media/hdd/movie/recording.ts",
			wantLocal:    filepath.Join("/mnt/movie", "recording.ts"),
			wantOK:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotLocal, gotOK := pm.ResolveLocal(tt.receiverPath)
			if gotOK != tt.wantOK {
				t.Errorf("ResolveLocal() gotOK = %v, want %v", gotOK, tt.wantOK)
			}
			if gotLocal != tt.wantLocal {
				t.Errorf("ResolveLocal() gotLocal = %q, want %q", gotLocal, tt.wantLocal)
			}
		})
	}
}

func TestPathMapper_TraversalBlocked(t *testing.T) {
	mappings := []config.RecordingPathMapping{
		{ReceiverRoot: "/media/net/movie", LocalRoot: "/mnt/nfs-recordings"},
	}
	pm := NewPathMapper(mappings)

	tests := []struct {
		name         string
		receiverPath string
	}{
		{
			name:         "parent directory traversal",
			receiverPath: "/media/net/movie/../../../etc/passwd",
		},
		{
			name:         "traversal with dots",
			receiverPath: "/media/net/movie/../../sensitive",
		},
		{
			name:         "relative path with traversal",
			receiverPath: "/media/net/movie/../other/file.ts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, gotOK := pm.ResolveLocal(tt.receiverPath)
			if gotOK {
				t.Errorf("ResolveLocal() should block traversal for %q", tt.receiverPath)
			}
		})
	}
}

func TestPathMapper_NonAbsolutePathBlocked(t *testing.T) {
	mappings := []config.RecordingPathMapping{
		{ReceiverRoot: "/media/net/movie", LocalRoot: "/mnt/nfs-recordings"},
	}
	pm := NewPathMapper(mappings)

	tests := []struct {
		name         string
		receiverPath string
	}{
		{
			name:         "relative path",
			receiverPath: "media/net/movie/test.ts",
		},
		{
			name:         "current directory",
			receiverPath: "./media/net/movie/test.ts",
		},
		{
			name:         "empty path",
			receiverPath: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, gotOK := pm.ResolveLocal(tt.receiverPath)
			if gotOK {
				t.Errorf("ResolveLocal() should reject non-absolute path %q", tt.receiverPath)
			}
		})
	}
}

func TestPathMapper_PathNormalization(t *testing.T) {
	mappings := []config.RecordingPathMapping{
		{ReceiverRoot: "/media/net/movie", LocalRoot: "/mnt/nfs-recordings"},
	}
	pm := NewPathMapper(mappings)

	tests := []struct {
		name         string
		receiverPath string
		wantLocal    string
		wantOK       bool
	}{
		{
			name:         "double slashes normalized",
			receiverPath: "/media/net/movie//subdir//file.ts",
			wantLocal:    filepath.Join("/mnt/nfs-recordings", "subdir", "file.ts"),
			wantOK:       true,
		},
		{
			name:         "dot segments normalized",
			receiverPath: "/media/net/movie/./subdir/./file.ts",
			wantLocal:    filepath.Join("/mnt/nfs-recordings", "subdir", "file.ts"),
			wantOK:       true,
		},
		{
			name:         "complex normalization",
			receiverPath: "/media/net/movie/a/b/../c/file.ts",
			wantLocal:    filepath.Join("/mnt/nfs-recordings", "a", "c", "file.ts"),
			wantOK:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotLocal, gotOK := pm.ResolveLocal(tt.receiverPath)
			if gotOK != tt.wantOK {
				t.Errorf("ResolveLocal() gotOK = %v, want %v", gotOK, tt.wantOK)
			}
			if gotLocal != tt.wantLocal {
				t.Errorf("ResolveLocal() gotLocal = %q, want %q", gotLocal, tt.wantLocal)
			}
		})
	}
}

func TestPathMapper_NoMapping(t *testing.T) {
	mappings := []config.RecordingPathMapping{
		{ReceiverRoot: "/media/net/movie", LocalRoot: "/mnt/nfs-recordings"},
	}
	pm := NewPathMapper(mappings)

	tests := []struct {
		name         string
		receiverPath string
	}{
		{
			name:         "unmapped path",
			receiverPath: "/media/hdd/movie/recording.ts",
		},
		{
			name:         "different root",
			receiverPath: "/media/other/file.ts",
		},
		{
			name:         "root only",
			receiverPath: "/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, gotOK := pm.ResolveLocal(tt.receiverPath)
			if gotOK {
				t.Errorf("ResolveLocal() should return false for unmapped path %q", tt.receiverPath)
			}
		})
	}
}

func TestPathMapper_InvalidMappingsSkipped(t *testing.T) {
	mappings := []config.RecordingPathMapping{
		{ReceiverRoot: "relative/path", LocalRoot: "/mnt/valid"},        // Invalid: non-absolute receiver
		{ReceiverRoot: "/valid", LocalRoot: "relative/local"},           // Invalid: non-absolute local
		{ReceiverRoot: "/", LocalRoot: "/mnt/root"},                     // Invalid: root-only receiver
		{ReceiverRoot: "/valid/path", LocalRoot: "/"},                   // Invalid: root-only local
		{ReceiverRoot: "/media/net/movie", LocalRoot: "/mnt/recordings"}, // Valid
	}
	pm := NewPathMapper(mappings)

	// Should only have 1 valid mapping
	if len(pm.mappings) != 1 {
		t.Errorf("NewPathMapper() should filter invalid mappings, got %d mappings, want 1", len(pm.mappings))
	}

	// Valid mapping should work
	gotLocal, gotOK := pm.ResolveLocal("/media/net/movie/test.ts")
	if !gotOK {
		t.Error("ResolveLocal() should work for valid mapping")
	}
	wantLocal := filepath.Join("/mnt/recordings", "test.ts")
	if gotLocal != wantLocal {
		t.Errorf("ResolveLocal() = %q, want %q", gotLocal, wantLocal)
	}
}

func TestPathMapper_EmptyMappings(t *testing.T) {
	pm := NewPathMapper(nil)

	_, gotOK := pm.ResolveLocal("/media/net/movie/test.ts")
	if gotOK {
		t.Error("ResolveLocal() should return false when no mappings configured")
	}
}

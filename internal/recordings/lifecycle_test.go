// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package recordings

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeFileInfo implements os.FileInfo for testing
type fakeFileInfo struct {
	name    string
	size    int64
	modTime time.Time
}

func (f *fakeFileInfo) Name() string       { return f.name }
func (f *fakeFileInfo) Size() int64        { return f.size }
func (f *fakeFileInfo) Mode() os.FileMode  { return 0644 }
func (f *fakeFileInfo) ModTime() time.Time { return f.modTime }
func (f *fakeFileInfo) IsDir() bool        { return false }
func (f *fakeFileInfo) Sys() interface{}   { return nil }

func TestClassifyLibrary_HappyPath(t *testing.T) {
	cfg := ClassifierConfig{
		StableWindow: 30 * time.Second,
		MinSizeBytes: 1 * 1024 * 1024,
		AllowedExt:   []string{".ts", ".mp4", ".mkv"},
	}

	tests := []struct {
		name    string
		path    string
		modTime time.Time
		size    int64
		want    LifecycleState
	}{
		{
			name:    "stable + valid → finished",
			path:    "/media/hdd/movie.ts",
			modTime: time.Now().Add(-60 * time.Second),
			size:    50 * 1024 * 1024,
			want:    StateFinished,
		},
		{
			name:    "unstable (< 30s) → recording",
			path:    "/media/hdd/movie.ts",
			modTime: time.Now().Add(-5 * time.Second),
			size:    50 * 1024 * 1024,
			want:    StateRecording,
		},
		{
			name:    "too small (< 1MB) → recording",
			path:    "/media/hdd/movie.ts",
			modTime: time.Now().Add(-60 * time.Second),
			size:    500 * 1024,
			want:    StateRecording,
		},
		{
			name:    "wrong extension → recording",
			path:    "/media/hdd/movie.avi",
			modTime: time.Now().Add(-60 * time.Second),
			size:    50 * 1024 * 1024,
			want:    StateRecording,
		},
		{
			name:    "valid .mkv → finished",
			path:    "/media/hdd/movie.mkv",
			modTime: time.Now().Add(-60 * time.Second),
			size:    50 * 1024 * 1024,
			want:    StateFinished,
		},
		{
			name:    "valid .mp4 → finished",
			path:    "/media/hdd/movie.mp4",
			modTime: time.Now().Add(-60 * time.Second),
			size:    50 * 1024 * 1024,
			want:    StateFinished,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &fakeFileInfo{
				name:    filepath.Base(tt.path),
				size:    tt.size,
				modTime: tt.modTime,
			}
			got := ClassifyLibrary(tt.path, info, cfg)
			if got != tt.want {
				t.Errorf("ClassifyLibrary() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClassifyLibrary_LockMarkers(t *testing.T) {
	cfg := ClassifierConfig{
		StableWindow: 30 * time.Second,
		MinSizeBytes: 1 * 1024 * 1024,
		AllowedExt:   []string{".ts", ".mp4", ".mkv"},
	}

	tests := []struct {
		name    string
		path    string
		modTime time.Time
		size    int64
		want    LifecycleState
	}{
		{
			name:    ".partial suffix → recording",
			path:    "/media/hdd/movie.ts.partial",
			modTime: time.Now().Add(-60 * time.Second),
			size:    50 * 1024 * 1024,
			want:    StateRecording,
		},
		{
			name:    ".lock suffix → recording",
			path:    "/media/hdd/movie.ts.lock",
			modTime: time.Now().Add(-60 * time.Second),
			size:    50 * 1024 * 1024,
			want:    StateRecording,
		},
		{
			name:    ".tmp suffix → recording",
			path:    "/media/hdd/movie.ts.tmp",
			modTime: time.Now().Add(-60 * time.Second),
			size:    50 * 1024 * 1024,
			want:    StateRecording,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &fakeFileInfo{
				name:    filepath.Base(tt.path),
				size:    tt.size,
				modTime: tt.modTime,
			}
			got := ClassifyLibrary(tt.path, info, cfg)
			if got != tt.want {
				t.Errorf("ClassifyLibrary() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClassifyLibrary_SiblingLock(t *testing.T) {
	// This test requires actual filesystem for os.Stat()
	cfg := ClassifierConfig{
		StableWindow: 30 * time.Second,
		MinSizeBytes: 1 * 1024 * 1024,
		AllowedExt:   []string{".ts"},
	}

	// Create temp file + sibling lock
	tmpDir := t.TempDir()
	moviePath := filepath.Join(tmpDir, "test.ts")
	lockPath := moviePath + ".lock"

	// Create both files
	if err := os.WriteFile(moviePath, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	// Modify time to be stable
	oldTime := time.Now().Add(-60 * time.Second)
	if err := os.Chtimes(moviePath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(moviePath)
	if err != nil {
		t.Fatal(err)
	}

	// Should detect sibling lock and return recording
	got := ClassifyLibrary(moviePath, info, cfg)
	if got != StateRecording {
		t.Errorf("ClassifyLibrary() with sibling lock = %v, want %v", got, StateRecording)
	}

	// Remove lock file
	if err := os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}

	// Now should be finished (but file too small - fix test)
	// Create larger file
	largeData := make([]byte, 2*1024*1024) // 2MB
	if err := os.WriteFile(moviePath, largeData, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(moviePath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	info, err = os.Stat(moviePath)
	if err != nil {
		t.Fatal(err)
	}

	got = ClassifyLibrary(moviePath, info, cfg)
	if got != StateFinished {
		t.Errorf("ClassifyLibrary() without lock = %v, want %v", got, StateFinished)
	}
}

func TestClassifyLibrary_EdgeCases(t *testing.T) {
	cfg := ClassifierConfig{
		StableWindow: 30 * time.Second,
		MinSizeBytes: 1 * 1024 * 1024,
		AllowedExt:   []string{".ts", ".mp4", ".mkv"},
	}

	tests := []struct {
		name    string
		path    string
		modTime time.Time
		size    int64
		want    LifecycleState
	}{
		{
			name:    "exactly 30s old → finished",
			path:    "/media/hdd/movie.ts",
			modTime: time.Now().Add(-30 * time.Second),
			size:    50 * 1024 * 1024,
			want:    StateFinished,
		},
		{
			name:    "exactly 1MB → finished",
			path:    "/media/hdd/movie.ts",
			modTime: time.Now().Add(-60 * time.Second),
			size:    1 * 1024 * 1024,
			want:    StateFinished,
		},
		{
			name:    "uppercase extension → finished",
			path:    "/media/hdd/movie.TS",
			modTime: time.Now().Add(-60 * time.Second),
			size:    50 * 1024 * 1024,
			want:    StateFinished,
		},
		{
			name:    "mixed case .MkV → finished",
			path:    "/media/hdd/movie.MkV",
			modTime: time.Now().Add(-60 * time.Second),
			size:    50 * 1024 * 1024,
			want:    StateFinished,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &fakeFileInfo{
				name:    filepath.Base(tt.path),
				size:    tt.size,
				modTime: tt.modTime,
			}
			got := ClassifyLibrary(tt.path, info, cfg)
			if got != tt.want {
				t.Errorf("ClassifyLibrary() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"errors"
	"os"
	"testing"
	"time"
)

type dummyFileInfo struct {
	isDir bool
}

func (d dummyFileInfo) Name() string       { return "dummy" }
func (d dummyFileInfo) Size() int64        { return 100 }
func (d dummyFileInfo) Mode() os.FileMode  { return 0o755 }
func (d dummyFileInfo) ModTime() time.Time { return time.Now() }
func (d dummyFileInfo) IsDir() bool        { return d.isDir }
func (d dummyFileInfo) Sys() any           { return nil }

func TestResolveMediaCoreBinWithLookups(t *testing.T) {
	t.Run("explicit env var takes precedence", func(t *testing.T) {
		got := resolveMediaCoreBinWithLookups(
			func(k string) (string, bool) {
				if k == "XG2G_MEDIA_CORE_BIN" {
					return "/custom/bin/xg2g-media-core", true
				}
				return "", false
			},
			func(string) (os.FileInfo, error) {
				return dummyFileInfo{isDir: false}, nil
			},
			func(string) (string, error) {
				return "/usr/bin/xg2g-media-core", nil
			},
		)
		if want := "/custom/bin/xg2g-media-core"; got != want {
			t.Fatalf("want %s, got %s", want, got)
		}
	})

	t.Run("default container path when env empty", func(t *testing.T) {
		got := resolveMediaCoreBinWithLookups(
			func(string) (string, bool) { return "", false },
			func(path string) (os.FileInfo, error) {
				if path == DefaultMediaCoreBin {
					return dummyFileInfo{isDir: false}, nil
				}
				return nil, os.ErrNotExist
			},
			func(string) (string, error) {
				return "/usr/bin/xg2g-media-core", nil
			},
		)
		if want := DefaultMediaCoreBin; got != want {
			t.Fatalf("want %s, got %s", want, got)
		}
	})

	t.Run("lookpath when default not found", func(t *testing.T) {
		got := resolveMediaCoreBinWithLookups(
			func(string) (string, bool) { return "", false },
			func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
			func(name string) (string, error) {
				if name == "xg2g-media-core" {
					return "/usr/bin/xg2g-media-core", nil
				}
				return "", errors.New("not found")
			},
		)
		if want := "/usr/bin/xg2g-media-core"; got != want {
			t.Fatalf("want %s, got %s", want, got)
		}
	})

	t.Run("empty when nothing matches", func(t *testing.T) {
		got := resolveMediaCoreBinWithLookups(
			func(string) (string, bool) { return "", false },
			func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
			func(string) (string, error) { return "", errors.New("not found") },
		)
		if got != "" {
			t.Fatalf("want empty string, got %s", got)
		}
	})
}

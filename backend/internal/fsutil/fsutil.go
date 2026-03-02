// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package fsutil

import (
	"github.com/ManuGH/xg2g/internal/platform/fs"
)

// Deprecated: Use internal/platform/fs.ConfineRelPath instead.
func ConfineRelPath(root, relTarget string) (string, error) {
	return fs.ConfineRelPath(root, relTarget)
}

// Deprecated: Use internal/platform/fs.ConfineAbsPath instead.
func ConfineAbsPath(rootAbs, targetAbs string) (string, error) {
	return fs.ConfineAbsPath(rootAbs, targetAbs)
}

// Deprecated: Use internal/platform/fs.IsRegularFile instead.
func IsRegularFile(path string) error {
	return fs.IsRegularFile(path)
}

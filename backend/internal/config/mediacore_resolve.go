// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"os"
	"os/exec"
	"strings"
)

// DefaultMediaCoreBin is the standard container install path for xg2g-media-core.
const DefaultMediaCoreBin = "/usr/local/bin/xg2g-media-core"

// ResolveMediaCoreBin returns an effective media-core binary path based on environment and system paths.
//
// Resolution order:
// 1) Explicit XG2G_MEDIA_CORE_BIN environment variable (if non-empty)
// 2) DefaultMediaCoreBin (/usr/local/bin/xg2g-media-core) if the binary exists and is not a directory
// 3) LookPath("xg2g-media-core") in $PATH
// 4) Empty string if not found
func ResolveMediaCoreBin() string {
	return resolveMediaCoreBinWithLookups(
		os.LookupEnv,
		os.Stat,
		exec.LookPath,
	)
}

func resolveMediaCoreBinWithLookups(
	lookupEnv func(string) (string, bool),
	stat func(string) (os.FileInfo, error),
	lookPath func(string) (string, error),
) string {
	if lookupEnv != nil {
		if val, ok := lookupEnv("XG2G_MEDIA_CORE_BIN"); ok {
			val = strings.TrimSpace(val)
			if val != "" {
				return val
			}
		}
	}

	if stat != nil {
		if fi, err := stat(DefaultMediaCoreBin); err == nil && fi != nil && !fi.IsDir() {
			return DefaultMediaCoreBin
		}
	}

	if lookPath != nil {
		if p, err := lookPath("xg2g-media-core"); err == nil && p != "" {
			return p
		}
	}

	return ""
}

// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import "strings"

// ResolveDataDirFromEnv resolves the data directory from supported environment keys.
func ResolveDataDirFromEnv() string {
	if v := strings.TrimSpace(ParseString("XG2G_DATA_DIR", "")); v != "" {
		return v
	}
	if v := strings.TrimSpace(ParseString("XG2G_DATA", "")); v != "" {
		return v
	}
	return ""
}

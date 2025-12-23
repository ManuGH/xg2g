// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package proxy

import "github.com/ManuGH/xg2g/internal/core/pathutil"

func sanitizeServiceRef(ref string) string {
	return pathutil.SanitizeServiceRef(ref)
}

func secureJoin(root, userPath string) (string, error) {
	return pathutil.SecureJoin(root, userPath)
}

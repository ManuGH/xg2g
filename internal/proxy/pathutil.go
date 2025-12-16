// SPDX-License-Identifier: MIT

package proxy

import "github.com/ManuGH/xg2g/internal/core/pathutil"

func sanitizeServiceRef(ref string) string {
	return pathutil.SanitizeServiceRef(ref)
}

func secureJoin(root, userPath string) (string, error) {
	return pathutil.SecureJoin(root, userPath)
}

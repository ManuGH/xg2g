// SPDX-License-Identifier: MIT

package jobs

import (
	"crypto/sha256"
	"encoding/hex"
)

// makeStableIDFromSRef creates a deterministic, collision-resistant tvg-id from a service reference
// Using a hash ensures the ID is stable even if the channel name changes and avoids issues
// with special characters in the sRef.
func makeStableIDFromSRef(sref string) string {
	sum := sha256.Sum256([]byte(sref))
	return "sref-" + hex.EncodeToString(sum[:])
}

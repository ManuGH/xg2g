// SPDX-License-Identifier: MIT

package jobs

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
)

// makeStableIDFromSRef creates a deterministic, collision-resistant tvg-id from a service reference
// Using a hash ensures the ID is stable even if the channel name changes and avoids issues
// with special characters in the sRef.
//
// Deprecated: Use makeTvgID(name, sref) instead for human-readable IDs.
// This function is kept for backwards compatibility and can be re-enabled via XG2G_USE_HASH_TVGID=true
func makeStableIDFromSRef(sref string) string {
	sum := sha256.Sum256([]byte(sref))
	return "sref-" + hex.EncodeToString(sum[:])
}

// makeTvgID creates a tvg-id for a channel, choosing between human-readable or hash-based format.
// By default, it creates human-readable IDs like "das-erste-hd-3fa92b".
// Set XG2G_USE_HASH_TVGID=true to use legacy hash-based IDs like "sref-a3f5d8c1...".
//
// Human-readable format:
//   - Easier to debug and understand in logs/playlists
//   - Better user experience in Plex/Jellyfin EPG mapping
//   - Still stable and collision-resistant via hash suffix
//
// Hash-based format (legacy):
//   - Fully opaque SHA256 hash
//   - Maximum stability if channel names change frequently
//   - Use if you have custom tooling that depends on hash format
func makeTvgID(name, sref string) string {
	// Allow users to opt-in to legacy hash-based IDs
	if os.Getenv("XG2G_USE_HASH_TVGID") == "true" {
		return makeStableIDFromSRef(sref)
	}

	// Default: human-readable IDs (new in v2.0)
	return makeHumanReadableTvgID(name, sref)
}

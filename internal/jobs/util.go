// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package jobs

import (
	"crypto/sha256"
	"encoding/hex"
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
// Set useHash=true to use legacy hash-based IDs like "sref-a3f5d8c1...".
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
func makeTvgID(name, sref string, useHash bool) string {
	if useHash {
		return makeStableIDFromSRef(sref)
	}

	// Default: human-readable IDs (new in v2.0)
	return makeHumanReadableTvgID(name, sref)
}

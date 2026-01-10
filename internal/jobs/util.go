// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package jobs

// makeTvgID creates a human-readable tvg-id for a channel.
// Examples: "das-erste-hd-3fa92b"
//
// Human-readable format:
//   - Easier to debug and understand in logs/playlists
//   - Better user experience in Plex/Jellyfin EPG mapping
//   - Still stable and collision-resistant via hash suffix

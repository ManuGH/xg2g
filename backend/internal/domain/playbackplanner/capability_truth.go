// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package playbackplanner

import "github.com/ManuGH/xg2g/internal/domain/playbackcompat"

// ReasonBrowserCannotDecodeDolby explains vetoing copied Dolby audio for
// browser clients (they claim AC-3 support they do not have).
const ReasonBrowserCannotDecodeDolby = playbackcompat.ReasonBrowserCannotDecodeDolby

// VetoedCapability reports whether the server-side capability truth table
// overrides a client-claimed codec, and if so with which reason.
func VetoedCapability(kind, codec string, ce ClientEvidence) (string, bool) {
	return playbackcompat.VetoReason(kind, codec, playbackcompat.Claims{
		Family:          ce.Family,
		PreferredEngine: ce.PreferredEngine,
	})
}

// IsBrowserClient reports whether the evidence describes a browser-hosted
// player (MSE/hls.js or native WebKit HLS) as opposed to a native app player
// (e.g. ExoPlayer) that owns its own decoders.
func IsBrowserClient(ce ClientEvidence) bool {
	return playbackcompat.IsBrowserClient(playbackcompat.Claims{
		Family:          ce.Family,
		PreferredEngine: ce.PreferredEngine,
	})
}

// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package playbackplanner

import "strings"

// Client capability claims are statements, not truths: browser media APIs
// (canPlayType/isTypeSupported) systematically over-report codec support —
// WebKit answers "maybe" for ac-3 although neither Safari MSE nor hls.js can
// decode it, which shipped as silent ORF2/ZDF playback. This table is the
// server-side ground truth that vetoes such claims per client class. Every
// entry carries the reason string that the shadow-diff classifier uses to
// explain the resulting divergence from legacy, so the veto and its
// explanation cannot drift apart.
type capabilityVeto struct {
	// Kind is "audio" or "video".
	Kind string
	// Codecs lists the claimed codec spellings this veto covers (lowercase).
	Codecs []string
	// AppliesTo selects the client class whose claim is overridden.
	AppliesTo func(ClientEvidence) bool
	// Reason is a stable machine-readable identifier for logs and the
	// shadow-diff classifier.
	Reason string
}

var capabilityVetoes = []capabilityVeto{
	{
		Kind:      "audio",
		Codecs:    []string{"ac3", "ac-3", "eac3", "ec-3"},
		AppliesTo: IsBrowserClient,
		Reason:    ReasonBrowserCannotDecodeDolby,
	},
}

// ReasonBrowserCannotDecodeDolby explains vetoing copied Dolby audio for
// browser clients (they claim AC-3 support they do not have).
const ReasonBrowserCannotDecodeDolby = "browser_cannot_decode_copied_dolby_audio"

// VetoedCapability reports whether the server-side capability truth table
// overrides a client-claimed codec, and if so with which reason.
func VetoedCapability(kind, codec string, ce ClientEvidence) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(codec))
	if normalized == "" {
		return "", false
	}
	for _, veto := range capabilityVetoes {
		if veto.Kind != kind {
			continue
		}
		for _, vetoed := range veto.Codecs {
			if vetoed == normalized {
				if veto.AppliesTo(ce) {
					return veto.Reason, true
				}
				break
			}
		}
	}
	return "", false
}

// IsBrowserClient reports whether the evidence describes a browser-hosted
// player (MSE/hls.js or native WebKit HLS) as opposed to a native app player
// (e.g. ExoPlayer) that owns its own decoders.
func IsBrowserClient(ce ClientEvidence) bool {
	if strings.EqualFold(strings.TrimSpace(ce.PreferredEngine), "hlsjs") {
		return true
	}
	family := strings.ToLower(ce.Family)
	for _, marker := range []string{"safari", "chrom", "firefox", "webkit", "ios"} {
		if strings.Contains(family, marker) {
			return true
		}
	}
	return false
}

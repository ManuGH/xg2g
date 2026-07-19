// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Package playbackcompat verifies client capability claims before they enter a
// playback decision. Runtime probes are evidence, not authority: this package
// may narrow a claim, but it must never add a capability the client supplied.
package playbackcompat

import "strings"

const (
	// PolicyVersion changes whenever a compatibility rule changes semantics.
	PolicyVersion = "2026-07-19.1"

	// ReasonBrowserCannotDecodeDolby is kept stable because it is emitted in
	// planner/shadow diagnostics and may be consumed by operational tooling.
	ReasonBrowserCannotDecodeDolby = "browser_cannot_decode_copied_dolby_audio"
)

// Claims are the canonical capability statements received from a client (or
// from an explicit family fallback when a client omitted a field).
type Claims struct {
	Scope           string
	Family          string
	PreferredEngine string
	Containers      []string
	VideoCodecs     []string
	AudioCodecs     []string
}

// Adjustment records why a client claim was not accepted as effective truth.
type Adjustment struct {
	Kind   string
	Value  string
	Reason string
}

// Resolution preserves the claims and exposes the verified effective subset.
type Resolution struct {
	PolicyVersion string
	Raw           Claims
	Effective     Claims
	Adjustments   []Adjustment
}

type rule struct {
	Kind    string
	Codecs  map[string]struct{}
	Applies func(Claims) bool
	Reason  string
}

var rules = []rule{
	{
		Kind:    "audio",
		Codecs:  stringSet("ac3", "ac-3", "eac3", "ec-3"),
		Applies: IsBrowserClient,
		Reason:  ReasonBrowserCannotDecodeDolby,
	},
}

var browserFamilies = stringSet(
	"safari",
	"safari_native",
	"ios_safari",
	"ios_safari_native",
	"firefox",
	"firefox_hlsjs",
	"android_tv_browser",
	"android_tv_hlsjs",
	"shield_browser",
	"chromium",
	"chrome",
	"edge",
	"chromium_hlsjs",
	"webkit",
)

// Resolve applies the compatibility policy without widening any supplied set.
func Resolve(raw Claims) Resolution {
	raw = canonicalClaims(raw)
	effective := cloneClaims(raw)
	adjustments := make([]Adjustment, 0)

	effective.Containers, adjustments = filter("container", raw.Containers, raw, adjustments)
	effective.VideoCodecs, adjustments = filter("video", raw.VideoCodecs, raw, adjustments)
	effective.AudioCodecs, adjustments = filter("audio", raw.AudioCodecs, raw, adjustments)

	return Resolution{
		PolicyVersion: PolicyVersion,
		Raw:           raw,
		Effective:     effective,
		Adjustments:   adjustments,
	}
}

// VetoReason reports the policy reason for rejecting one capability claim.
// It is also used as a migration adapter by the planner and shadow classifier.
func VetoReason(kind, codec string, claims Claims) (string, bool) {
	kind = token(kind)
	codec = token(codec)
	if codec == "" {
		return "", false
	}
	claims = canonicalClaims(claims)
	for _, candidate := range rules {
		if candidate.Kind != kind || !candidate.Applies(claims) {
			continue
		}
		if _, denied := candidate.Codecs[codec]; denied {
			return candidate.Reason, true
		}
	}
	return "", false
}

// IsBrowserClient distinguishes browser-hosted playback engines from native
// app players that own their decoder stack.
func IsBrowserClient(claims Claims) bool {
	if token(claims.PreferredEngine) == "hlsjs" {
		return true
	}
	_, browser := browserFamilies[token(claims.Family)]
	return browser
}

func filter(kind string, values []string, claims Claims, adjustments []Adjustment) ([]string, []Adjustment) {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if reason, denied := VetoReason(kind, value, claims); denied {
			adjustments = append(adjustments, Adjustment{Kind: kind, Value: value, Reason: reason})
			continue
		}
		out = append(out, value)
	}
	return out, adjustments
}

func canonicalClaims(in Claims) Claims {
	out := in
	out.Scope = token(out.Scope)
	out.Family = token(out.Family)
	out.PreferredEngine = token(out.PreferredEngine)
	out.Containers = canonicalSet(out.Containers)
	out.VideoCodecs = canonicalSet(out.VideoCodecs)
	out.AudioCodecs = canonicalSet(out.AudioCodecs)
	return out
}

func cloneClaims(in Claims) Claims {
	out := in
	out.Containers = append([]string(nil), in.Containers...)
	out.VideoCodecs = append([]string(nil), in.VideoCodecs...)
	out.AudioCodecs = append([]string(nil), in.AudioCodecs...)
	return out
}

func canonicalSet(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = token(value)
		if value == "" {
			continue
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func stringSet(values ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[token(value)] = struct{}{}
	}
	return out
}

func token(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

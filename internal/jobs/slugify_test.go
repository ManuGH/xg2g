// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package jobs

import (
	"testing"
)

func TestSlugify(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple lowercase",
			input:    "Das Erste",
			expected: "das-erste",
		},
		{
			name:     "with HD suffix",
			input:    "Das Erste HD",
			expected: "das-erste-hd",
		},
		{
			name:     "german umlauts",
			input:    "RTL Größer & Schöner",
			expected: "rtl-groesser-schoener",
		},
		{
			name:     "special characters",
			input:    "Sky Sport 1 (HD)",
			expected: "sky-sport-1-hd",
		},
		{
			name:     "multiple spaces",
			input:    "ZDF    Info",
			expected: "zdf-info",
		},
		{
			name:     "leading/trailing spaces",
			input:    "  ProSieben  ",
			expected: "prosieben",
		},
		{
			name:     "french accents",
			input:    "France 2 Télévision",
			expected: "france-2-television",
		},
		{
			name:     "spanish characters",
			input:    "España TV Niños",
			expected: "espana-tv-ninos",
		},
		{
			name:     "numbers only",
			input:    "3sat",
			expected: "3sat",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "channel",
		},
		{
			name:     "only special chars",
			input:    "!!!###",
			expected: "channel",
		},
		{
			name:     "very long name",
			input:    "This Is A Very Very Very Long Channel Name That Should Be Truncated To Reasonable Length",
			expected: "this-is-a-very-very-very-long-channel-name-that-sh",
		},
		{
			name:     "dots and underscores",
			input:    "Channel.One_HD",
			expected: "channel-one-hd",
		},
		{
			name:     "plus signs",
			input:    "RTL+",
			expected: "rtl",
		},
		{
			name:     "ampersand",
			input:    "Rock & Pop",
			expected: "rock-pop",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := slugify(tt.input)
			if result != tt.expected {
				t.Errorf("slugify(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestMakeHumanReadableTvgID(t *testing.T) {
	tests := []struct {
		name         string
		channelName  string
		serviceRef   string
		wantPrefix   string // We check prefix because hash suffix varies
		wantSuffixRe string // Regex pattern for suffix
	}{
		{
			name:         "Das Erste HD",
			channelName:  "Das Erste HD",
			serviceRef:   "1:0:19:132F:3EF:1:C00000:0:0:0:",
			wantPrefix:   "das-erste-hd-",
			wantSuffixRe: `^[a-f0-9]{6}$`,
		},
		{
			name:         "ZDF",
			channelName:  "ZDF",
			serviceRef:   "1:0:19:1334:3EF:1:C00000:0:0:0:",
			wantPrefix:   "zdf-",
			wantSuffixRe: `^[a-f0-9]{6}$`,
		},
		{
			name:         "Sky Sport HD with special chars",
			channelName:  "Sky Sport 1 (HD)",
			serviceRef:   "1:0:19:83:6:85:C00000:0:0:0:",
			wantPrefix:   "sky-sport-1-hd-",
			wantSuffixRe: `^[a-f0-9]{6}$`,
		},
		{
			name:         "Channel with umlauts",
			channelName:  "Größer & Schöner",
			serviceRef:   "1:0:1:2775:3F8:1:C00000:0:0:0:",
			wantPrefix:   "groesser-schoener-",
			wantSuffixRe: `^[a-f0-9]{6}$`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := makeHumanReadableTvgID(tt.channelName, tt.serviceRef)

			// Check prefix
			if len(result) < len(tt.wantPrefix) || result[:len(tt.wantPrefix)] != tt.wantPrefix {
				t.Errorf("makeHumanReadableTvgID() prefix = %q, want prefix %q", result, tt.wantPrefix)
			}

			// Extract suffix (last 6 chars after last dash)
			parts := result[len(tt.wantPrefix):]
			if len(parts) != 6 {
				t.Errorf("makeHumanReadableTvgID() suffix length = %d, want 6", len(parts))
			}

			// Check suffix is hex
			for _, c := range parts {
				if (c < 'a' || c > 'f') && (c < '0' || c > '9') {
					t.Errorf("makeHumanReadableTvgID() suffix %q contains non-hex char %c", parts, c)
				}
			}
		})
	}
}

func TestMakeHumanReadableTvgID_Stability(t *testing.T) {
	// Same inputs should always produce same output
	name := "Das Erste HD"
	sref := "1:0:19:132F:3EF:1:C00000:0:0:0:"

	id1 := makeHumanReadableTvgID(name, sref)
	id2 := makeHumanReadableTvgID(name, sref)

	if id1 != id2 {
		t.Errorf("makeHumanReadableTvgID() not stable: %q != %q", id1, id2)
	}
}

func TestMakeHumanReadableTvgID_Uniqueness(t *testing.T) {
	// Different service refs should produce different IDs
	name := "Das Erste HD"
	sref1 := "1:0:19:132F:3EF:1:C00000:0:0:0:"
	sref2 := "1:0:19:1334:3EF:1:C00000:0:0:0:" // Different service ref

	id1 := makeHumanReadableTvgID(name, sref1)
	id2 := makeHumanReadableTvgID(name, sref2)

	if id1 == id2 {
		t.Errorf("makeHumanReadableTvgID() not unique for different srefs: %q == %q", id1, id2)
	}

	// Prefixes should be the same (same channel name)
	prefix1 := id1[:len(id1)-7] // Remove "-SUFFIX"
	prefix2 := id2[:len(id2)-7]

	if prefix1 != prefix2 {
		t.Errorf("makeHumanReadableTvgID() prefixes differ: %q != %q", prefix1, prefix2)
	}

	// Suffixes should differ (different service refs)
	suffix1 := id1[len(id1)-6:]
	suffix2 := id2[len(id2)-6:]

	if suffix1 == suffix2 {
		t.Errorf("makeHumanReadableTvgID() suffixes should differ for different srefs: %q == %q", suffix1, suffix2)
	}
}

// Benchmark slugify performance
func BenchmarkSlugify(b *testing.B) {
	testCases := []string{
		"Das Erste HD",
		"RTL Größer & Schöner",
		"Sky Sport 1 (HD)",
		"This Is A Very Very Very Long Channel Name That Should Be Truncated",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, tc := range testCases {
			_ = slugify(tc)
		}
	}
}

// Benchmark makeHumanReadableTvgID performance
func BenchmarkMakeHumanReadableTvgID(b *testing.B) {
	name := "Das Erste HD"
	sref := "1:0:19:132F:3EF:1:C00000:0:0:0:"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = makeHumanReadableTvgID(name, sref)
	}
}

// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.
package epg

import (
	"testing"
)

func TestFindBest(t *testing.T) {
	nameToID := map[string]string{
		"orf1":     "orf1.at",
		"orf2":     "orf2.at",
		"rtl":      "rtl.de",
		"pro7":     "pro7.de",
		"sat1":     "sat1.de",
		"zdf":      "zdf.de",
		"ard":      "ard.de",
		"3sat":     "3sat.de",
		"arte":     "arte.de",
		"servustv": "servus.at",
	}

	tests := []struct {
		name       string
		input      string
		maxDist    int
		expectedID string
		found      bool
	}{
		{
			name:       "exact_match",
			input:      "orf1",
			maxDist:    2,
			expectedID: "orf1.at",
			found:      true,
		},
		{
			name:       "fuzzy_match_distance_1",
			input:      "orf1x", // Distance 1 (insertion)
			maxDist:    2,
			expectedID: "orf1.at",
			found:      true,
		},
		{
			name:       "fuzzy_match_distance_2",
			input:      "orfy", // Distance 2 - could match orf1 or orf2
			maxDist:    2,
			expectedID: "", // Don't check exact ID, just that something is found
			found:      true,
		},
		{
			name:       "no_match_exceeds_distance",
			input:      "completely_different",
			maxDist:    2,
			expectedID: "",
			found:      false,
		},
		{
			name:       "empty_input",
			input:      "",
			maxDist:    1,
			expectedID: "",
			found:      false,
		},
		{
			name:       "case_insensitive_match",
			input:      "ORF1",
			maxDist:    0,
			expectedID: "orf1.at",
			found:      true,
		},
		{
			name:       "fuzzy_match_max_distance_0",
			input:      "orf1x",
			maxDist:    0, // No fuzzy matching allowed
			expectedID: "",
			found:      false,
		},
		{
			name:       "numeric_channel_match",
			input:      "3sat",
			maxDist:    1,
			expectedID: "3sat.de",
			found:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, found := FindBest(tt.input, nameToID, tt.maxDist)

			if found != tt.found {
				t.Errorf("FindBest() found = %v, want %v", found, tt.found)
			}

			if found && tt.found && tt.expectedID != "" && id != tt.expectedID {
				t.Errorf("FindBest() id = %v, want %v", id, tt.expectedID)
			}
		})
	}
}

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		name     string
		a, b     string
		expected int
	}{
		{
			name:     "identical_strings",
			a:        "test",
			b:        "test",
			expected: 0,
		},
		{
			name:     "one_insertion",
			a:        "test",
			b:        "tests",
			expected: 1,
		},
		{
			name:     "one_deletion",
			a:        "tests",
			b:        "test",
			expected: 1,
		},
		{
			name:     "one_substitution",
			a:        "test",
			b:        "best",
			expected: 1,
		},
		{
			name:     "empty_strings",
			a:        "",
			b:        "",
			expected: 0,
		},
		{
			name:     "one_empty_string",
			a:        "test",
			b:        "",
			expected: 4,
		},
		{
			name:     "other_empty_string",
			a:        "",
			b:        "test",
			expected: 4,
		},
		{
			name:     "completely_different",
			a:        "abc",
			b:        "xyz",
			expected: 3,
		},
		{
			name:     "unicode_characters",
			a:        "äöü",
			b:        "abc",
			expected: 3,
		},
		{
			name:     "long_strings",
			a:        "abcdefghijklmnop",
			b:        "abcdefghijklmnxy",
			expected: 2, // Last two chars different
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := levenshtein(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestNameKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "lowercase_conversion",
			input:    "ORF1",
			expected: "orf1",
		},
		{
			name:     "already_lowercase",
			input:    "orf1",
			expected: "orf1",
		},
		{
			name:     "mixed_case",
			input:    "ProSieben",
			expected: "prosieben",
		},
		{
			name:     "empty_string",
			input:    "",
			expected: "",
		},
		{
			name:     "with_numbers",
			input:    "Pro7 MAXX",
			expected: "pro7 maxx",
		},
		{
			name:     "unicode_characters",
			input:    "Österreich",
			expected: "österreich",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NameKey(tt.input)
			if result != tt.expected {
				t.Errorf("NameKey(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func BenchmarkLevenshtein(b *testing.B) {
	const str1 = "abcdefghijklmnopqrstuvwxyz"
	const str2 = "abcdefghijklmnopqrstuvwxxy"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		levenshtein(str1, str2)
	}
}

func BenchmarkFindBest(b *testing.B) {
	nameToID := map[string]string{
		"orf1":     "orf1.at",
		"orf2":     "orf2.at",
		"rtl":      "rtl.de",
		"pro7":     "pro7.de",
		"sat1":     "sat1.de",
		"zdf":      "zdf.de",
		"ard":      "ard.de",
		"3sat":     "3sat.de",
		"arte":     "arte.de",
		"servustv": "servus.at",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		FindBest("orf1x", nameToID, 2)
	}
}

func FuzzLevenshtein(f *testing.F) {
	// Seed with some test cases
	f.Add("", "")
	f.Add("a", "b")
	f.Add("abc", "def")
	f.Add("test", "tests")

	f.Fuzz(func(t *testing.T, a, b string) {
		result := levenshtein(a, b)

		// Distance should never be negative
		if result < 0 {
			t.Errorf("Levenshtein distance cannot be negative: %d", result)
		}

		// Distance should be symmetric (except for implementation details)
		reverse := levenshtein(b, a)
		if result != reverse {
			t.Errorf("Levenshtein should be symmetric: levenshtein(%q, %q) = %d, but levenshtein(%q, %q) = %d",
				a, b, result, b, a, reverse)
		}

		// Distance from string to itself should be 0
		if a == b && result != 0 {
			t.Errorf("Distance from string to itself should be 0, got %d", result)
		}

		// Maximum distance is max(len(a), len(b))
		maxPossible := len([]rune(a))
		if l := len([]rune(b)); l > maxPossible {
			maxPossible = l
		}
		if result > maxPossible {
			t.Errorf("Distance %d exceeds maximum possible %d for strings %q and %q", result, maxPossible, a, b)
		}
	})
}

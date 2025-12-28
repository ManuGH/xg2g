// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import "testing"

func TestExtractPathFromServiceRef(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "standard enigma2 service ref",
			input:    "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/Film.ts",
			expected: "/media/hdd/movie/Film.ts",
		},
		{
			name:     "extended enigma2 service ref with more colons",
			input:    "1:0:0:0:0:0:0:0:0:0:0:0:0:/media/nfs-recordings/Recording.ts",
			expected: "/media/nfs-recordings/Recording.ts",
		},
		{
			name:     "path with spaces",
			input:    "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/Film Name.ts",
			expected: "/media/hdd/movie/Film Name.ts",
		},
		{
			name:     "no colon - return as-is",
			input:    "/media/hdd/movie/Film.ts",
			expected: "/media/hdd/movie/Film.ts",
		},
		{
			name:     "colon but no absolute path - return as-is (defensive)",
			input:    "1:0:0:0:0:0:0:0:0:0:relative/path.ts",
			expected: "1:0:0:0:0:0:0:0:0:0:relative/path.ts",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only colons",
			input:    ":::",
			expected: ":::",
		},
		{
			name:     "trailing colon no path",
			input:    "1:0:0:0:",
			expected: "1:0:0:0:",
		},
		{
			name:     "windows-style path (not supported, returned as-is)",
			input:    "1:0:0:0:0:0:0:0:0:0:C:\\Windows\\path.ts",
			expected: "1:0:0:0:0:0:0:0:0:0:C:\\Windows\\path.ts",
		},
		{
			name:     "path with special characters",
			input:    "1:0:0:0:0:0:0:0:0:0:/media/recordings/Film (2024) - Part 1.ts",
			expected: "/media/recordings/Film (2024) - Part 1.ts",
		},
		{
			name:     "path with unicode characters",
			input:    "1:0:0:0:0:0:0:0:0:0:/media/recordings/Фильм.ts",
			expected: "/media/recordings/Фильм.ts",
		},
		{
			name:     "path with url encoding (already decoded)",
			input:    "1:0:0:0:0:0:0:0:0:0:/media/recordings/Film%20Name.ts",
			expected: "/media/recordings/Film%20Name.ts",
		},
		{
			name:     "root path",
			input:    "1:0:0:0:0:0:0:0:0:0:/",
			expected: "/",
		},
		{
			name:     "deep nested path",
			input:    "1:0:0:0:0:0:0:0:0:0:/a/b/c/d/e/f/g/h/i/j/file.ts",
			expected: "/a/b/c/d/e/f/g/h/i/j/file.ts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractPathFromServiceRef(tt.input)
			if result != tt.expected {
				t.Errorf("extractPathFromServiceRef(%q) = %q, expected %q",
					tt.input, result, tt.expected)
			}
		})
	}
}

// Benchmark to ensure the extraction is fast
func BenchmarkExtractPathFromServiceRef(b *testing.B) {
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/Film.ts"
	for i := 0; i < b.N; i++ {
		_ = extractPathFromServiceRef(serviceRef)
	}
}

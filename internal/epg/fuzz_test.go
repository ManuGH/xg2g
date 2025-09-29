// SPDX-License-Identifier: MIT
package epg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func FuzzParseChannelName(f *testing.F) {
	// Seed with common channel names and edge cases
	testCases := []string{
		"ORF1 HD",
		"ORF 2",
		"RTL",
		"Pro7 MAXX",
		"Sat.1 Gold",
		"",
		"A",
		strings.Repeat("X", 1000),
		"Channel with Ümlauts äöü",
		"Channel_with-special.chars!@#$%",
		"123456789",
		"Ch@nnel (HD) [DE]",
	}

	for _, tc := range testCases {
		f.Add(tc)
	}

	f.Fuzz(func(t *testing.T, channelName string) {
		// Test that makeStableID doesn't panic and produces valid IDs
		id := makeStableID(channelName)

		// Basic validation: should never return empty ID
		// Empty or whitespace-only inputs should return "unknown"
		if len(id) == 0 {
			t.Errorf("Channel name %q produced empty ID, expected non-empty", channelName)
		}

		// ID should be safe for XML attributes
		if strings.Contains(id, " ") || strings.Contains(id, "'") || strings.Contains(id, "\"") {
			t.Errorf("ID %q contains unsafe characters for XML", id)
		}

		// Should not contain XML-unsafe characters
		// Note: makeStableID only replaces spaces, dots, and hyphens, other chars are preserved
		for _, forbidden := range []string{" ", "'", "\"", "<", ">", "&"} {
			if strings.Contains(id, forbidden) {
				t.Errorf("ID %q contains XML-unsafe character %q", id, forbidden)
			}
		}
	})
}

func FuzzXMLTVGeneration(f *testing.F) {
	// Seed with various channel configurations
	f.Add("Test Channel", "test.id", "Test Group")
	f.Add("", "", "")
	f.Add("Ümlauts äöü", "special.id", "Special")

	f.Fuzz(func(t *testing.T, name, id, group string) {
		channels := []Channel{
			{
				ID:          id,
				DisplayName: []string{name},
			},
		}

		// Create temp directory for test
		tmpDir := t.TempDir()
		xmlPath := filepath.Join(tmpDir, "test.xml")

		// Test XMLTV generation doesn't panic
		err := WriteXMLTV(channels, xmlPath)
		if err != nil {
			t.Logf("XMLTV generation failed for name=%q, id=%q: %v", name, id, err)
			return // Don't fail on expected errors with invalid input
		}

		// Verify file was created
		if _, err := os.Stat(xmlPath); err != nil {
			t.Errorf("XMLTV file not created: %v", err)
		}
	})
}

// makeStableID creates a stable identifier from a channel name
// This mirrors the implementation from internal/jobs/refresh.go for testing
func makeStableID(name string) string {
	// Normalize: lowercase, replace spaces/special chars with underscores
	id := strings.ToLower(name)
	id = strings.ReplaceAll(id, " ", "_")
	id = strings.ReplaceAll(id, ".", "_")
	id = strings.ReplaceAll(id, "-", "_")

	// Remove consecutive underscores
	for strings.Contains(id, "__") {
		id = strings.ReplaceAll(id, "__", "_")
	}

	// Trim leading/trailing underscores
	id = strings.Trim(id, "_")

	if id == "" {
		return "unknown"
	}
	return id
}

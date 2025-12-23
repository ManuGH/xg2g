// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.
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

		// Basic validation: empty input or only special chars should return empty ID
		// Non-empty meaningful input should produce non-empty ID
		trimmed := strings.TrimSpace(channelName)
		if trimmed != "" && len(id) == 0 {
			// Check if input contains any alphanumeric or valid characters
			hasValid := false
			for _, r := range channelName {
				if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r > 127 {
					hasValid = true
					break
				}
			}
			if hasValid {
				t.Errorf("Channel name %q with valid characters produced empty ID", channelName)
			}
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

	f.Fuzz(func(t *testing.T, name, id, _ string) {
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
		err := WriteXMLTV(GenerateXMLTV(channels, nil), xmlPath)
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

// makeStableID is now implemented in id.go

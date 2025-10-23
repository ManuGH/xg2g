// SPDX-License-Identifier: MIT
package epg

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"testing"
)

func TestXMLTVParsingRoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		channels []Channel
		wantErr  bool
	}{
		{
			name: "single_channel_basic",
			channels: []Channel{
				{
					ID:          "orf1.at",
					DisplayName: []string{"ORF1 HD"},
				},
			},
		},
		{
			name: "multiple_channels_with_icons",
			channels: []Channel{
				{
					ID:          "orf1.at",
					DisplayName: []string{"ORF1 HD"},
					Icon:        &Icon{Src: "http://example.com/orf1.png"},
				},
				{
					ID:          "orf2.at",
					DisplayName: []string{"ORF 2"},
					Icon:        &Icon{Src: "http://example.com/orf2.png"},
				},
			},
		},
		{
			name: "channel_with_multiple_names",
			channels: []Channel{
				{
					ID:          "rtl.de",
					DisplayName: []string{"RTL", "RTL Television"},
				},
			},
		},
		{
			name: "special_characters_in_names",
			channels: []Channel{
				{
					ID:          "pro7.de",
					DisplayName: []string{"Pro7 M&AXX", "ProSieben MAXX"},
				},
			},
		},
		{
			name:     "empty_channel_list",
			channels: []Channel{},
		},
		{
			name: "channel_with_empty_display_name",
			channels: []Channel{
				{
					ID:          "test.id",
					DisplayName: []string{},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory
			tmpDir := t.TempDir()
			xmlPath := filepath.Join(tmpDir, "test.xml")

			// Generate XMLTV
			err := WriteXMLTV(GenerateXMLTV(tt.channels, nil), xmlPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("WriteXMLTV() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			// Verify file exists
			if _, err := os.Stat(xmlPath); err != nil {
				t.Fatalf("XMLTV file not created: %v", err)
			}

			// Parse back and verify structure
			parsed, err := parseXMLTVFile(xmlPath)
			if err != nil {
				t.Fatalf("Failed to parse generated XMLTV: %v", err)
			}

			// Verify channel count
			if len(parsed.Channels) != len(tt.channels) {
				t.Errorf("Channel count mismatch: got %d, want %d", len(parsed.Channels), len(tt.channels))
			}

			// Verify channel IDs
			for i, ch := range tt.channels {
				if i < len(parsed.Channels) && parsed.Channels[i].ID != ch.ID {
					t.Errorf("Channel[%d] ID mismatch: got %q, want %q", i, parsed.Channels[i].ID, ch.ID)
				}
			}
		})
	}
}

func TestXMLTVErrorCases(t *testing.T) {
	tests := []struct {
		name    string
		xmlPath string
		wantErr bool
	}{
		{
			name:    "invalid_directory",
			xmlPath: "/nonexistent/dir/test.xml",
			wantErr: true,
		},
		{
			name:    "readonly_directory",
			xmlPath: "/test.xml", // Root directory (should be readonly)
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channels := []Channel{{ID: "test.id", DisplayName: []string{"Test"}}}

			err := WriteXMLTV(GenerateXMLTV(channels, nil), tt.xmlPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("WriteXMLTV() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestChannelIDGeneration(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple_name",
			input:    "ORF1",
			expected: "orf1",
		},
		{
			name:     "name_with_spaces",
			input:    "ORF1 HD",
			expected: "orf1.hd",
		},
		{
			name:     "special_characters",
			input:    "Pro7 MAXX (HD)",
			expected: "pro7.maxx.hd",
		},
		{
			name:     "umlauts_and_special",
			input:    "Sat.1 Dö&küs",
			expected: "sat.1.döküs", // Umlauts preserved, & removed
		},
		{
			name:     "empty_string",
			input:    "",
			expected: "",
		},
		{
			name:     "only_special_chars",
			input:    "!@#$%",
			expected: "",
		},
		{
			name:     "consecutive_separators",
			input:    "A  -  B",
			expected: "a.b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := makeStableID(tt.input)
			if result != tt.expected {
				t.Errorf("makeStableID(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestXMLTVStructValidation(t *testing.T) {
	t.Run("xml_structure_validity", func(t *testing.T) {
		channels := []Channel{
			{
				ID:          "test.channel",
				DisplayName: []string{"Test Channel"},
				Icon:        &Icon{Src: "http://example.com/icon.png"},
			},
		}

		tmpDir := t.TempDir()
		xmlPath := filepath.Join(tmpDir, "structure_test.xml")

		err := WriteXMLTV(GenerateXMLTV(channels, nil), xmlPath)
		if err != nil {
			t.Fatalf("WriteXMLTV failed: %v", err)
		}

		// Verify XML structure by parsing with standard library
		// #nosec G304 -- xmlPath is derived from t.TempDir, ensuring a trusted location
		data, err := os.ReadFile(xmlPath)
		if err != nil {
			t.Fatalf("Failed to read XMLTV file: %v", err)
		}

		var tv TV
		if err := xml.Unmarshal(data, &tv); err != nil {
			t.Fatalf("Generated XMLTV is not valid XML: %v", err)
		}

		// Verify required attributes
		if tv.Generator != "xg2g" {
			t.Errorf("Missing or incorrect generator-info-name: got %q", tv.Generator)
		}
	})
}

// parseXMLTVFile is a helper to parse XMLTV files for testing
func parseXMLTVFile(path string) (*TV, error) {
	// #nosec G304 -- path is always provided by test fixtures within the repository
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var tv TV
	if err := xml.Unmarshal(data, &tv); err != nil {
		return nil, err
	}

	return &tv, nil
}

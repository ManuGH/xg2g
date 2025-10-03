// SPDX-License-Identifier: MIT
package epg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGoldenFiles(t *testing.T) {
	tests := []struct {
		name       string
		channels   []Channel
		goldenFile string
	}{
		{
			name: "multi_channel_with_icons",
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
				{
					ID:          "rtl.de",
					DisplayName: []string{"RTL Television"},
				},
			},
			goldenFile: "multi_channel.golden.xml",
		},
		{
			name:       "empty_channels",
			channels:   []Channel{},
			goldenFile: "empty.golden.xml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Generate XMLTV
			tmpDir := t.TempDir()
			outputPath := filepath.Join(tmpDir, "output.xml")

			err := WriteXMLTV(tt.channels, outputPath)
			if err != nil {
				t.Fatalf("WriteXMLTV failed: %v", err)
			}

			// Read generated content
			generated, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatalf("Failed to read generated file: %v", err)
			}

			// Read golden file
			goldenPath := filepath.Join("testdata", tt.goldenFile)
			expected, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("Failed to read golden file %s: %v", goldenPath, err)
			}

			// Normalize whitespace for comparison
			generatedStr := normalizeXML(string(generated))
			expectedStr := normalizeXML(string(expected))

			if generatedStr != expectedStr {
				t.Errorf("Generated XMLTV doesn't match golden file %s", tt.goldenFile)
				t.Logf("Expected:\n%s", expectedStr)
				t.Logf("Generated:\n%s", generatedStr)
			}
		})
	}
}

// normalizeXML removes extra whitespace and normalizes formatting for comparison
func normalizeXML(xml string) string {
	lines := strings.Split(xml, "\n")
	var normalized []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			normalized = append(normalized, trimmed)
		}
	}

	return strings.Join(normalized, "\n")
}

func TestXMLTVBenchmark(t *testing.T) {
	// Skip in short mode
	if testing.Short() {
		t.Skip("Skipping benchmark test in short mode")
	}

	// Generate large channel list for performance testing
	var channels []Channel
	for i := 0; i < 1000; i++ {
		channels = append(channels, Channel{
			ID:          makeStableID(fmt.Sprintf("channel-%d", i)),
			DisplayName: []string{fmt.Sprintf("Channel %d", i)},
		})
	}

	tmpDir := t.TempDir()
	xmlPath := filepath.Join(tmpDir, "benchmark.xml")

	// Time the generation
	start := time.Now()
	err := WriteXMLTV(channels, xmlPath)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("WriteXMLTV failed: %v", err)
	}

	t.Logf("Generated XMLTV for %d channels in %v", len(channels), duration)

	// Verify file size is reasonable
	info, err := os.Stat(xmlPath)
	if err != nil {
		t.Fatalf("Failed to stat generated file: %v", err)
	}

	t.Logf("Generated file size: %d bytes", info.Size())

	// Should complete in reasonable time (adjust threshold as needed)
	if duration > 5*time.Second {
		t.Errorf("XMLTV generation took too long: %v", duration)
	}
}

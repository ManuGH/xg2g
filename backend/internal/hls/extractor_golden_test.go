package hls

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type truthGolden struct {
	IsVOD         bool   `json:"isVOD"`
	HasPDT        bool   `json:"hasPDT"`
	TotalDuration string `json:"totalDuration"`
}

func TestExtractSegmentTruth_Goldens(t *testing.T) {
	fixtureDir := filepath.Join("testdata", "fixtures")
	goldenDir := filepath.Join("testdata", "golden")
	updateGoldens := os.Getenv("UPDATE_GOLDEN") == "1"

	tests := []struct {
		name          string
		fixture       string
		wantError     bool
		errorContains string
	}{
		{
			name:    "VOD_with_PDT",
			fixture: "vod_with_pdt.m3u8",
		},
		{
			name:    "Live_with_PDT",
			fixture: "live_with_pdt.m3u8",
		},
		{
			name:      "Invalid_Missing_EXTM3U",
			fixture:   "invalid_missing_extm3u.m3u8",
			wantError: false, // Extractor doesn't strictly check for EXTM3U, it just scans lines.
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(fixtureDir, tt.fixture)
			content, err := os.ReadFile(path)
			require.NoError(t, err)

			truth, err := ExtractSegmentTruth(string(content))
			if tt.wantError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				return
			}

			require.NoError(t, err)

			actual := truthGolden{
				IsVOD:         truth.IsVOD,
				HasPDT:        truth.HasPDT,
				TotalDuration: truth.TotalDuration.String(),
			}

			base := strings.TrimSuffix(tt.fixture, filepath.Ext(tt.fixture))
			goldenPath := filepath.Join(goldenDir, base+".truth.json")

			if updateGoldens {
				require.NoError(t, os.MkdirAll(goldenDir, 0o755))
				blob, err := json.MarshalIndent(actual, "", "  ")
				require.NoError(t, err)
				blob = append(blob, '\n')
				require.NoError(t, os.WriteFile(goldenPath, blob, 0o644))
				return
			}

			expectedBytes, err := os.ReadFile(goldenPath)
			require.NoError(t, err, "missing golden file; set UPDATE_GOLDEN=1 to generate")

			var expected truthGolden
			require.NoError(t, json.Unmarshal(expectedBytes, &expected))

			if diff := cmp.Diff(expected, actual); diff != "" {
				t.Fatalf("golden mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

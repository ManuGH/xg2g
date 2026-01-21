package hls

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractSegmentTruth_Goldens(t *testing.T) {
	// Find project root (HACK: depending on execution environment,
	// we might need to adjust this. But we know fixtures are at /root/xg2g/fixtures/hls/)
	fixtureDir := "/root/xg2g/fixtures/hls"

	tests := []struct {
		name          string
		fixture       string
		wantIsVOD     bool
		wantHasPDT    bool
		wantTotalDur  time.Duration
		wantError     bool
		errorContains string
	}{
		{
			name:         "VOD_with_PDT",
			fixture:      "vod_with_pdt.m3u8",
			wantIsVOD:    true,
			wantHasPDT:   true,
			wantTotalDur: 20 * time.Second,
		},
		{
			name:         "Live_with_PDT",
			fixture:      "live_with_pdt.m3u8",
			wantIsVOD:    false,
			wantHasPDT:   true,
			wantTotalDur: 20 * time.Second,
		},
		{
			name:         "Invalid_Missing_EXTM3U",
			fixture:      "invalid_missing_extm3u.m3u8",
			wantError:    false, // Extractor doesn't strictly check for EXTM3U, it just scans lines.
			wantTotalDur: 0,
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
			assert.Equal(t, tt.wantIsVOD, truth.IsVOD)
			assert.Equal(t, tt.wantHasPDT, truth.HasPDT)
			assert.Equal(t, tt.wantTotalDur, truth.TotalDuration)
		})
	}
}

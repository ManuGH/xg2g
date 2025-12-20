// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package proxy

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestParseFFmpegStats(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		want    *FFmpegStats
		wantNil bool
	}{
		{
			name: "Perfect line",
			line: "frame=  123 fps= 25 q=28.0 size=    1234kB time=00:00:12.34 bitrate= 800.0kbits/s speed=1.0x",
			want: &FFmpegStats{
				Frame:       123,
				FPS:         25.0,
				Time:        12*time.Second + 340*time.Millisecond,
				BitrateKBPS: 800.0,
				Speed:       1.0,
				Valid:       true,
			},
		},
		{
			name: "Different spacing",
			line: "frame=123 fps=25.5 time=00:01:00.00 bitrate=1000.5kbits/s speed=2.5x",
			want: &FFmpegStats{
				Frame:       123,
				FPS:         25.5,
				Time:        60 * time.Second,
				BitrateKBPS: 1000.5,
				Speed:       2.5,
				Valid:       true,
			},
		},
		{
			name: "Missing some fields",
			line: "frame=100 fps=0.0 time=00:00:05.00",
			want: &FFmpegStats{
				Frame:       100,
				FPS:         0.0,
				Time:        5 * time.Second,
				BitrateKBPS: 0.0,
				Speed:       0.0,
				Valid:       true,
			},
		},
		{
			name: "N/A values",
			line: "frame=123 fps=25 time=00:00:10.00 bitrate=N/A speed=N/A",
			want: &FFmpegStats{
				Frame:       123,
				FPS:         25.0,
				Time:        10 * time.Second,
				BitrateKBPS: 0.0,
				Speed:       0.0,
				Valid:       true,
			},
		},
		{
			name: "kb/s unit",
			line: "bitrate= 500kb/s",
			want: &FFmpegStats{
				BitrateKBPS: 500.0,
				Valid:       true,
			},
		},
		{
			name:    "Garbage line",
			line:    "Output #0, hls, to 'playlist.m3u8':",
			wantNil: true,
		},
		{
			name:    "Empty line",
			line:    "",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseFFmpegStats(tt.line)
			if tt.wantNil {
				assert.Nil(t, got)
			} else {
				assert.NotNil(t, got)
				assert.Equal(t, tt.want.Frame, got.Frame, "Frame mismatch")
				assert.Equal(t, tt.want.FPS, got.FPS, "FPS mismatch")
				assert.InDelta(t, tt.want.Time.Seconds(), got.Time.Seconds(), 0.01, "Time mismatch")
				assert.Equal(t, tt.want.BitrateKBPS, got.BitrateKBPS, "Bitrate mismatch")
				assert.Equal(t, tt.want.Speed, got.Speed, "Speed mismatch")
				assert.Equal(t, tt.want.Valid, got.Valid, "Valid mismatch")
			}
		})
	}
}

// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package proxy

import "testing"

func TestClassifyFFmpegError(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Connection refused (exact)",
			input:    "tcp://10.10.55.64:17999: Connection refused",
			expected: "stream_connect_reset",
		},
		{
			name:     "Connection refused (lowercase)",
			input:    "tcp://... connection refused",
			expected: "stream_connect_reset",
		},
		{
			name:     "Connection reset",
			input:    "recvmsg: Connection reset by peer",
			expected: "stream_connect_reset",
		},
		{
			name:     "Broken pipe",
			input:    "write: Broken pipe",
			expected: "stream_connect_reset",
		},
		{
			name:     "Broken pipe (write error)",
			input:    "av_interleaved_write_frame(): Broken pipe",
			expected: "stream_connect_reset", // Explicitly matching broad "broken pipe"
		},
		{
			name:     "Input/output error",
			input:    "av_interleaved_write_frame(): Input/output error",
			expected: "io_error",
		},
		{
			name:     "Generic info log",
			input:    "Opening 'http://...'",
			expected: "",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyFFmpegError(tt.input)
			if got != tt.expected {
				t.Errorf("ClassifyFFmpegError(%q) = %q; want %q", tt.input, got, tt.expected)
			}
		})
	}
}

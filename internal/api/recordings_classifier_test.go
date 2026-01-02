package api

import (
	"errors"
	"testing"
)

func TestClassifyFFmpegError(t *testing.T) {
	tests := []struct {
		name            string
		stderr          string
		segmentsWritten int
		expectedErr     error
	}{
		{
			name:            "Auth Failure (401)",
			stderr:          "Server returned 401 Unauthorized (authorization failed)",
			segmentsWritten: 0,
			expectedErr:     ErrSourceUnavailable,
		},
		{
			name:            "Missing Source (404)",
			stderr:          "Server returned 404 Not Found",
			segmentsWritten: 0,
			expectedErr:     ErrSourceUnavailable,
		},
		{
			name:            "Connection Refused",
			stderr:          "Connection refused",
			segmentsWritten: 0,
			expectedErr:     ErrSourceUnavailable,
		},
		{
			name:            "Probe Failed (No Streams)",
			stderr:          "[mpegts @ 0x...] Could not find codec parameters for stream 0 (Video: h264 ([27][0][0][0] / 0x001B), none): no streams\nConsider increasing the value for the 'analyzeduration' and 'probesize' options",
			segmentsWritten: 0,
			expectedErr:     ErrProbeFailed,
		},
		{
			name:            "Probe Failed (Invalid Data)",
			stderr:          "Invalid data found when processing input",
			segmentsWritten: 0,
			expectedErr:     ErrProbeFailed,
		},
		{
			name:            "Late Failure (Runtime Fatal)",
			stderr:          "write failed: No space left on device",
			segmentsWritten: 10, // Segments already written
			expectedErr:     ErrFFmpegFatal,
		},
		{
			name:            "Probe Failure Pattern but Late (Runtime Fatal)",
			stderr:          "Invalid data found (corruption mid-stream)",
			segmentsWritten: 5,
			expectedErr:     ErrFFmpegFatal,
		},
		{
			name:            "Unknown Error (Fatal)",
			stderr:          "Something unexpected happened",
			segmentsWritten: 0,
			expectedErr:     ErrFFmpegFatal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyFFmpegError(tt.stderr, tt.segmentsWritten)
			if !errors.Is(got, tt.expectedErr) {
				t.Errorf("classifyFFmpegError() = %v, want %v", got, tt.expectedErr)
			}
		})
	}
}

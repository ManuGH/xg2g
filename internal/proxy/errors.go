// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package proxy

import "strings"

// ClassifyFFmpegError determines the failure reason based on FFmpeg stderr output.
// It returns a specific reason string (e.g., "stream_connect_reset", "io_error")
// or empty string if no specific error is found.
// This allows deterministic mapping of raw log lines to metric labels.
// Precedence: connection-reset signals take priority over io_error when both appear.
func ClassifyFFmpegError(stderrLine string) string {
	s := strings.ToLower(stderrLine)

	// Check for connection refusal/reset (Race condition signature)
	if strings.Contains(s, "connection refused") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "broken pipe") {
		return "stream_connect_reset"
	}

	// Check for generic I/O errors (often receiver not ready/network reset)
	if strings.Contains(s, "input/output error") {
		return "io_error"
	}

	// Future: Add more classifications here (e.g. 403 Forbidden, 404 Not Found)
	// if strings.Contains(s, "404 not found") { ... }

	return ""
}

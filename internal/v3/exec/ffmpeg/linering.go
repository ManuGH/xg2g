// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"strings"
	"sync"
)

// LineRing is a thread-safe ring buffer for capturing the last N lines of log output.
type LineRing struct {
	mu    sync.RWMutex
	lines []string
	head  int
	size  int
}

// NewLineRing creates a LineRing with the specified capacity.
func NewLineRing(capacity int) *LineRing {
	if capacity < 1 {
		capacity = 50 // Default
	}
	return &LineRing{
		lines: make([]string, capacity),
		size:  capacity,
	}
}

// Write implements io.Writer to capture output. It assumes line-oriented input or
// breaks input into lines.
// NOTE: For simplicity, this naive implementation splits by newline.
// A robust implementation would handle partial writes, but for stderr logs this is often sufficient.
func (r *LineRing) Write(p []byte) (n int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Split input into lines
	s := string(p)
	parts := strings.Split(s, "\n")

	// Handle trailing newline common in log output: "foo\n" -> ["foo", ""]
	// We usually discard the last empty string if the input ends in newline.
	// But simply adding all non-empty lines is safer.
	for _, line := range parts {
		if line == "" {
			continue
		}
		r.lines[r.head] = line
		r.head = (r.head + 1) % r.size
	}

	return len(p), nil
}

// LastN returns the last N lines in chronological order.
func (r *LineRing) LastN(n int) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if n > r.size {
		n = r.size
	}

	// Start from tail: (head - n + size) % size?
	// Or just collect all populated lines and return last N.

	// Simpler loop: iterate from oldest to newest, append to slice, take suffix.
	// Ordering: The oldest line is at r.head (if wrapped) or 0.
	// Actually, r.head is the *next* write position.
	// So r.head-1 is the newest.

	// Let's reconstruct chronological order:
	// From (r.head) to (r.head - 1) wrapping around.

	// Simple approach:
	ordered := make([]string, 0, r.size)
	for i := 0; i < r.size; i++ {
		idx := (r.head + i) % r.size
		if r.lines[idx] != "" {
			ordered = append(ordered, r.lines[idx])
		}
	}

	if len(ordered) <= n {
		return ordered
	}
	return ordered[len(ordered)-n:]
}

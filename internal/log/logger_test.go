// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package log

import (
	"strings"
	"testing"
)

func TestStructuredBufferWriter_Framing(t *testing.T) {
	ClearRecentLogs()
	w := &structuredBufferWriter{}

	// 1. Split write: half line + rest\n
	line1Part1 := `{"time":"2026-01-01T00:00:00Z","level":"info","component":"audit","event":"test.split","message":"part1`
	line1Part2 := `_part2"}` + "\n"

	w.Write([]byte(line1Part1))
	if len(GetRecentLogs()) != 0 {
		t.Errorf("expected 0 logs after partial write, got %d", len(GetRecentLogs()))
	}

	w.Write([]byte(line1Part2))
	logs := GetRecentLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log after full write, got %d", len(logs))
	}
	if logs[0].Fields["event"] != "test.split" {
		t.Errorf("expected event test.split, got %v", logs[0].Fields["event"])
	}

	// 2. Multi-line burst
	line2 := `{"time":"2026-01-01T00:00:01Z","level":"info","component":"audit","event":"burst.1","message":"msg1"}` + "\n"
	line3 := `{"time":"2026-01-01T00:00:02Z","level":"info","event":"request.handled","message":"msg2"}` + "\n"

	w.Write([]byte(line2 + line3))
	logs = GetRecentLogs()
	if len(logs) != 3 {
		t.Fatalf("expected 3 logs total, got %d", len(logs))
	}
}

func TestStructuredBufferWriter_Bounds(t *testing.T) {
	ClearRecentLogs()
	w := &structuredBufferWriter{}

	// 1. MaxPartialBytes Overflow
	giantChunk := strings.Repeat("A", maxPartialBytes+1) // no newline
	w.Write([]byte(giantChunk))

	if w.partial.Len() != 0 {
		t.Error("partial buffer should have been reset after overflow")
	}
	metrics := GetBufferMetrics()
	if metrics.DroppedPartialOverflow == 0 {
		t.Error("expected DroppedPartialOverflow metric to be incremented")
	}

	// 2. MaxLineBytes Drop
	ClearRecentLogs()
	giantLine := `{"level":"info","component":"audit","event":"too.big","msg":"` + strings.Repeat("B", maxLineBytes) + `"}` + "\n"
	w.Write([]byte(giantLine))

	if len(GetRecentLogs()) != 0 {
		t.Error("giant line should have been dropped")
	}
	metrics = GetBufferMetrics()
	if metrics.DroppedTooLargeLines == 0 {
		t.Error("expected DroppedTooLargeLines metric to be incremented")
	}
}

func TestStructuredBufferWriter_RelevanceFilter(t *testing.T) {
	ClearRecentLogs()
	w := &structuredBufferWriter{}

	// 1. Relevant: Audit
	auditLine := `{"level":"info","component":"audit","event":"log.level_changed","message":"ok"}` + "\n"
	w.Write([]byte(auditLine))

	// 2. Relevant: Request Handled
	reqLine := `{"level":"info","event":"request.handled","message":"ok"}` + "\n"
	w.Write([]byte(reqLine))

	// 3. Irrelevant: Debug trace
	debugLine := `{"level":"debug","component":"sql","message":"select * from users"}` + "\n"
	w.Write([]byte(debugLine))

	logs := GetRecentLogs()
	if len(logs) != 2 {
		t.Errorf("expected 2 logs (audit + request), got %d", len(logs))
	}

	metrics := GetBufferMetrics()
	if metrics.DroppedIrrelevant == 0 {
		t.Error("expected DroppedIrrelevant metric to be incremented")
	}
}

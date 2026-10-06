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

func TestStructuredBufferWriter_Scrubbing(t *testing.T) {
	ClearRecentLogs()
	w := &structuredBufferWriter{}

	// Write an audit log containing a raw IPTV reference and external stream URL
	rawRef := "4097:0:1:0:0:0:0:0:0:0:http%3a//canary.invalid/SECRET-CANARY-1/live/token-xyz-987/channel_prime.ts:Canary Channel 1"
	auditLine := `{"time":"2026-01-01T00:00:00Z","level":"info","component":"audit","event":"channel.tune","message":"tuning raw ref: ` + rawRef + `","service_ref":"` + rawRef + `"}` + "\n"

	w.Write([]byte(auditLine))
	logs := GetRecentLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}

	// Verify message and fields are scrubbed
	if strings.Contains(logs[0].Message, "canary.invalid") || strings.Contains(logs[0].Message, "SECRET-CANARY-1") {
		t.Errorf("expected Message to be scrubbed, got: %s", logs[0].Message)
	}
	if !strings.Contains(logs[0].Message, "[REDACTED_IPTV_REF]") {
		t.Errorf("expected Message to contain [REDACTED_IPTV_REF], got: %s", logs[0].Message)
	}

	fieldRef, ok := logs[0].Fields["service_ref"].(string)
	if !ok || strings.Contains(fieldRef, "canary.invalid") {
		t.Errorf("expected service_ref field to be scrubbed, got: %v", logs[0].Fields["service_ref"])
	}
}

func TestScrubbingWriter(t *testing.T) {
	var buf strings.Builder
	sw := newScrubbingWriter(&buf)

	rawRef := "4097:0:1:0:0:0:0:0:0:0:http%3a//canary.invalid/SECRET-CANARY-1/live/token-xyz-987/channel_prime.ts:Canary Channel 1"
	input := `{"level":"info","ref":"` + rawRef + `","msg":"hello"}` + "\n"

	n, err := sw.Write([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != len(input) {
		t.Errorf("expected n=%d, got %d", len(input), n)
	}

	written := buf.String()
	if strings.Contains(written, "canary.invalid") || strings.Contains(written, "SECRET-CANARY-1") {
		t.Errorf("expected written output to be scrubbed, got: %s", written)
	}
	if !strings.Contains(written, "[REDACTED_IPTV_REF]") {
		t.Errorf("expected written output to contain [REDACTED_IPTV_REF], got: %s", written)
	}
}

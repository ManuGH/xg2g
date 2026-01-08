package v3

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestParseRecordingDurationSeconds(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		wantErr  bool
	}{
		{"01:02:03", 3723, false},
		{"12:34", 754, false},
		{"90 min", 5400, false},
		{"90 mins", 5400, false},
		{"90m", 5400, false},
		{" 90 m ", 5400, false},
		{"0", 0, true},
		{"", 0, true},
		{"invalid", 0, true},
		{"1:2", 62, false},
		{"0:0:1", 1, false},
		{"0:1", 1, false},
		// Killer cases from previous round
		{" : ", 0, true},
		{"12:", 0, true},
		{":34", 0, true},
		{"1:2:3:4", 0, true},
		{"xx:yy", 0, true},
		{"-1:20", 0, true},
		{"12 : 34", 754, false},
		{" 1 : 02 : 03 ", 3723, false},
		{"90 minutes", 5400, false},
		{"90 Min.", 5400, false},
		{"90M", 5400, false},
		{"-90 min", 0, true},
		{"90 xx", 0, true},
		// Range validation (Stopp-Schilder)
		{"1:60", 0, true},      // SS >= 60
		{"60:00", 3600, false}, // MM >= 60 is ok in MM:SS but not in HH:MM:SS
		{"1:60:00", 0, true},   // MM >= 60
		{"1:00:60", 0, true},   // SS >= 60
		// Overflow cases
		{fmt.Sprintf("%d:00:00", math.MaxInt64/3600+1), 0, true},
	}

	for _, tt := range tests {
		got, err := ParseRecordingDurationSeconds(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseRecordingDurationSeconds(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if err == nil && got != tt.expected {
			t.Errorf("ParseRecordingDurationSeconds(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func mustNotPanic(t *testing.T, input string, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("PANIC detected for input %q: %v", input, r)
		}
	}()
	fn()
}

func TestParseRecordingDurationSeconds_Invariants(t *testing.T) {
	// 1. Bound check: Never negative
	inputs := []string{"-1", "-12:34", "90 m", "-90 min", "1:-2:3"}
	for _, in := range inputs {
		got, err := ParseRecordingDurationSeconds(in)
		if err == nil && got < 0 {
			t.Errorf("Invariant violation: Negative result for %q: %d", in, got)
		}
	}

	// 2. Bound check: 0 is error
	got, err := ParseRecordingDurationSeconds("0")
	if err == nil || got != 0 {
		t.Errorf("Invariant violation: '0' should be (0, err), got (%d, %v)", got, err)
	}
}

func TestParseRecordingDurationSeconds_NeverPanic_Fuzz(t *testing.T) {
	// 1000 iteration fuzz-light
	for i := 0; i < 1000; i++ {
		s := ""
		// Construct various "risky" strings
		switch i % 3 {
		case 0:
			// Random printable characters
			for j := 0; j < 15; j++ {
				s += string(rune(32 + (i*j)%95))
			}
		case 1:
			// Random colon placements
			parts := make([]string, (i%5)+1)
			for j := range parts {
				parts[j] = fmt.Sprintf("%d", i*j)
			}
			s = strings.Join(parts, ":")
		case 2:
			// Random whitespace and suffixes
			s = fmt.Sprintf("  %d  min  ", i)
		}

		mustNotPanic(t, s, func() {
			_, _ = ParseRecordingDurationSeconds(s)
		})
	}
}

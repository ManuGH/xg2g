package ffmpeg

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestParseFPS(t *testing.T) {
	tests := []struct {
		input    string
		expected int
		hasError bool
	}{
		{"25/1", 25, false},
		{"30000/1001", 30, false},
		{"50/1", 50, false},
		{"60/1", 60, false},
		{"24000/1001", 24, false},
		{"30", 30, false},
		{"25", 25, false},
		{"0/0", 0, true},
		{"0/1", 0, false}, // Technically valid 0 fps
		{"1/0", 0, true},  // Division by zero
		{"abc", 0, true},
		{"", 0, true},
		{"25/1/2", 0, true},
		{"24.5", 0, true}, // ffmpeg usually outputs fractions, but robust check
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseFPS(tt.input)
			if tt.hasError {
				if err == nil {
					t.Errorf("parseFPS(%q) expected error, got %d", tt.input, got)
				}
			} else {
				if err != nil {
					t.Errorf("parseFPS(%q) unexpected error: %v", tt.input, err)
				}
				if got != tt.expected {
					t.Errorf("parseFPS(%q) = %d; want %d", tt.input, got, tt.expected)
				}
			}
		})
	}
}

func TestParseFPSProbeOutput(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantFPS   int
		wantBasis string
		wantErr   bool
	}{
		{
			name: "interlaced_field_rate_preferred",
			input: `r_frame_rate=50/1
avg_frame_rate=25/1
field_order=tt`,
			wantFPS:   50,
			wantBasis: "r_frame_rate_field_rate",
		},
		{
			name: "progressive_avg_preferred",
			input: `r_frame_rate=30000/1001
avg_frame_rate=24000/1001
field_order=progressive`,
			wantFPS:   24,
			wantBasis: "avg_frame_rate",
		},
		{
			name: "single_r_frame_rate",
			input: `r_frame_rate=25/1
field_order=unknown`,
			wantFPS:   25,
			wantBasis: "r_frame_rate",
		},
		{
			name: "invalid_values_error",
			input: `r_frame_rate=0/0
avg_frame_rate=abc
field_order=unknown`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotFPS, gotBasis, err := parseFPSProbeOutput(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got fps=%d basis=%s", gotFPS, gotBasis)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotFPS != tt.wantFPS {
				t.Fatalf("fps=%d want=%d", gotFPS, tt.wantFPS)
			}
			if gotBasis != tt.wantBasis {
				t.Fatalf("basis=%s want=%s", gotBasis, tt.wantBasis)
			}
		})
	}
}

func TestShouldRetryFPSProbe(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "deadline", err: context.DeadlineExceeded, want: true},
		{name: "canceled", err: context.Canceled, want: true},
		{name: "wrapped_deadline", err: fmt.Errorf("wrapped: %w", context.DeadlineExceeded), want: true},
		{name: "signal_killed", err: errors.New("ffprobe failed: signal: killed"), want: true},
		{name: "deadline_text", err: errors.New("context deadline exceeded while probing"), want: true},
		{name: "other", err: errors.New("exit status 1"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldRetryFPSProbe(tt.err)
			if got != tt.want {
				t.Fatalf("shouldRetryFPSProbe(%v) = %v; want %v", tt.err, got, tt.want)
			}
		})
	}
}

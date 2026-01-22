package ffmpeg

import (
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

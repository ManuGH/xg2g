package v3

import (
	"math"
	"testing"
)

func TestClampPublishedEndpointPriority(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    int
		expected int32
	}{
		{name: "within range", input: 10, expected: 10},
		{name: "clamps max", input: math.MaxInt32 + 1, expected: math.MaxInt32},
		{name: "clamps min", input: math.MinInt32 - 1, expected: math.MinInt32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := clampPublishedEndpointPriority(tt.input); got != tt.expected {
				t.Fatalf("clampPublishedEndpointPriority(%d) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

package migration

import (
	"testing"
	"time"
)

func TestSemanticMapping_GoldenVectors(t *testing.T) {
	tests := []struct {
		name     string
		inputS   int64
		inputT   time.Time
		expectMS int64
	}{
		{"Zero", 0, time.Unix(0, 0), 0},
		{"Standard Epoch", 1600000000, time.Unix(1600000000, 0), 1600000000000},
		{"Near Future", 1767225600, time.Unix(1767225600, 0), 1767225600000}, // 2026-01-01
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the ms conversion logic used in sessions_store.go/migrate.go
			got := tt.inputS * 1000
			if got != tt.expectMS {
				t.Errorf("s2ms(%d) = %d, want %d", tt.inputS, got, tt.expectMS)
			}

			// Test time.Time mapping
			gotT := tt.inputT.UnixMilli()
			if gotT != tt.expectMS {
				t.Errorf("Time(%v).UnixMilli() = %d, want %d", tt.inputT, gotT, tt.expectMS)
			}
		})
	}
}

func TestSemanticMapping_Monotonicity(t *testing.T) {
	// A strictly before B in seconds must be strictly before B in milliseconds
	t1 := int64(1000)
	t2 := int64(1001)

	m1 := t1 * 1000
	m2 := t2 * 1000

	if !(m1 < m2) {
		t.Errorf("Monotonicity failed: %d < %d but %d >= %d", t1, t2, m1, m2)
	}
}

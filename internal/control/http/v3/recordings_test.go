package v3

import (
	"testing"
)

// TestPathCleaningLogic verifies the POSIX-style path sanitization logic
// used in GetRecordings to ensure traversal attempts are blocked.
func TestPathCleaningLogic(t *testing.T) {
	tests := []struct {
		input       string
		wantBlocked bool
		wantClean   string
	}{
		{"normal/path", false, "normal/path"},
		{"../parent", true, ""}, // Traversal
		{"../../passwd", true, ""},
		{"/absolute", false, "absolute"}, // Absolute stripped to relative
		{"./current", false, "current"},
		{"complex/../path", false, "path"},
		{"hack/..", false, ""}, // Cleaned to dot, then empty string in logic
		{"", false, ""},
	}

	for _, tt := range tests {
		// Mirroring logic in GetRecordings
		cleanRel, blocked := SanitizeRecordingRelPath(tt.input)

		if blocked != tt.wantBlocked {
			t.Errorf("Input %q: blocked=%v, want %v", tt.input, blocked, tt.wantBlocked)
		}
		if !blocked && cleanRel != tt.wantClean {
			t.Errorf("Input %q: clean=%q, want %q", tt.input, cleanRel, tt.wantClean)
		}
	}
}

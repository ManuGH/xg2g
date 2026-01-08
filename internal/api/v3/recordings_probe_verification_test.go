package v3

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestProbeDurationIntegration verifies that probeDuration correctly invokes ffprobe
// and parses its output. It skips if ffprobe is not installed.
func TestProbeDurationIntegration(t *testing.T) {
	// 1. Check for ffprobe availability
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not found in PATH, skipping integration test")
	}

	// 2. Create a dummy test file (5 seconds)
	tmpFile := "/tmp/xg2g_probe_regression_test.mp4"
	defer func() { _ = os.Remove(tmpFile) }()

	// Generate 5s video using ffmpeg (assumed present if ffprobe is, or we fail which is fine for local verification)
	// We can also check ffmpeg, but typically they come together.
	cmd := exec.Command("ffmpeg", "-f", "lavfi", "-i", "testsrc=duration=5:size=1280x720:rate=30", "-c:v", "libx264", "-y", tmpFile)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to generate test file: %v\n%s", err, string(out))
	}

	// 3. Run probeDuration
	// Accessing the internal function from the same package (api)
	start := time.Now()
	dur, err := ProbeDuration(context.Background(), tmpFile)
	elapsed := time.Since(start)

	// 4. Verify
	if err != nil {
		t.Fatalf("probeDuration failed: %v", err)
	}

	t.Logf("Probe successful!")
	t.Logf("Detected Duration: %v", dur)
	t.Logf("Execution Time: %v", elapsed)

	// Check accuracy (allow small variance for encoding overhead)
	// 5s = 5000ms. Allow 4.9s to 5.1s
	if dur < 4900*time.Millisecond || dur > 5100*time.Millisecond {
		t.Errorf("Duration mismatch: expected ~5s, got %v", dur)
	}
}

package recordings

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIsStable_StableFile(t *testing.T) {
	// Create temp file with content
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "stable.ts")

	content := []byte("stable video content")
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// File should be stable (size unchanged)
	if !IsStable(filePath, 100*time.Millisecond) {
		t.Error("expected file to be stable, but it wasn't")
	}
}

func TestIsStable_WritingFile(t *testing.T) {
	// Create temp file
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "writing.ts")

	// Write initial content
	if err := os.WriteFile(filePath, []byte("initial"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Start a goroutine that writes to the file
	done := make(chan bool)
	go func() {
		time.Sleep(50 * time.Millisecond)
		f, err := os.OpenFile(filePath, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			t.Logf("failed to open file for writing: %v", err)
			return
		}
		defer f.Close()
		f.WriteString(" more data")
		done <- true
	}()

	// File should NOT be stable (size changing)
	stable := IsStable(filePath, 100*time.Millisecond)
	<-done

	if stable {
		t.Error("expected file to be unstable (size changing), but it was reported as stable")
	}
}

func TestIsStable_NonExistentFile(t *testing.T) {
	// File doesn't exist
	if IsStable("/nonexistent/path/file.ts", 100*time.Millisecond) {
		t.Error("expected non-existent file to be unstable, but it was reported as stable")
	}
}

func TestIsStable_FileDeletedDuringCheck(t *testing.T) {
	// Create temp file
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "deleted.ts")

	if err := os.WriteFile(filePath, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Start a goroutine that deletes the file mid-check
	go func() {
		time.Sleep(50 * time.Millisecond)
		os.Remove(filePath)
	}()

	// File should NOT be stable (deleted during check)
	if IsStable(filePath, 100*time.Millisecond) {
		t.Error("expected file deleted during check to be unstable, but it was reported as stable")
	}
}

func TestIsStable_ZeroWindow(t *testing.T) {
	// Create temp file
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "zero-window.ts")

	if err := os.WriteFile(filePath, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Zero window should still work (immediate re-check)
	if !IsStable(filePath, 0) {
		t.Error("expected stable file with zero window to be stable")
	}
}

func TestIsStable_LargeWindow(t *testing.T) {
	// Create temp file
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "large-window.ts")

	if err := os.WriteFile(filePath, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Larger window (500ms) should still detect stable file
	// Note: This makes the test slower but more realistic
	start := time.Now()
	if !IsStable(filePath, 500*time.Millisecond) {
		t.Error("expected stable file with large window to be stable")
	}
	elapsed := time.Since(start)

	// Verify the window was actually waited
	if elapsed < 500*time.Millisecond {
		t.Errorf("expected at least 500ms elapsed, got %v", elapsed)
	}
}

func TestIsStable_EmptyFile(t *testing.T) {
	// Create empty file
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "empty.ts")

	if err := os.WriteFile(filePath, []byte{}, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Empty file (size=0) should be stable
	if !IsStable(filePath, 100*time.Millisecond) {
		t.Error("expected empty file to be stable")
	}
}

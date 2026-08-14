package recordings

import (
	"context"
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
	if err := os.WriteFile(filePath, content, 0600); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// File should be stable (size unchanged)
	if stable, err := IsStableCtx(context.Background(), filePath, 100*time.Millisecond); err != nil || !stable {
		t.Errorf("expected stable; got stable=%v err=%v", stable, err)
	}
}

func TestIsStable_WritingFile(t *testing.T) {
	// Create temp file
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "writing.ts")

	// Write initial content
	if err := os.WriteFile(filePath, []byte("initial"), 0600); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Keep writing throughout the observation window. A single delayed write
	// makes this test depend on the Go scheduler and can be starved when the
	// package suite is running CPU-heavy FFmpeg tests in parallel.
	ready := make(chan error, 1)
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		// #nosec G304
		f, err := os.OpenFile(filepath.Clean(filePath), os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			ready <- err
			done <- err
			return
		}
		defer func() { _ = f.Close() }()

		ready <- nil
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if _, err := f.WriteString(" more data"); err != nil {
					done <- err
					return
				}
			case <-stop:
				done <- nil
				return
			}
		}
	}()
	if err := <-ready; err != nil {
		t.Fatalf("failed to open file for writing: %v", err)
	}

	// File should NOT be stable (size changing)
	stable, err := IsStableCtx(context.Background(), filePath, time.Second)
	close(stop)
	if writeErr := <-done; writeErr != nil {
		t.Fatalf("failed to append test data: %v", writeErr)
	}

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stable {
		t.Error("expected file to be unstable (size changing), but it was reported as stable")
	}
}

func TestIsStable_NonExistentFile(t *testing.T) {
	// File doesn't exist
	if stable, err := IsStableCtx(context.Background(), "/nonexistent/path/file.ts", 100*time.Millisecond); err != nil || stable {
		t.Errorf("expected unstable (and no error for missing file?); got stable=%v err=%v", stable, err)
	}
}

func TestIsStable_FileDeletedDuringCheck(t *testing.T) {
	// Create temp file
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "deleted.ts")

	if err := os.WriteFile(filePath, []byte("content"), 0600); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Start a goroutine that deletes the file mid-check
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = os.Remove(filePath)
	}()

	// File should NOT be stable (deleted during check) -- stat2 fails
	if stable, err := IsStableCtx(context.Background(), filePath, 100*time.Millisecond); stable {
		t.Errorf("expected unstable; got stable=%v err=%v", stable, err)
	}
}

func TestIsStable_ZeroWindow(t *testing.T) {
	// Create temp file
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "zero-window.ts")

	if err := os.WriteFile(filePath, []byte("content"), 0600); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Zero window should still work (immediate re-check)
	if stable, err := IsStableCtx(context.Background(), filePath, 0); err != nil || !stable {
		t.Errorf("expected stable; got stable=%v err=%v", stable, err)
	}
}

func TestIsStable_LargeWindow(t *testing.T) {
	// Create temp file
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "large-window.ts")

	if err := os.WriteFile(filePath, []byte("content"), 0600); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Larger window (500ms) should still detect stable file
	start := time.Now()
	if stable, err := IsStableCtx(context.Background(), filePath, 500*time.Millisecond); err != nil || !stable {
		t.Errorf("expected stable; got stable=%v err=%v", stable, err)
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

	if err := os.WriteFile(filePath, []byte{}, 0600); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Empty file (size=0) should be stable
	if stable, err := IsStableCtx(context.Background(), filePath, 100*time.Millisecond); err != nil || !stable {
		t.Errorf("expected stable; got stable=%v err=%v", stable, err)
	}
}

func TestIsStable_ContextCancelled(t *testing.T) {
	// Create temp file
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "cancel.ts")

	if err := os.WriteFile(filePath, []byte("content"), 0600); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Create context that cancels quickly
	ctx, cancel := context.WithCancel(context.Background())

	// Start IsStable with long window
	start := time.Now()

	// Sleep briefly then cancel
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	stable, err := IsStableCtx(ctx, filePath, 2*time.Second)
	elapsed := time.Since(start)

	// Should return error
	if err == nil {
		t.Fatal("expected error due to context cancellation, got nil")
	}
	if stable {
		t.Error("expected not stable on cancel")
	}

	// Should return quickly (much less than 2s)
	if elapsed > 500*time.Millisecond {
		t.Errorf("IsStable took too long to cancel: %v", elapsed)
	}
}

func TestIsStable_LegacyWrapper(t *testing.T) {
	// Create temp file with content
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "legacy.ts")

	content := []byte("stable video content")
	if err := os.WriteFile(filePath, content, 0600); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Legacy wrapper should still work
	if !IsStable(filePath, 100*time.Millisecond) {
		t.Error("expected file to be stable using legacy wrapper")
	}
}

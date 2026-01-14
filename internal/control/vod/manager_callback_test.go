package vod

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestCallbackExecution verifies that BuildMonitor calls Manager callbacks on success/failure.
func TestCallbackExecution(t *testing.T) {
	t.Run("success callback updates metadata to READY", func(t *testing.T) {
		// Setup
		runner := &FakeRunner{
			startDelay: 100 * time.Millisecond,
			exitCode:   0,
		}
		manager := NewManager(runner, &mockProber{}, nil)

		// Trigger build
		id := "test-recording-id"
		input := "/fake/input.ts"
		workDir := "/tmp/test-vod-callback"
		outputTemp := "index.live.m3u8"
		finalPath := "/tmp/test-vod-callback/index.m3u8"

		if err := os.MkdirAll(workDir, 0755); err != nil {
			t.Fatalf("failed to create workDir: %v", err)
		}
		if err := os.WriteFile(workDir+"/"+outputTemp, []byte("#EXTM3U\n"), 0600); err != nil {
			t.Fatalf("failed to create output temp: %v", err)
		}

		mon, err := manager.StartBuild(context.Background(), id, id, input, workDir, outputTemp, finalPath, ProfileDefault)
		if err != nil {
			t.Fatalf("StartBuild failed: %v", err)
		}

		// Wait for build to complete
		time.Sleep(200 * time.Millisecond)

		// Verify callback was called by checking metadata state
		meta, exists := manager.GetMetadata(id)
		if !exists {
			t.Fatal("metadata does not exist after build completion")
		}

		if meta.State != ArtifactStateReady {
			t.Errorf("expected state=READY, got state=%s", meta.State)
		}

		// Note: PlaylistPath won't be set when finalPath is empty
		if finalPath != "" && meta.PlaylistPath != finalPath {
			t.Errorf("expected playlistPath=%q, got %q", finalPath, meta.PlaylistPath)
		}

		if meta.Error != "" {
			t.Errorf("expected no error, got error=%q", meta.Error)
		}

		// Verify job was removed from jobs map
		_, jobExists := manager.Get(context.Background(), id)
		if jobExists {
			t.Error("job still exists in jobs map after completion (expected cleanup)")
		}

		t.Logf("SUCCESS: Callback executed, metadata updated to READY, job cleaned up")
		_ = mon // Silence unused warning
	})

	t.Run("failure callback updates metadata to FAILED", func(t *testing.T) {
		// Setup
		runner := &FakeRunner{
			startDelay: 100 * time.Millisecond,
			exitCode:   1, // Simulate failure
		}
		manager := NewManager(runner, &mockProber{}, nil)

		// Trigger build
		id := "test-recording-fail"
		input := "/fake/input-fail.ts"
		workDir := "/tmp/test-vod-callback-fail"
		outputTemp := "index.live.m3u8"
		finalPath := "/tmp/test-vod-callback-fail/index.m3u8"

		if err := os.MkdirAll(workDir, 0755); err != nil {
			t.Fatalf("failed to create workDir: %v", err)
		}
		if err := os.WriteFile(workDir+"/"+outputTemp, []byte("#EXTM3U\n"), 0600); err != nil {
			t.Fatalf("failed to create output temp: %v", err)
		}

		_, err := manager.StartBuild(context.Background(), id, id, input, workDir, outputTemp, finalPath, ProfileDefault)
		if err != nil {
			t.Fatalf("StartBuild failed: %v", err)
		}

		// Wait for build to fail
		time.Sleep(200 * time.Millisecond)

		// Verify callback was called by checking metadata state
		meta, exists := manager.GetMetadata(id)
		if !exists {
			t.Fatal("metadata does not exist after build failure")
		}

		if meta.State != ArtifactStateFailed {
			t.Errorf("expected state=FAILED, got state=%s", meta.State)
		}

		if meta.Error == "" {
			t.Error("expected error message, got empty string")
		}

		// Verify job was removed from jobs map
		_, jobExists := manager.Get(context.Background(), id)
		if jobExists {
			t.Error("job still exists in jobs map after failure (expected cleanup)")
		}

		t.Logf("SUCCESS: Failure callback executed, metadata updated to FAILED, job cleaned up")
	})
}

// FakeRunner simulates a VOD runner for testing.
type FakeRunner struct {
	startDelay time.Duration
	exitCode   int
}

func (f *FakeRunner) Start(ctx context.Context, spec Spec) (Handle, error) {
	return &FakeHandle{
		delay:    f.startDelay,
		exitCode: f.exitCode,
		progress: make(chan ProgressEvent, 10),
	}, nil
}

// FakeHandle simulates a build process handle.
type FakeHandle struct {
	delay      time.Duration
	exitCode   int
	progress   chan ProgressEvent
	done       chan struct{}
	wg         sync.WaitGroup
	initOnce   sync.Once
	stopOnce   sync.Once
	closeOnce  sync.Once
	stopCalled bool
}

func (f *FakeHandle) Wait() error {
	// Initialize done channel once
	f.initOnce.Do(func() {
		f.done = make(chan struct{})

		// Send progress events in background
		f.wg.Add(1)
		go func() {
			defer f.wg.Done()
			ticker := time.NewTicker(50 * time.Millisecond)
			defer ticker.Stop()

			for {
				select {
				case <-f.done:
					return
				case <-ticker.C:
					select {
					case f.progress <- ProgressEvent{At: time.Now()}:
					case <-f.done:
						return
					}
				}
			}
		}()
	})

	// Wait for delay, then return
	time.Sleep(f.delay)

	// Signal background goroutine to stop (only once)
	f.stopOnce.Do(func() {
		close(f.done)
	})

	// Wait for goroutine to finish before closing progress channel
	f.wg.Wait()

	// Close the progress channel
	f.closeOnce.Do(func() { close(f.progress) })

	if f.exitCode != 0 {
		return &ExitError{Code: f.exitCode}
	}
	return nil
}

func (f *FakeHandle) Stop(grace, kill time.Duration) error {
	f.stopOnce.Do(func() {
		f.stopCalled = true
	})
	return nil
}

func (f *FakeHandle) Progress() <-chan ProgressEvent {
	return f.progress
}

func (f *FakeHandle) Diagnostics() []string {
	if f.exitCode != 0 {
		return []string{"fake process failed"}
	}
	return []string{"fake process succeeded"}
}

// ExitError represents a process exit with non-zero code.
type ExitError struct {
	Code int
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("process exited with code %d", e.Code)
}

func TestExitErrorString(t *testing.T) {
	msg := (&ExitError{Code: 12}).Error()
	if !strings.Contains(msg, "12") {
		t.Fatalf("expected exit code in error, got %q", msg)
	}
}

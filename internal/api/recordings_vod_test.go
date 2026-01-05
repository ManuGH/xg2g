package api

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

// TestWatchFFmpegProgress_Stall validates stall detection kills process
func TestWatchFFmpegProgress_Stall(t *testing.T) {
	// Setup: Create progress channel with initial progress
	progressCh := make(chan FFmpegProgress, 10)
	done := make(chan error, 1)

	// Send initial progress, then stop
	progressCh <- FFmpegProgress{
		OutTimeUs: 1000000,
		TotalSize: 500000,
		Speed:     "1.0x",
	}
	progressCh <- FFmpegProgress{
		OutTimeUs: 2000000,
		TotalSize: 1000000,
		Speed:     "1.0x",
	}
	// No more progress - simulating stall

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Mock process (nil is ok since we test error return, not actual kill)
	var proc *os.Process

	cfg := ProgressWatchConfig{
		StartupGrace: 100 * time.Millisecond, // Short grace for test
		StallTimeout: 200 * time.Millisecond, // Short stall timeout
		Tick:         50 * time.Millisecond,
		Strategy:     "test",
		RecordingID:  "test-recording",
	}

	logger := zerolog.New(os.Stdout).Level(zerolog.Disabled)

	// Execute watchdog (should detect stall)
	errCh := make(chan error, 1)
	go func() {
		errCh <- watchFFmpegProgress(ctx, done, progressCh, proc, cfg, logger)
	}()

	// Wait for stall detection (should happen after grace + stall timeout)
	select {
	case err := <-errCh:
		// Verify error indicates stall
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no progress")
		assert.Contains(t, err.Error(), "200ms") // stall timeout
	case <-time.After(1 * time.Second):
		t.Fatal("watchdog did not detect stall within timeout")
	}
}

// TestWatchFFmpegProgress_Success validates watchdog passes through success
func TestWatchFFmpegProgress_Success(t *testing.T) {
	progressCh := make(chan FFmpegProgress, 10)
	done := make(chan error, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := ProgressWatchConfig{
		StartupGrace: 100 * time.Millisecond,
		StallTimeout: 200 * time.Millisecond,
		Tick:         50 * time.Millisecond,
		Strategy:     "test",
		RecordingID:  "test-recording",
	}

	logger := zerolog.New(os.Stdout).Level(zerolog.Disabled)

	// Simulate successful completion
	go func() {
		time.Sleep(50 * time.Millisecond)
		done <- nil // ffmpeg completed successfully
	}()

	err := watchFFmpegProgress(ctx, done, progressCh, nil, cfg, logger)
	assert.NoError(t, err, "watchdog should pass through success")
}

// TestWatchFFmpegProgress_ContinuousProgress validates no stall with progress
func TestWatchFFmpegProgress_ContinuousProgress(t *testing.T) {
	progressCh := make(chan FFmpegProgress, 10)
	done := make(chan error, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := ProgressWatchConfig{
		StartupGrace: 50 * time.Millisecond,
		StallTimeout: 200 * time.Millisecond,
		Tick:         50 * time.Millisecond,
		Strategy:     "test",
		RecordingID:  "test-recording",
	}

	logger := zerolog.New(os.Stdout).Level(zerolog.Disabled)

	// Feed continuous progress for 500ms, then signal completion
	go func() {
		ticker := time.NewTicker(30 * time.Millisecond)
		defer ticker.Stop()
		outTime := int64(1000000)

		for i := 0; i < 10; i++ {
			<-ticker.C
			outTime += 1000000
			progressCh <- FFmpegProgress{
				OutTimeUs: outTime,
				TotalSize: int64(i * 100000),
				Speed:     "1.0x",
			}
		}
		done <- nil // Complete successfully
	}()

	err := watchFFmpegProgress(ctx, done, progressCh, nil, cfg, logger)
	assert.NoError(t, err, "continuous progress should not trigger stall")
}

// TestWatchFFmpegProgress_GracePeriod validates no stall during grace
func TestWatchFFmpegProgress_GracePeriod(t *testing.T) {
	progressCh := make(chan FFmpegProgress, 10)
	done := make(chan error, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := ProgressWatchConfig{
		StartupGrace: 500 * time.Millisecond, // Long grace period
		StallTimeout: 100 * time.Millisecond, // Shorter than grace
		Tick:         50 * time.Millisecond,
		Strategy:     "test",
		RecordingID:  "test-recording",
	}

	logger := zerolog.New(os.Stdout).Level(zerolog.Disabled)

	// Complete during grace period (no progress sent)
	go func() {
		time.Sleep(200 * time.Millisecond) // Within grace
		done <- nil
	}()

	err := watchFFmpegProgress(ctx, done, progressCh, nil, cfg, logger)
	assert.NoError(t, err, "should not stall during grace period")
}

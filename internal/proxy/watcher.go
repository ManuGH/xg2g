package proxy

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog"
)

// WaitForFile waits for a file to appear and reach a non-zero size using fsnotify.
// It replaces inefficient sleep-based polling.
func WaitForFile(ctx context.Context, logger zerolog.Logger, path string, timeout time.Duration) error {
	// 1. Fast path: check if file already exists
	info, err := os.Stat(path)
	if err == nil && info.Size() > 0 {
		return nil
	}

	// 2. Setup watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("fsnotify.NewWatcher: %w", err)
	}
	defer func() {
		_ = watcher.Close()
	}()

	// Watch the parent directory
	dir := filepath.Dir(path)
	if err := watcher.Add(dir); err != nil {
		// If parent dir doesn't exist yet, we might need to wait for it or just fail.
		// For HLS, the directory is usually created before calling this.
		return fmt.Errorf("watch directory %s: %w", dir, err)
	}

	// 3. Wait for events
	targetName := filepath.Base(path)
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	// Double check after adding watcher (race condition safety)
	if info, err := os.Stat(path); err == nil && info.Size() > 0 {
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return fmt.Errorf("timeout waiting for file %s", targetName)
		case event, ok := <-watcher.Events:
			if !ok {
				return fmt.Errorf("watcher channel closed")
			}
			// Check if this event relates to our file
			// We care about Create and Write events
			if filepath.Base(event.Name) == targetName {
				if event.Op&fsnotify.Create == fsnotify.Create || event.Op&fsnotify.Write == fsnotify.Write {
					// Verify size (sometimes Create fires before data is flushed)
					if info, err := os.Stat(path); err == nil && info.Size() > 0 {
						return nil
					}
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return fmt.Errorf("watcher error channel closed")
			}
			logger.Warn().Err(err).Msg("fsnotify watcher error")
		}
	}
}

// ReadStableFile reads a file once it stabilizes (no writes for a short duration).
// Optimised for Safari playlist reloading.
func ReadStableFile(ctx context.Context, path string, stabilityWindow time.Duration, timeout time.Duration) ([]byte, error) {
	// This is a "debounce" read.
	// 1. Wait for file existence (if not exists)
	// 2. Loop until file mtime/size stops changing for `stabilityWindow`

	deadline := time.Now().Add(timeout)

	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timeout waiting for stable file")
		}

		b1, err := os.ReadFile(path) // #nosec G304
		if err != nil {
			// If file doesn't exist, wait a bit
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(50 * time.Millisecond):
				continue
			}
		}

		info1, err := os.Stat(path)
		if err != nil {
			continue
		}

		// Wait for stability window
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(stabilityWindow):
		}

		b2, err := os.ReadFile(path) // #nosec G304
		if err != nil {
			continue
		}
		info2, err := os.Stat(path)
		if err != nil {
			continue
		}

		// Check stability
		if info2.Size() == info1.Size() && info2.ModTime().Equal(info1.ModTime()) {
			if bytes.Equal(b1, b2) {
				return b2, nil
			}
		}
	}
}

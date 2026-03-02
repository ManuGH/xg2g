package recordings

import (
	"context"
	"os"
	"time"
)

// IsStable checks if a file's size is stable over the given window duration (blocking).
// Deprecated: Use IsStableCtx for context-aware stability check.
func IsStable(path string, window time.Duration) bool {
	ok, _ := IsStableCtx(context.Background(), path, window)
	return ok
}

// IsStableCtx checks if a file's size is stable over the given window duration.
// This prevents streaming files that are currently being written to.
//
//   - Context is cancelled (returns error)
//   - File does not exist
//   - File size changes during the window
//   - Any stat operation fails
func IsStableCtx(ctx context.Context, path string, window time.Duration) (bool, error) {
	// First stat
	stat1, err := os.Stat(path)
	if err != nil {
		return false, nil
	}

	// Wait for stability window
	t := time.NewTimer(window)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
		return false, ctx.Err()
	}

	// Second stat
	stat2, err := os.Stat(path)
	if err != nil {
		return false, nil
	}

	// File is stable if size hasn't changed
	return stat1.Size() == stat2.Size(), nil
}

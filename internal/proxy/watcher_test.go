package proxy

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWaitForFile(t *testing.T) {
	// Create temp dir
	tmpDir, err := os.MkdirTemp("", "watcher-test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	logger := zerolog.Nop()
	targetPath := filepath.Join(tmpDir, "test.txt")

	// 1. Test Timeout
	t.Run("Timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		err := WaitForFile(ctx, logger, targetPath, 100*time.Millisecond)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "timeout")
	})

	// 2. Test Success (File Created)
	t.Run("Success", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		done := make(chan error, 1) // buffered to prevent blockage
		go func() {
			done <- WaitForFile(ctx, logger, targetPath, 1*time.Second)
		}()

		time.Sleep(100 * time.Millisecond)
		// Create file - G306 fix: 0600
		err := os.WriteFile(targetPath, []byte("test"), 0600)
		require.NoError(t, err)

		err = <-done
		assert.NoError(t, err)
	})

	// 3. Test Fast Path (File Already Exists)
	t.Run("FastPath", func(t *testing.T) {
		// File already exists from previous test
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		start := time.Now()
		err := WaitForFile(ctx, logger, targetPath, 1*time.Second)
		assert.NoError(t, err)
		assert.WithinDuration(t, start, time.Now(), 50*time.Millisecond, "fast path should return immediately")
	})
}

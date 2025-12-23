// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunner_Lifecycle(t *testing.T) {
	// Use "sleep_test" profile to invoke "sleep 10"
	runner := NewRunner("run_test", "/tmp/hls", "http://localhost") // binPath override in Start

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Start
	err := runner.Start(ctx, "test1", "1:0:1", "sleep_test")
	require.NoError(t, err)

	// 2. Stop (Signal)
	go func() {
		time.Sleep(100 * time.Millisecond)
		runner.Stop(ctx)
	}()

	// 3. Wait
	status, err := runner.Wait(ctx)

	// Sleep was SIGTERMed.
	// Exit code should be non-zero (usually 143 or similar for SIGTERM)
	// Error handles exit error
	assert.Error(t, err)
	assert.NotEqual(t, 0, status.Code)
	// Reason: "error" (since ctx wasn't cancelled yet, just Run stopped)
	// Or did we check cmd.ProcessState?
	// The runner logic sets reason="error" if err != nil and ctx not Done.
	assert.Equal(t, "error", status.Reason)
}

func TestRunner_ContextCancel(t *testing.T) {
	runner := NewRunner("run_test", "/tmp/hls", "http://localhost")

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := runner.Start(ctx, "test2", "1:0:1", "sleep_test")
	require.NoError(t, err)

	status, err := runner.Wait(ctx)

	// Should be killed by context
	assert.Error(t, err)
	assert.Equal(t, "ctx_cancel", status.Reason)
}

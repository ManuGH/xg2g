// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//go:build linux || (unix && !darwin)

package procgroup

import (
	"errors"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessGroupKill(t *testing.T) {
	// 1. Start a parent process (bash) that spawns a child (sleep)
	// We use "sleep 100 & sleep 100" to ensure we have a process structure
	// bash -> sleep (background)
	//      -> sleep (foreground)
	cmd := exec.Command("bash", "-c", "sleep 10 & sleep 10")

	// 2. Set it to be a group leader
	Set(cmd)

	// Start it
	err := cmd.Start()
	require.NoError(t, err)
	require.NotNil(t, cmd.Process)

	pid := cmd.Process.Pid
	t.Logf("Started parent process with PID %d", pid)

	// Wait a moment for bash to spawn children
	time.Sleep(100 * time.Millisecond)

	// Verify the process group exists
	pgid, err := syscall.Getpgid(pid)
	require.NoError(t, err)
	assert.Equal(t, pid, pgid, "Process should be group leader")

	// 3. Kill the group
	t.Logf("Killing process group %d", pgid)
	err = Kill(cmd, syscall.SIGKILL)
	require.NoError(t, err)

	// 4. Verify parent is gone
	// We wait for Wait() to return
	err = cmd.Wait()
	// It should exit with error (killed)
	if err == nil {
		t.Error("Command exited without error, expected signal kill")
	} else {
		// Verify it was killed
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// On Linux, WaitStatus is available
			status, ok := exitErr.Sys().(syscall.WaitStatus)
			if ok {
				assert.True(t, status.Signaled(), "Process should be signaled")
				assert.Equal(t, syscall.SIGKILL, status.Signal(), "Process should be killed by SIGKILL")
			}
		}
	}

	// 5. Verify children are gone (Logic check)
	// We can check if the PGID still exists by sending signal 0
	// This might be flaky if PIDs are reused rapidly, but on a test runner it's reasonable
	// Wait a tiny bit for kernel to reap
	time.Sleep(50 * time.Millisecond)

	err = syscall.Kill(-pgid, syscall.Signal(0))
	// We expect an error here: ESRCH (no such process) specific to the process group
	if err == nil {
		// If no error, the group still exists!
		t.Errorf("Process group %d still exists after kill", pgid)

		// Cleanup if failed
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	} else {
		assert.ErrorIs(t, err, syscall.ESRCH, "Process group should not exist")
	}
}

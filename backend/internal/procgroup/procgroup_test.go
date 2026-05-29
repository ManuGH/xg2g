// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//go:build linux

package procgroup

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGroupKill(t *testing.T) {
	// 1. Spawn a process that spawns a child and sleeps
	// We'll use a shell script for simplicity
	cmd := exec.Command("sh", "-c", "sleep 100 & sleep 100")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	err := cmd.Start()
	require.NoError(t, err)

	pid := cmd.Process.Pid
	pgid, err := syscall.Getpgid(pid)
	require.NoError(t, err)
	require.Equal(t, pid, pgid, "PID should be PGID leader")

	// 2. Kill the group
	err = KillGroup(pid, 100*time.Millisecond, 500*time.Millisecond)
	require.NoError(t, err)

	// 3. Verify parent is gone
	process, _ := os.FindProcess(pid)
	// On Unix, FindProcess always succeeds. We need to check Signal(0)
	err = process.Signal(syscall.Signal(0))
	require.Error(t, err, "Parent process should be dead")

	// 4. Verify the process group eventually disappears.
	// Child processes can briefly remain as zombies after the parent exits,
	// so an immediate ESRCH is racy under slower CI/coverage runs.
	require.Eventually(t, func() bool {
		return syscall.Kill(-pgid, syscall.Signal(0)) == syscall.ESRCH
	}, time.Second, 10*time.Millisecond, "Process group should be dead")
}

func TestKillGroupAlreadyGone(t *testing.T) {
	err := KillGroup(99999, 10*time.Millisecond, 10*time.Millisecond)
	require.NoError(t, err, "Should not fail if process is already gone")
}

// TestKillGroupGracefulDoesNotReap mirrors the ffmpeg adapter: a single owner
// reaps via cmd.Wait while KillGroupGraceful drives the SIGTERM->grace->SIGKILL
// ladder. The graceful kill must NOT reap (no os.Process.Wait), or it would race
// cmd.Wait on wait4 and corrupt the observed exit status.
func TestKillGroupGracefulDoesNotReap(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 100 & sleep 100")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, cmd.Start())

	pid := cmd.Process.Pid
	pgid, err := syscall.Getpgid(pid)
	require.NoError(t, err)
	require.Equal(t, pid, pgid, "PID should be PGID leader")

	// Sole reaper, exactly like the ffmpeg monitor goroutine.
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	require.NoError(t, KillGroupGraceful(pid, 500*time.Millisecond, 2*time.Second))

	select {
	case err := <-waitErr:
		require.Error(t, err, "a signalled process must yield a non-nil exit error")
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr, "cmd.Wait should observe the signal exit, not a double-wait error: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("cmd.Wait did not return; a double-wait likely stole the exit status")
	}

	require.Eventually(t, func() bool {
		return syscall.Kill(-pgid, syscall.Signal(0)) == syscall.ESRCH
	}, 2*time.Second, 10*time.Millisecond, "process group should be gone")
}

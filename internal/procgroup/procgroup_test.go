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

	// 4. Verify no processes found in that PGID
	// This is harder to check directly without scanning /proc or using pgrep
	// but Signal(-pid, 0) should return ESRCH
	err = syscall.Kill(-pgid, syscall.Signal(0))
	require.Equal(t, syscall.ESRCH, err, "Process group should be dead")
}

func TestKillGroupAlreadyGone(t *testing.T) {
	err := KillGroup(99999, 10*time.Millisecond, 10*time.Millisecond)
	require.NoError(t, err, "Should not fail if process is already gone")
}

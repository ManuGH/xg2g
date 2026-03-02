// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package procgroup

import (
	"errors"
	"os/exec"
	"time"
)

var (
	ErrProcessNotFound = errors.New("process not found")
	ErrKillFailed      = errors.New("kill operation failed")
)

// Set configures the command to start in a new process group.
// Mandatory for KillGroup to function as a group reaper.
func Set(cmd *exec.Cmd) {
	set(cmd)
}

// KillGroup attempts to terminate an entire process group tree.
// Mandatory: The process MUST have been spawned with procgroup.Set(cmd).
func KillGroup(pid int, grace, timeout time.Duration) error {
	// Standard lifecycle: SIGTERM -> wait -> SIGKILL
	return killGroup(pid, grace, timeout)
}

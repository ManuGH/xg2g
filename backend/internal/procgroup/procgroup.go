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
//
// KillGroup reaps the leader itself (os.Process.Wait). Do NOT use it for a
// process whose exec.Cmd.Wait is called elsewhere — use KillGroupGraceful, which
// never reaps, so it cannot race the owner's Wait on the wait4 syscall and
// corrupt the observed exit status.
func KillGroup(pid int, grace, timeout time.Duration) error {
	// Standard lifecycle: SIGTERM -> wait -> SIGKILL
	return killGroup(pid, grace, timeout)
}

// TerminateGroup sends SIGTERM to the whole process group, without waiting or
// reaping. It is intended as exec.Cmd.Cancel: a context cancellation then
// gracefully stops the group (letting ffmpeg flush its final HLS segment and
// ENDLIST) instead of the stdlib default of SIGKILL to the leader PID only.
func TerminateGroup(pid int) error {
	return terminateGroup(pid)
}

// KillGroupGraceful runs SIGTERM -> grace -> SIGKILL against the process group
// WITHOUT reaping the process. The exec.Cmd that owns the process must remain
// the sole reaper (via cmd.Wait); this function only signals and observes
// liveness, so it never races cmd.Wait on the wait4 syscall.
func KillGroupGraceful(pid int, grace, timeout time.Duration) error {
	return killGroupGraceful(pid, grace, timeout)
}

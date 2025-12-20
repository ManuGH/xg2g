// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//go:build unix && !windows

package procgroup

import (
	"errors"
	"os/exec"
	"syscall"
)

// Set configures the command to start in a new process group.
func Set(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// Kill sends a signal to the process group of the command.
// If the command or process is nil, or if the process has already exited, it returns nil.
func Kill(cmd *exec.Cmd, sig syscall.Signal) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	pid := cmd.Process.Pid
	// Use the process's PID as the PGID because we set Setpgid=true
	// which makes the process a process group leader with PGID = PID.
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		// If the process already exited, treat as success
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}

	// Negative PGID kills the whole group
	if err := syscall.Kill(-pgid, sig); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}
	return nil
}

// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//go:build linux

package procgroup

import (
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
)

func set(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

func killGroup(pid int, grace, timeout time.Duration) error {
	if pid <= 0 {
		return nil
	}

	// 1. Check if process exists
	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil // Already gone
	}

	// 2. SIGTERM to process group
	// Note: We use -pid to target the PGID leader and all children.
	// This works because we set Setpgid: true at spawn time.
	log.L().Debug().Int("pid", pid).Msg("sending SIGTERM to process group")
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		// If process group is already gone, or permissions fail
		if err == syscall.ESRCH {
			return nil
		}
		// Fallback to single PID if PGID kill restricted/failed
		_ = proc.Signal(syscall.SIGTERM)
	}

	// 3. Wait grace period
	done := make(chan struct{})
	go func() {
		_, _ = proc.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(grace):
		// Still alive after grace
	}

	// 4. SIGKILL to process group
	log.L().Warn().Int("pid", pid).Msg("SIGTERM grace period exceeded, sending SIGKILL to process group")
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		if err == syscall.ESRCH {
			return nil
		}
		_ = proc.Kill()
	}

	// Wait for final cleanup
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return ErrKillFailed
	}
}

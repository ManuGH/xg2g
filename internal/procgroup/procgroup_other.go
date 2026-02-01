// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//go:build !linux

package procgroup

import (
	"os"
	"os/exec"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
)

func set(cmd *exec.Cmd) {
	// No-op or best-effort for non-linux systems
}

func killGroup(pid int, grace, timeout time.Duration) error {
	if pid <= 0 {
		return nil
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}

	// Fallback Path: Only kill the root process
	log.L().Debug().Int("pid", pid).Msg("sending SIGTERM to root process (non-linux fallback)")
	_ = proc.Signal(os.Interrupt) // Use generic Interrupt

	done := make(chan struct{})
	go func() {
		_, _ = proc.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(grace):
		_ = proc.Kill()
	}

	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return ErrKillFailed
	}
}

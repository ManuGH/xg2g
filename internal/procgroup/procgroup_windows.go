// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//go:build windows

package procgroup

import (
	"os/exec"
	"syscall"
)

// Set is a no-op on Windows for process groups in this context.
func Set(cmd *exec.Cmd) {
	// No-op
}

// Kill sends a signal to the process on Windows.
// Since signals are not fully supported, it maps SIGKILL to Process.Kill().
// SIGTERM is ignored (no-op) as Windows doesn't support graceful termination reliably via signals.
func Kill(cmd *exec.Cmd, sig syscall.Signal) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	if sig == syscall.SIGKILL {
		return cmd.Process.Kill()
	}

	// Windows doesn't support SIGTERM in the same way.
	// For this specific use case, we rely on SIGKILL eventually.
	return nil
}

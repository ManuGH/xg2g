// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package procgroup

import (
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/ManuGH/xg2g/internal/metrics"
)

// Terminate attempts to gracefully stop a process group.
// It sends SIGTERM, waits for the process to exit (via the provided wait channel),
// and if it doesn't exit within grace, sends SIGKILL.
// It consumes and returns the error from waitCh.
// It is safe to call on nil commands (returns nil).
// Terminate attempts to gracefully stop a process group.
// It sends SIGTERM, waits for the process to exit (via the provided wait channel),
// and if it doesn't exit within grace, sends SIGKILL.
// It consumes and returns the error from waitCh.
// It is safe to call on nil commands (returns nil).
func Terminate(cmd *exec.Cmd, waitCh <-chan error, grace time.Duration) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	// 1. Send SIGTERM to Process Group
	// Note: If the process already finished normally, Kill calls are no-ops or harmless errors (ESRCH).
	if err := Kill(cmd, syscall.SIGTERM); err == nil {
		metrics.IncProcTerminate("SIGTERM", "sent")
	} else if strings.Contains(err.Error(), "process already finished") || strings.Contains(err.Error(), "no such process") {
		metrics.IncProcTerminate("SIGTERM", "esrch")
	} else {
		metrics.IncProcTerminate("SIGTERM", "error")
	}

	select {
	case err := <-waitCh:
		// Process exited voluntarily or due to SIGTERM
		if err == nil {
			metrics.IncProcWait("exit0")
		} else {
			metrics.IncProcWait("exit_nonzero")
		}
		return err
	case <-time.After(grace):
		// 2. Timeout -> Force Kill (SIGKILL)
		if err := Kill(cmd, syscall.SIGKILL); err == nil {
			metrics.IncProcTerminate("SIGKILL", "sent")
		} else if strings.Contains(err.Error(), "process already finished") || strings.Contains(err.Error(), "no such process") {
			metrics.IncProcTerminate("SIGKILL", "esrch")
		} else {
			metrics.IncProcTerminate("SIGKILL", "error")
		}

		// 3. Always Drain waitCh
		// We ignore the error from SIGKILL and return the result of the Wait().
		// If the process was blocked, SIGKILL should free it.
		err := <-waitCh
		if err == nil {
			metrics.IncProcWait("forced_exit0")
		} else {
			metrics.IncProcWait("forced_error")
		}
		return err
	}
}

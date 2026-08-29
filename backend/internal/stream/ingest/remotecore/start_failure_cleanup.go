// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package remotecore

import (
	"os/exec"
	"syscall"
)

// killTheGroupBeforeAnythingReapsIt ends a core that could not be owned.
//
// This is the only place in this package that addresses a process group by
// number, and it is correct exactly here because all four of these hold:
//
//   - cmd.Start returned successfully with SysProcAttr.Setpgid, so the leader is
//     the leader of its own group and its pid is that group's number;
//   - no Wait has run, so nothing has reaped the leader;
//   - a pid stays pinned while any member of its group is alive, and the leader
//     is - so the kernel cannot have handed this number to anybody else yet;
//   - acquiring a stable identity has just failed, so there is no identity to
//     signal through instead.
//
// It has to be the group rather than the leader. A core can have spawned
// something in the milliseconds between Start and the failed acquisition, and
// refusing to run the core is no reason to leave that behind. And it has to
// happen before cmd.Wait, because the moment the leader is reaped this call
// would be the very race the rest of the package exists to remove.
//
// Nothing else may do this. The running lifecycle - shutdown, teardown, the
// grace poll - goes through processIdentity, and a test enforces that this file
// is the only one here that names a group by number.
func killTheGroupBeforeAnythingReapsIt(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

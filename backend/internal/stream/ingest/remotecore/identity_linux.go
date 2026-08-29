// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//go:build linux

package remotecore

import (
	"errors"
	"fmt"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

// pidfdSignalProcessGroup asks the kernel to deliver to the process group led by
// the process the pidfd refers to.
//
// Not in golang.org/x/sys yet, and not in the manual page shipped by the distro
// either, so it is spelled out here: linux/pidfd.h defines the flags as
// THREAD (1<<0), THREAD_GROUP (1<<1) and PROCESS_GROUP (1<<2). A kernel that
// does not know the flag rejects the call with EINVAL, which is what the probe
// in acquireProcessIdentity turns into a refusal to start.
const pidfdSignalProcessGroup = 1 << 2

// errIdentityClosed is what group operations answer after the identity is gone.
// Nothing should ask, so this is a bug report rather than a condition to handle.
var errIdentityClosed = errors.New("process identity is closed")

// pidfdIdentity is a process group named by a pidfd.
//
// The reference is to the kernel's own record of that process, not to its
// number, and that is the whole point: after the leader has been reaped the
// pidfd still delivers to the group the leader once led, and once that group is
// empty it answers ESRCH - even if the number has meanwhile been handed to
// somebody else.
type pidfdIdentity struct {
	mu        sync.Mutex
	fd        int
	closeOnce sync.Once
	closeErr  error
}

// acquireProcessIdentity takes ownership of a freshly started core.
//
// Call order matters and is the caller's job: this has to run before anything
// can reap the leader, because a reaped leader is a number the kernel may have
// already given away.
func acquireProcessIdentity(pid int) (processIdentity, error) {
	// The pidfd is only a group identity if it names the group leader. The kernel
	// resolves PROCESS_GROUP through the leader's own reference and answers ESRCH
	// for any other member, so a core that is not its own leader would leave the
	// lifecycle signalling nothing at all. Setpgid makes it one; this checks that
	// rather than assuming it in a comment.
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil, fmt.Errorf("%w: gone before it could be identified", mediafacts.ErrCoreCrashed)
		}
		return nil, fmt.Errorf("read process group of %d: %w", pid, err)
	}
	if pgid != pid {
		return nil, fmt.Errorf("core %d is not the leader of its process group %d", pid, pgid)
	}

	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil, fmt.Errorf("%w: gone before it could be identified", mediafacts.ErrCoreCrashed)
		}
		return nil, fmt.Errorf("%w: pidfd_open: %w", ErrCoreUnsupportedPlatform, err)
	}
	id := &pidfdIdentity{fd: fd}

	// The capability probe, and the reason there is no kernel version check
	// anywhere in this package: signal 0 delivers nothing and runs the checks, so
	// the answer is the kernel's own. EINVAL means it does not know the flag.
	if err := id.signal(0); err != nil {
		switch {
		case errors.Is(err, syscall.EPERM):
			// The group is there and not ours to signal at this instant. That is a
			// permission answer about a group that exists, not a missing capability.
		case errors.Is(err, syscall.ESRCH):
			_ = id.Close()
			return nil, fmt.Errorf("%w: gone before it could be identified", mediafacts.ErrCoreCrashed)
		case errors.Is(err, syscall.EINVAL):
			_ = id.Close()
			return nil, fmt.Errorf("%w: this kernel has no PIDFD_SIGNAL_PROCESS_GROUP", ErrCoreUnsupportedPlatform)
		default:
			_ = id.Close()
			return nil, fmt.Errorf("probe process group of %d: %w", pid, err)
		}
	}
	return id, nil
}

// signal is the one place this package sends anything to a process group.
func (p *pidfdIdentity) signal(sig syscall.Signal) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fd < 0 {
		return errIdentityClosed
	}
	// A closed pidfd's number can be reopened as something else, which is why the
	// fd is read under the same lock that retires it.
	return unix.PidfdSendSignal(p.fd, sig, nil, pidfdSignalProcessGroup)
}

func (p *pidfdIdentity) SignalGroup(sig syscall.Signal) error { return p.signal(sig) }

func (p *pidfdIdentity) GroupExists() (bool, error) {
	err := p.signal(0)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, syscall.EPERM):
		// There, and not ours to signal - a descendant that changed user is still a
		// descendant.
		return true, nil
	case errors.Is(err, syscall.ESRCH):
		// The original group is gone. Not "a group under this number is gone":
		// this answer is about the group the core actually had.
		return false, nil
	default:
		return false, err
	}
}

func (p *pidfdIdentity) Close() error {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.fd >= 0 {
			p.closeErr = syscall.Close(p.fd)
			p.fd = -1
		}
	})
	return p.closeErr
}

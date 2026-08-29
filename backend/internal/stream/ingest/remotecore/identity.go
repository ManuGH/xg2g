// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package remotecore

import (
	"errors"
	"syscall"
)

// ErrCoreUnsupportedPlatform means this host cannot give the core a process
// identity that outlives its leader.
//
// It is not a crash and not a protocol failure: the core may well have started
// and answered. It is a statement about the kernel underneath it, and the only
// safe answer to it is to refuse - signalling by number instead would reinstate
// exactly the race this identity exists to remove.
var ErrCoreUnsupportedPlatform = errors.New("media facts core: platform does not provide a stable process identity")

// processIdentity is the core's process group, named by something the kernel
// owns rather than by a number.
//
// A pid is not an identity. Once the leader is reaped the number is the kernel's
// to hand out again, and it hands it out as soon as the group is empty - at
// which point kill(-pgid, ...) is a signal to whoever holds the number now, with
// no way to notice. Every group operation goes through here so that there is no
// second, numeric way to do it.
//
// There is deliberately no PID() on this interface. The moment the lifecycle can
// get the number back out, the old mistake is one line away again.
type processIdentity interface {
	// SignalGroup sends sig to every member of the original process group. It
	// reports ESRCH once that group is gone - never a signal to a group that
	// merely reuses the number.
	SignalGroup(sig syscall.Signal) error

	// GroupExists reports whether the original group still has members. A group
	// whose members are all zombies still counts as present: an unreaped process
	// is not gone, which is what the init/subreaper contract in the deployment is
	// there for.
	GroupExists() (bool, error)

	// Close releases the identity. Calling it more than once is not an error.
	Close() error
}

// acquireIdentity binds an identity to a process that has just been started.
//
// A variable so a test can put its own ownership layer underneath the lifecycle
// and prove that the lifecycle never reaches around it.
var acquireIdentity = acquireProcessIdentity

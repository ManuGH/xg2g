// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//go:build !linux

package remotecore

import "fmt"

// acquireProcessIdentity refuses, everywhere that is not Linux.
//
// The alternative would be to fall back to kill(-pgid, ...) off Linux, which is
// the race this package spent a step removing. A core that cannot be owned is a
// core that does not get started.
func acquireProcessIdentity(pid int) (processIdentity, error) {
	return nil, fmt.Errorf("%w: a stable process identity needs Linux pidfd support", ErrCoreUnsupportedPlatform)
}

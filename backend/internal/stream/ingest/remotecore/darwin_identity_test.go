// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package remotecore

import (
	"errors"
	"syscall"
)

type darwinTestIdentity struct {
	pid int
}

func (d *darwinTestIdentity) SignalGroup(sig syscall.Signal) error {
	return syscall.Kill(-d.pid, sig)
}

func (d *darwinTestIdentity) GroupExists() (bool, error) {
	err := syscall.Kill(-d.pid, 0)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	return false, err
}

func (d *darwinTestIdentity) Close() error {
	return nil
}

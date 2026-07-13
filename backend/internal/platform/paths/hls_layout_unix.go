// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//go:build !windows

package paths

import (
	"math"
	"os"
	"syscall"
)

// shmFreeBytes returns the number of free bytes in /dev/shm if it exists and
// is writable. Returns 0 on any error so callers fall back to disk.
func shmFreeBytes() uint64 { //nolint:unused
	if stat, err := os.Stat("/dev/shm"); err != nil || !stat.IsDir() {
		return 0
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs("/dev/shm", &st); err != nil {
		return 0
	}
	if st.Bsize <= 0 || st.Bavail == 0 {
		return 0
	}
	if uint64(st.Bsize) > math.MaxUint64/st.Bavail {
		return 0
	}
	return st.Bavail * uint64(st.Bsize)
}

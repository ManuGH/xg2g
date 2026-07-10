// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//go:build windows

package paths

// shmFreeBytes returns the number of free bytes in /dev/shm.
// On Windows this always returns 0 since /dev/shm does not exist.
func shmFreeBytes() uint64 {
	return 0
}
